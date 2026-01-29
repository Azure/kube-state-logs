package collector

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
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
	client          *kubernetes.Clientset
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
	stopCh          chan struct{}
	wg              sync.WaitGroup
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
			InsecureSkipVerify: true, // Kubelet uses self-signed certs
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create kubelet client: %w", err)
		}
		klog.Infof("Using kubelet API at %s:%d for pod/container collection", cfg.NodeIP, cfg.KubeletPort)
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
		c.kubeletHandlers["pod"] = resources.NewKubeletPodHandler(c.kubeletClient)
		c.kubeletHandlers["container"] = resources.NewKubeletContainerHandler(c.kubeletClient, c.metricsClient, c.config.ContainerEnvVars)
		klog.Info("Registered kubelet-based handlers for pod and container")
	}

	// Register resource handlers (informer-based)
	// Note: pod and container handlers are still registered but won't be used in kubelet mode
	handlers := map[string]interfaces.ResourceHandler{
		"pod":                              resources.NewPodHandler(c.client),
		"container":                        resources.NewContainerHandler(c.client, c.metricsClient, c.config.ContainerEnvVars),
		"service":                          resources.NewServiceHandler(c.client),
		"node":                             resources.NewNodeHandler(c.client, c.metricsClient),
		"deployment":                       resources.NewDeploymentHandler(c.client),
		"job":                              resources.NewJobHandler(c.client),
		"cronjob":                          resources.NewCronJobHandler(c.client),
		"configmap":                        resources.NewConfigMapHandler(c.client),
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

	for resourceName, handler := range handlers {
		c.handlers[resourceName] = handler
	}
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

		if !c.isCRDAvailable(gvr) {
			handlerKey := fmt.Sprintf("%s.%s", crdConfig.Resource, group)
			klog.Warningf("Skipping CRD handler for %s (%s): API resource not found", handlerKey, crdConfig.APIVersion)
			continue
		}

		// Create CRD handler
		handler := resources.NewCRDHandler(c.dynClient, gvr, crdConfig.Resource, crdConfig.CustomFields)
		handlerKey := fmt.Sprintf("%s.%s", crdConfig.Resource, group)
		c.crdHandlers[handlerKey] = handler

		klog.Infof("Registered CRD handler for %s (%s)", handlerKey, crdConfig.APIVersion)
	}
}

// isCRDAvailable verifies that the CRD's API resource is discoverable before wiring informers for it
func (c *Collector) isCRDAvailable(gvr schema.GroupVersionResource) bool {
	groupVersion := fmt.Sprintf("%s/%s", gvr.Group, gvr.Version)
	apiResourceList, err := c.client.Discovery().ServerResourcesForGroupVersion(groupVersion)
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) {
			if failed, ok := err.(*discovery.ErrGroupDiscoveryFailed); ok {
				for gv, groupErr := range failed.Groups {
					if apierrors.IsNotFound(groupErr) {
						klog.Warningf("Discovery reports %s as not found: %v", gv, groupErr)
						return false
					}
				}
			}
			klog.Warningf("Discovery failed for %s: %v (treating as transient)", groupVersion, err)
			return true
		}
		if apierrors.IsNotFound(err) {
			klog.Warningf("API group/version %s not found: %v", groupVersion, err)
			return false
		}
		klog.Warningf("Unable to discover resources for %s: %v (continuing)", groupVersion, err)
		return true
	}

	for _, resource := range apiResourceList.APIResources {
		if resource.Name == gvr.Resource {
			return true
		}
	}

	klog.Warningf("Resource %s not listed in discovery for %s/%s", gvr.Resource, gvr.Group, gvr.Version)
	return false
}

// Run starts the informers and collection loop
func (c *Collector) Run(ctx context.Context) error {
	klog.Info("Starting kube-state-logs with individual tickers...")

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
			klog.Warningf("No handler found for resource type: %s", resourceType)
			continue
		}

		// Use the podFactory for pod and container handlers (supports node filtering)
		factoryToUse := c.factory
		if shouldUsePodFactory(resourceType) {
			factoryToUse = c.podFactory
		}

		// Setup informer with no resync period
		if err := handler.SetupInformer(factoryToUse, c.logger, 0); err != nil {
			klog.Errorf("Failed to setup informer for %s: %v", resourceType, err)
			continue
		}
	}

	// Setup informers for CRD resources
	for handlerKey, crdHandler := range c.crdHandlers {
		if err := crdHandler.SetupInformer(c.dynFactory, c.logger, 0); err != nil {
			klog.Errorf("Failed to setup CRD informer for %s: %v", handlerKey, err)
			continue
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

	// Wait for all informers to sync
	klog.Info("Waiting for informers to sync...")
	synced := c.factory.WaitForCacheSync(c.stopCh)
	for resourceType, isSynced := range synced {
		if !isSynced {
			return fmt.Errorf("failed to sync informer for %v", resourceType)
		}
	}

	// Wait for pod factory informers to sync if it exists and is different from main factory
	if c.podFactory != nil && c.podFactory != c.factory {
		klog.Info("Waiting for pod factory informers to sync...")
		podSynced := c.podFactory.WaitForCacheSync(c.stopCh)
		for resourceType, isSynced := range podSynced {
			if !isSynced {
				return fmt.Errorf("failed to sync pod informer for %v", resourceType)
			}
		}
	}

	// Wait for dynamic informers to sync too
	if len(c.crdHandlers) > 0 {
		klog.Info("Waiting for dynamic informers to sync...")
		dynSynced := c.dynFactory.WaitForCacheSync(c.stopCh)
		for resourceType, isSynced := range dynSynced {
			if !isSynced {
				klog.Warningf("Failed to sync dynamic informer for %v", resourceType)
				// Don't fail completely, just warn and continue
			}
		}
	}

	klog.Info("All informers synced successfully")

	// Start individual tickers for each resource
	c.startResourceTickers(ctx)

	// Wait for context cancellation
	<-ctx.Done()
	klog.Info("Shutting down...")

	// Wait for all goroutines to finish
	c.wg.Wait()
	klog.Info("All goroutines stopped")
	return ctx.Err()
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

	// Start tickers for all resources
	for resourceName, interval := range resourceIntervals {
		// Check if this should use a kubelet handler
		if c.shouldUseKubeletHandler(resourceName) {
			kubeletHandler, exists := c.kubeletHandlers[resourceName]
			if !exists {
				klog.Warningf("No kubelet handler found for resource type: %s", resourceName)
				continue
			}

			klog.Infof("Starting kubelet ticker for %s with interval %v", resourceName, interval)

			c.wg.Add(1)
			go func(name string, tickerInterval time.Duration, h interfaces.KubeletHandler) {
				defer c.wg.Done()

				validatedInterval := validateTickerInterval(tickerInterval, name)
				ticker := time.NewTicker(validatedInterval)
				defer ticker.Stop()

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if err := c.collectAndLogKubeletResource(ctx, name, h); err != nil {
							klog.Errorf("Kubelet collection failed for %s: %v", name, err)
						}
					}
				}
			}(resourceName, interval, kubeletHandler)
			continue
		}

		// Use informer-based handler
		handler, exists := c.handlers[resourceName]
		if !exists {
			klog.Warningf("No handler found for resource type: %s", resourceName)
			continue
		}

		klog.Infof("Starting ticker for %s with interval %v", resourceName, interval)

		c.wg.Add(1)
		go func(name string, tickerInterval time.Duration, h interfaces.ResourceHandler) {
			defer c.wg.Done()

			// Validate ticker interval to prevent panics
			validatedInterval := validateTickerInterval(tickerInterval, name)
			ticker := time.NewTicker(validatedInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := c.collectAndLogResource(ctx, name, h); err != nil {
						klog.Errorf("Collection failed for %s: %v", name, err)
					}
				}
			}
		}(resourceName, interval, handler)
	}

	// Start tickers for CRD resources
	// Only start CRD tickers if "crd" is in the resources list OR if no standard resources are configured
	shouldStartCRDTickers := false
	for _, resourceName := range c.config.Resources {
		if resourceName == "crd" {
			shouldStartCRDTickers = true
			break
		}
	}

	// Also start CRD tickers if no standard resources are configured (CRD-only mode)
	if !shouldStartCRDTickers && len(resourceIntervals) == 0 {
		shouldStartCRDTickers = true
	}

	if shouldStartCRDTickers {
		for handlerKey, crdHandler := range c.crdHandlers {
			klog.Infof("Starting ticker for CRD %s with interval %v", handlerKey, c.config.LogInterval)

			c.wg.Add(1)
			go func(name string, tickerInterval time.Duration, h *resources.CRDHandler) {
				defer c.wg.Done()

				// Validate ticker interval to prevent panics
				validatedInterval := validateTickerInterval(tickerInterval, name)
				ticker := time.NewTicker(validatedInterval)
				defer ticker.Stop()

				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						if err := c.collectAndLogCRD(ctx, name, h); err != nil {
							klog.Errorf("CRD collection failed for %s: %v", name, err)
						}
					}
				}
			}(handlerKey, c.config.LogInterval, crdHandler)
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

// collectAndLog collects data from all configured resources and logs them
// This is now mainly used for initial collection or manual triggers
func (c *Collector) collectAndLog(ctx context.Context) error {
	var allEntries []any

	// Collect from each configured resource type
	for _, resourceType := range c.config.Resources {
		handler, exists := c.handlers[resourceType]
		if !exists {
			klog.Warningf("No handler found for resource type: %s", resourceType)
			continue
		}

		entries, err := handler.Collect(ctx, c.config.Namespaces)
		if err != nil {
			klog.Errorf("Failed to collect %s: %v", resourceType, err)
			continue
		}

		allEntries = append(allEntries, entries...)
	}

	// Log all collected entries
	for _, entry := range allEntries {
		if err := c.logger.Log(entry); err != nil {
			klog.Errorf("Failed to log entry: %v", err)
		}
	}

	klog.V(2).Infof("Collected and logged %d entries", len(allEntries))
	return nil
}
