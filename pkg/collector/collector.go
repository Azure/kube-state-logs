// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package collector

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	metricsclientset "k8s.io/metrics/pkg/client/clientset/versioned"

	"github.com/azure/kube-state-logs/pkg/collector/resources"
	"github.com/azure/kube-state-logs/pkg/config"
	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/kubelet"
)

// Collector handles the collection and logging of Kubernetes resource state
type Collector struct {
	config          *config.Config
	client          kubernetes.Interface
	dynClient       dynamic.Interface
	metricsClient   metricsclientset.Interface
	logger          interfaces.Logger
	handlers        map[string]interfaces.ResourceHandler
	kubeletHandlers map[string]interfaces.KubeletHandler // Kubelet-based handlers for pod/container
	crdHandlers     map[string]*resources.CRDHandler
	factory         informers.SharedInformerFactory
	podFactory      informers.SharedInformerFactory // Separate factory for pods, may have node filtering
	dynFactory      dynamicinformer.DynamicSharedInformerFactory
	kubeletClient   *kubelet.Client // Kubelet API client (nil if not using kubelet mode)
	kubeletSource   kubelet.SnapshotSource
	stopCh          chan struct{}
	wg              sync.WaitGroup
	ready           atomic.Bool
	electionRunning atomic.Bool
	leading         atomic.Bool
}

// Ready reports whether all enabled built-in caches have synced and collection is running.
func (c *Collector) Ready() bool {
	if c.config != nil && c.config.LeaderElection && c.electionRunning.Load() && !c.leading.Load() {
		return true
	}
	return c.ready.Load()
}

// validateTickerInterval ensures the interval is positive to prevent time.NewTicker panics
func validateTickerInterval(interval time.Duration, resource string) time.Duration {
	if interval <= 0 {
		klog.Warningf("Invalid ticker interval %v for resource %s, using default 1 minute", interval, resource)
		return time.Minute
	}
	return interval
}

// New creates a new Collector instance
func New(cfg *config.Config) (*Collector, error) {
	// Create Kubernetes client
	var kubeConfig *rest.Config
	var err error

	if cfg.Kubeconfig != "" {
		// Use kubeconfig file
		klog.Infof("Using kubeconfig file: %s", cfg.Kubeconfig)
		kubeConfig, err = clientcmd.BuildConfigFromFlags("", cfg.Kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("failed to build config from kubeconfig file: %w", err)
		}
	} else {
		// Use in-cluster config
		klog.Info("Using in-cluster config")
		kubeConfig, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("failed to get in-cluster config: %w", err)
		}
	}

	client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create dynamic client for CRDs
	dynClient, err := dynamic.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	// Create metrics client
	metricsClient, err := metricsclientset.NewForConfig(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics client: %w", err)
	}

	// Create logger
	logger := NewLogger()

	// Create informer factories (not used for pod/container in kubelet mode)
	factory, podFactory := createInformerFactories(client, cfg)

	// Create dynamic shared informer factory for CRDs
	dynFactory := dynamicinformer.NewDynamicSharedInformerFactory(dynClient, 0)

	// Create kubelet client if kubelet mode is enabled
	var kubeletClient *kubelet.Client
	if cfg.UseKubeletAPI {
		kubeletClient, err = kubelet.NewClient(kubelet.ClientConfig{
			NodeIP:             cfg.NodeIP,
			Port:               cfg.KubeletPort,
			InsecureSkipVerify: cfg.KubeletInsecureSkipVerify,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create kubelet client: %w", err)
		}
		if cfg.KubeletInsecureSkipVerify {
			klog.Warning("Kubelet TLS certificate verification is disabled")
		}
		klog.Infof("Using kubelet API at %s:%d for pod/container collection", cfg.NodeIP, cfg.KubeletPort)
	}

	var kubeletSource kubelet.SnapshotSource
	if kubeletClient != nil {
		kubeletSource = kubelet.NewCachedSnapshotSource(kubeletClient, 250*time.Millisecond)
	}

	// Create collector
	c := &Collector{
		config:          cfg,
		client:          client,
		dynClient:       dynClient,
		metricsClient:   metricsClient,
		logger:          logger,
		handlers:        make(map[string]interfaces.ResourceHandler),
		kubeletHandlers: make(map[string]interfaces.KubeletHandler),
		crdHandlers:     make(map[string]*resources.CRDHandler),
		factory:         factory,
		podFactory:      podFactory,
		dynFactory:      dynFactory,
		kubeletClient:   kubeletClient,
		kubeletSource:   kubeletSource,
		stopCh:          make(chan struct{}),
	}

	// Register resource handlers
	c.registerHandlers()

	// Register CRD handlers
	c.registerCRDHandlers()

	return c, nil
}

// createInformerFactories creates the main informer factory and pod-specific factory.
// The pod factory may be different from the main factory when node filtering is enabled.
// Returns (mainFactory, podFactory). podFactory will be nil if using kubelet API mode.
func createInformerFactories(client kubernetes.Interface, cfg *config.Config) (informers.SharedInformerFactory, informers.SharedInformerFactory) {
	// Build informer factory options
	var factoryOpts []informers.SharedInformerOption
	if len(cfg.Namespaces) == 1 {
		factoryOpts = append(factoryOpts, informers.WithNamespace(cfg.Namespaces[0]))
		klog.Infof("Created namespace-scoped informer factory for namespace: %s", cfg.Namespaces[0])
	} else if len(cfg.Namespaces) > 1 {
		klog.Infof("Created cluster-wide informer factory for multiple namespaces: %v", cfg.Namespaces)
	} else {
		klog.Info("Created cluster-wide informer factory for all namespaces")
	}

	// Create shared informer factory
	factory := informers.NewSharedInformerFactoryWithOptions(client, 0, factoryOpts...)

	// If using kubelet API, we don't need a pod factory since pod/container
	// will be collected via kubelet API, not informers
	if cfg.UseKubeletAPI {
		return factory, nil
	}

	// Create a separate pod informer factory for node-filtered or unscheduled-pod scenarios.
	// This is used by pod and container handlers when --node or --track-unscheduled-pods is set.
	var podFactory informers.SharedInformerFactory
	if cfg.Node != "" {
		// Filter to pods scheduled on this specific node
		podFactoryOpts := append(factoryOpts, informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = "spec.nodeName=" + cfg.Node
		}))
		podFactory = informers.NewSharedInformerFactoryWithOptions(client, 0, podFactoryOpts...)
		klog.Infof("Created node-filtered pod informer factory for node: %s", cfg.Node)
	} else if cfg.TrackUnscheduledPods {
		// Filter to pods that have not yet been scheduled (spec.nodeName is empty)
		podFactoryOpts := append(factoryOpts, informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.FieldSelector = "spec.nodeName="
		}))
		podFactory = informers.NewSharedInformerFactoryWithOptions(client, 0, podFactoryOpts...)
		klog.Info("Created unscheduled-pods informer factory (spec.nodeName='')")
	} else {
		// No special filtering - use the main factory
		podFactory = factory
	}

	return factory, podFactory
}

// shouldUsePodFactory returns true if the given resource type should use the pod factory
// (which may have node filtering applied) instead of the main factory.
func shouldUsePodFactory(resourceType string) bool {
	return resourceType == "pod" || resourceType == "container"
}

// shouldUseKubeletHandler returns true if the given resource type should use
// kubelet handlers instead of informer-based handlers.
func (c *Collector) shouldUseKubeletHandler(resourceType string) bool {
	return c.kubeletClient != nil && (resourceType == "pod" || resourceType == "container")
}

// registerHandlers registers all available resource handlers
func (c *Collector) registerHandlers() {
	// If using kubelet mode, register kubelet handlers for pod/container
	if c.kubeletClient != nil {
		c.kubeletHandlers["pod"] = resources.NewKubeletPodHandler(
			c.kubeletSource,
			c.client,
			c.config.Node,
			c.config.PromotedNodeLabelsFor("pod")...,
		)
		c.kubeletHandlers["container"] = resources.NewKubeletContainerHandler(
			c.kubeletSource,
			c.client,
			c.config.Node,
			c.config.ContainerEnvVars,
			c.config.PromotedNodeLabelsFor("container")...,
		)
		klog.Info("Registered kubelet-based handlers for pod and container")
	}

	podPromotedNodeLabels := c.config.PromotedNodeLabelsFor("pod")
	containerPromotedNodeLabels := c.config.PromotedNodeLabelsFor("container")
	if c.config.TrackUnscheduledPods {
		// Unscheduled pods have no node to promote labels from. More importantly,
		// the pod factory's spec.nodeName selector must never be applied to a node
		// informer created from that same factory.
		podPromotedNodeLabels = nil
		containerPromotedNodeLabels = nil
	}
	podHandler := resources.NewPodHandler(c.client, podPromotedNodeLabels...)
	containerHandler := resources.NewContainerHandler(c.client, c.metricsClient, c.config.ContainerEnvVars, containerPromotedNodeLabels...)
	if c.config.Node != "" {
		podHandler.UseDirectNodeLabelLookup(c.client, c.config.Node)
		containerHandler.UseDirectNodeLabelLookup(c.client, c.config.Node)
		containerHandler.SetNodeFilter(c.config.Node)
	}

	// Register resource handlers (informer-based)
	// Note: pod and container handlers are still registered but won't be used in kubelet mode
	handlers := map[string]interfaces.ResourceHandler{
		"pod":                              podHandler,
		"container":                        containerHandler,
		"service":                          resources.NewServiceHandler(c.client),
		"node":                             resources.NewNodeHandler(c.client, c.metricsClient),
		"deployment":                       resources.NewDeploymentHandler(c.client),
		"job":                              resources.NewJobHandler(c.client),
		"cronjob":                          resources.NewCronJobHandler(c.client),
		"configmap":                        resources.NewConfigMapHandler(c.client, c.config.ConfigMapIncludeValues),
		"secret":                           resources.NewSecretHandler(c.client),
		"persistentvolumeclaim":            resources.NewPersistentVolumeClaimHandler(c.client),
		"ingress":                          resources.NewIngressHandler(c.client),
		"horizontalpodautoscaler":          resources.NewHorizontalPodAutoscalerHandler(c.client),
		"serviceaccount":                   resources.NewServiceAccountHandler(c.client),
		"endpoints":                        resources.NewEndpointsHandler(c.client),
		"persistentvolume":                 resources.NewPersistentVolumeHandler(c.client),
		"resourcequota":                    resources.NewResourceQuotaHandler(c.client),
		"poddisruptionbudget":              resources.NewPodDisruptionBudgetHandler(c.client),
		"storageclass":                     resources.NewStorageClassHandler(c.client),
		"networkpolicy":                    resources.NewNetworkPolicyHandler(c.client),
		"replicationcontroller":            resources.NewReplicationControllerHandler(c.client),
		"limitrange":                       resources.NewLimitRangeHandler(c.client),
		"lease":                            resources.NewLeaseHandler(c.client),
		"role":                             resources.NewRoleHandler(c.client),
		"clusterrole":                      resources.NewClusterRoleHandler(c.client),
		"rolebinding":                      resources.NewRoleBindingHandler(c.client),
		"clusterrolebinding":               resources.NewClusterRoleBindingHandler(c.client),
		"volumeattachment":                 resources.NewVolumeAttachmentHandler(c.client),
		"certificatesigningrequest":        resources.NewCertificateSigningRequestHandler(c.client),
		"namespace":                        resources.NewNamespaceHandler(c.client),
		"daemonset":                        resources.NewDaemonSetHandler(c.client),
		"statefulset":                      resources.NewStatefulSetHandler(c.client),
		"replicaset":                       resources.NewReplicaSetHandler(c.client),
		"mutatingwebhookconfiguration":     resources.NewMutatingWebhookConfigurationHandler(c.client),
		"validatingwebhookconfiguration":   resources.NewValidatingWebhookConfigurationHandler(c.client),
		"ingressclass":                     resources.NewIngressClassHandler(c.client),
		"priorityclass":                    resources.NewPriorityClassHandler(c.client),
		"runtimeclass":                     resources.NewRuntimeClassHandler(c.client),
		"validatingadmissionpolicy":        resources.NewValidatingAdmissionPolicyHandler(c.client),
		"validatingadmissionpolicybinding": resources.NewValidatingAdmissionPolicyBindingHandler(c.client),
	}

	maps.Copy(c.handlers, handlers)
}

// registerCRDHandlers registers CRD handlers based on configuration
func (c *Collector) registerCRDHandlers() {
	for _, crdConfig := range c.config.CRDs {
		// Parse the API version
		parts := strings.Split(crdConfig.APIVersion, "/")
		if len(parts) != 2 {
			klog.Warningf("Invalid API version format: %s", crdConfig.APIVersion)
			continue
		}

		group := parts[0]
		version := parts[1]

		// Create GroupVersionResource
		gvr := schema.GroupVersionResource{
			Group:    group,
			Version:  version,
			Resource: crdConfig.Resource,
		}

		// Create CRD handler
		handler := resources.NewCRDHandler(c.dynClient, gvr, crdConfig.Resource, crdConfig.CustomFields)
		handlerKey := fmt.Sprintf("%s.%s", crdConfig.Resource, group)
		c.crdHandlers[handlerKey] = handler

		klog.Infof("Registered CRD handler for %s (%s)", handlerKey, crdConfig.APIVersion)
	}
}

// Run starts the informers and collection loop
func (c *Collector) Run(ctx context.Context) error {
	if ctx.Err() != nil {
		return nil
	}
	if !c.config.LeaderElection {
		return c.run(ctx)
	}

	lock, err := resourcelock.New(
		resourcelock.LeasesResourceLock,
		c.config.LeaderElectionLeaseNamespace,
		c.config.LeaderElectionLeaseName,
		c.client.CoreV1(),
		c.client.CoordinationV1(),
		resourcelock.ResourceLockConfig{Identity: c.config.LeaderElectionIdentity},
	)
	if err != nil {
		return fmt.Errorf("failed to create leader election lock: %w", err)
	}

	electionCtx, cancelElection := context.WithCancelCause(ctx)
	defer cancelElection(nil)

	runResult := make(chan error, 1)
	var startedLeading atomic.Bool
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          lock,
		LeaseDuration: c.config.LeaderElectionLeaseDuration,
		RenewDeadline: c.config.LeaderElectionRenewDeadline,
		RetryPeriod:   c.config.LeaderElectionRetryPeriod,
		Name:          c.config.LeaderElectionLeaseName,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(leaderCtx context.Context) {
				startedLeading.Store(true)
				c.leading.Store(true)
				klog.Infof("Acquired leader lease %s/%s", c.config.LeaderElectionLeaseNamespace, c.config.LeaderElectionLeaseName)
				runErr := c.run(leaderCtx)
				runResult <- runErr
				if runErr != nil {
					c.electionRunning.Store(false)
					cancelElection(runErr)
				}
				c.leading.Store(false)
			},
			OnStoppedLeading: func() {
				if ctx.Err() == nil {
					klog.Warning("Leader election stopped")
				}
			},
			OnNewLeader: func(identity string) {
				if identity != c.config.LeaderElectionIdentity {
					klog.Infof("Leader is now %s", identity)
				}
			},
		},
	})
	if err != nil {
		return fmt.Errorf("invalid leader election configuration: %w", err)
	}

	klog.Infof("Starting leader election with identity %s", c.config.LeaderElectionIdentity)
	c.electionRunning.Store(true)
	defer c.electionRunning.Store(false)
	elector.Run(electionCtx)

	var runErr error
	if startedLeading.Load() {
		runErr = <-runResult
	}
	if ctx.Err() != nil {
		return nil
	}
	if runErr != nil {
		return runErr
	}
	if cause := context.Cause(electionCtx); cause != nil && cause != context.Canceled {
		return cause
	}
	return fmt.Errorf("leader election lost")
}

func (c *Collector) run(ctx context.Context) (runErr error) {
	parentCtx := ctx
	ctx, cancel := context.WithCancelCause(ctx)
	defer func() {
		c.ready.Store(false)
		// Informer failures are fatal, but caller cancellation is a graceful shutdown.
		if runErr == nil && parentCtx.Err() == nil {
			runErr = context.Cause(ctx)
		}
		cancel(nil)
	}()
	c.ready.Store(false)
	if ctx.Err() != nil {
		return nil
	}

	klog.Info("Starting kube-state-logs with individual tickers...")

	for _, resourceConfig := range c.config.ResourceConfigs {
		if resourceConfig.Name == "crd" || c.shouldUseKubeletHandler(resourceConfig.Name) {
			continue
		}
		if _, exists := c.handlers[resourceConfig.Name]; !exists {
			return fmt.Errorf("unknown resource type: %s", resourceConfig.Name)
		}
	}

	// Setup informers for each configured resource type (excluding "crd" which is handled separately)
	for _, resourceType := range c.config.Resources {
		// Skip "crd" as it's a special resource type for CRD-only collection
		if resourceType == "crd" {
			continue
		}

		// Skip informer setup for pod/container if using kubelet mode
		if c.shouldUseKubeletHandler(resourceType) {
			klog.Infof("Skipping informer setup for %s (using kubelet API)", resourceType)
			continue
		}

		handler, exists := c.handlers[resourceType]
		if !exists {
			return fmt.Errorf("unknown resource type: %s", resourceType)
		}

		// Use the podFactory for pod and container handlers (supports node filtering)
		factoryToUse := c.factory
		if shouldUsePodFactory(resourceType) {
			factoryToUse = c.podFactory
		}

		// Setup informer with no resync period
		if err := handler.SetupInformer(factoryToUse, c.logger, 0); err != nil {
			return fmt.Errorf("failed to setup informer for %s: %w", resourceType, err)
		}
		for _, informer := range handler.GetInformers() {
			if err := informer.SetWatchErrorHandlerWithContext(func(watchCtx context.Context, reflector *cache.Reflector, err error) {
				cache.DefaultWatchErrorHandler(watchCtx, reflector, err)
				if apierrors.IsNotFound(err) || apierrors.IsUnauthorized(err) || apierrors.IsForbidden(err) {
					c.ready.Store(false)
					cancel(fmt.Errorf("required informer for %s failed: %w", resourceType, err))
				}
			}); err != nil {
				return fmt.Errorf("failed to set informer error handler for %s: %w", resourceType, err)
			}
		}
	}

	// Setup informers for CRD resources
	for handlerKey, crdHandler := range c.crdHandlers {
		if err := crdHandler.SetupInformer(c.dynFactory, c.logger, 0); err != nil {
			return fmt.Errorf("failed to setup CRD informer for %s: %w", handlerKey, err)
		}
	}

	// Create a context-aware stop channel
	go func() {
		<-ctx.Done()
		close(c.stopCh)
	}()

	// Start the informer factories
	c.factory.Start(c.stopCh)
	// Start the pod factory if it exists and is different from the main factory
	if c.podFactory != nil && c.podFactory != c.factory {
		c.podFactory.Start(c.stopCh)
	}
	c.dynFactory.Start(c.stopCh)

	// CRDs sync independently; only built-in caches gate readiness.
	klog.Info("Waiting for built-in informers to sync...")
	synced := c.factory.WaitForCacheSync(c.stopCh)
	if ctx.Err() != nil {
		return nil
	}
	for resourceType, isSynced := range synced {
		if !isSynced {
			return fmt.Errorf("failed to sync informer for %v", resourceType)
		}
	}

	// Wait for pod factory informers to sync if it exists and is different from main factory
	if c.podFactory != nil && c.podFactory != c.factory {
		klog.Info("Waiting for pod factory informers to sync...")
		podSynced := c.podFactory.WaitForCacheSync(c.stopCh)
		if ctx.Err() != nil {
			return nil
		}
		for resourceType, isSynced := range podSynced {
			if !isSynced {
				return fmt.Errorf("failed to sync pod informer for %v", resourceType)
			}
		}
	}

	klog.Info("All built-in informers synced successfully")

	// Start individual tickers for each resource
	c.startResourceTickers(ctx)
	c.ready.Store(true)

	// Wait for context cancellation
	<-ctx.Done()
	c.ready.Store(false)
	klog.Info("Shutting down...")

	// Wait for all goroutines to finish
	c.wg.Wait()
	klog.Info("All goroutines stopped")
	return nil
}

// startResourceTickers starts individual tickers for each resource based on their configured intervals
func (c *Collector) startResourceTickers(ctx context.Context) {
	// Create a map of resource names to their intervals
	resourceIntervals := make(map[string]time.Duration)

	// First, populate with specific resource configs (excluding "crd")
	for _, resourceConfig := range c.config.ResourceConfigs {
		// Skip "crd" as it's handled separately
		if resourceConfig.Name == "crd" {
			continue
		}
		resourceIntervals[resourceConfig.Name] = resourceConfig.Interval
	}

	// Then, ensure all resources in the Resources list have an interval (use default if not specified)
	for _, resourceName := range c.config.Resources {
		// Skip "crd" as it's handled separately
		if resourceName == "crd" {
			continue
		}
		if _, exists := resourceIntervals[resourceName]; !exists {
			resourceIntervals[resourceName] = c.config.LogInterval
		}
	}

	resourceConfigMap := make(map[string]config.ResourceConfig)
	for _, rc := range c.config.ResourceConfigs {
		resourceConfigMap[rc.Name] = rc
	}

	// Start tickers for all resources
	for resourceName, interval := range resourceIntervals {
		// Check if this should use a kubelet handler
		if c.shouldUseKubeletHandler(resourceName) {
			kubeletHandler, exists := c.kubeletHandlers[resourceName]
			if !exists {
				klog.Warningf("No kubelet handler found for resource type: %s", resourceName)
				continue
			}
			if rc, ok := resourceConfigMap[resourceName]; ok {
				if configurable, ok := kubeletHandler.(interface {
					SetSelectors(labels.Selector, fields.Selector)
				}); ok {
					configurable.SetSelectors(rc.LabelSelector, rc.FieldSelector)
				}
			}

			klog.Infof("Starting kubelet ticker for %s with interval %v", resourceName, interval)

			c.wg.Go(func() {
				ticker := time.Tick(validateTickerInterval(interval, resourceName))

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker:
						if err := c.collectAndLogKubeletResource(ctx, resourceName, kubeletHandler); err != nil {
							klog.Errorf("Kubelet collection failed for %s: %v", resourceName, err)
						}
					}
				}
			})
			continue
		}

		// Use informer-based handler
		handler, exists := c.handlers[resourceName]
		if !exists {
			klog.Warningf("No handler found for resource type: %s", resourceName)
			continue
		}

		if rc, ok := resourceConfigMap[resourceName]; ok {
			if configurable, ok := handler.(interface {
				SetSelectors(labels.Selector, fields.Selector)
			}); ok {
				configurable.SetSelectors(rc.LabelSelector, rc.FieldSelector)
			}
		}

		klog.Infof("Starting ticker for %s with interval %v", resourceName, interval)

		c.wg.Go(func() {
			ticker := time.Tick(validateTickerInterval(interval, resourceName))

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker:
					if err := c.collectAndLogResource(ctx, resourceName, handler); err != nil {
						klog.Errorf("Collection failed for %s: %v", resourceName, err)
					}
				}
			}
		})
	}

	// Start tickers for CRD resources
	// Only start CRD tickers if "crd" is in the resources list OR if no standard resources are configured
	shouldStartCRDTickers := slices.Contains(c.config.Resources, "crd")

	// Also start CRD tickers if no standard resources are configured (CRD-only mode)
	if !shouldStartCRDTickers && len(resourceIntervals) == 0 {
		shouldStartCRDTickers = true
	}

	if shouldStartCRDTickers {
		for handlerKey, crdHandler := range c.crdHandlers {
			c.wg.Go(func() {
				klog.Infof("Waiting for CRD informer %s to sync...", handlerKey)
				if !cache.WaitForCacheSync(ctx.Done(), crdHandler.HasSynced) {
					return
				}

				klog.Infof("Starting ticker for CRD %s with interval %v", handlerKey, c.config.LogInterval)
				ticker := time.Tick(validateTickerInterval(c.config.LogInterval, handlerKey))

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker:
						if err := c.collectAndLogCRD(ctx, handlerKey, crdHandler); err != nil {
							klog.Errorf("CRD collection failed for %s: %v", handlerKey, err)
						}
					}
				}
			})
		}
	}
}

// collectAndLogResource collects and logs data for a specific resource
func (c *Collector) collectAndLogResource(ctx context.Context, resourceName string, handler interfaces.ResourceHandler) error {
	entries, err := handler.Collect(ctx, c.config.Namespaces)
	if err != nil {
		return fmt.Errorf("failed to collect %s: %w", resourceName, err)
	}

	// Log all collected entries
	for _, entry := range entries {
		if err := c.logger.Log(entry); err != nil {
			klog.Errorf("Failed to log entry for %s: %v", resourceName, err)
		}
	}

	klog.V(2).Infof("Collected and logged %d entries for %s", len(entries), resourceName)
	return nil
}

// collectAndLogKubeletResource collects and logs data for a kubelet-based resource
func (c *Collector) collectAndLogKubeletResource(ctx context.Context, resourceName string, handler interfaces.KubeletHandler) error {
	entries, err := handler.Collect(ctx, c.config.Namespaces)
	if err != nil {
		return fmt.Errorf("failed to collect %s from kubelet: %w", resourceName, err)
	}

	// Log all collected entries
	for _, entry := range entries {
		if err := c.logger.Log(entry); err != nil {
			klog.Errorf("Failed to log entry for %s: %v", resourceName, err)
		}
	}

	klog.V(2).Infof("Collected and logged %d entries for %s (kubelet)", len(entries), resourceName)
	return nil
}

// collectAndLogCRD collects and logs data for a specific CRD
func (c *Collector) collectAndLogCRD(ctx context.Context, handlerKey string, handler *resources.CRDHandler) error {
	entries, err := handler.Collect(ctx, c.config.Namespaces)
	if err != nil {
		return fmt.Errorf("failed to collect CRD %s: %w", handlerKey, err)
	}

	// Log all collected entries
	for _, entry := range entries {
		if err := c.logger.Log(entry); err != nil {
			klog.Errorf("Failed to log CRD entry for %s: %v", handlerKey, err)
		}
	}

	klog.V(2).Infof("Collected and logged %d CRD entries for %s", len(entries), handlerKey)
	return nil
}
