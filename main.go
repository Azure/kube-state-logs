// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"k8s.io/klog/v2"

	"github.com/azure/kube-state-logs/pkg/collector"
	"github.com/azure/kube-state-logs/pkg/config"
)

func main() {
	// Parse command line flags
	var (
		logInterval          = flag.Duration("log-interval", 1*time.Minute, "Default interval between log outputs")
		resources            = flag.String("resources", config.DefaultResourceList, "Comma-separated list of resources to monitor")
		resourceConfigs      = flag.String("resource-configs", "", "Comma-separated list of resource configs: 'resource:interval[:labels=...][:fields=...][:promote-node-labels=label|label]'. Use '\\\\,' to escape commas in selectors (e.g., 'configmap:1m:labels=app=foo\\\\,env=prod'). If not specified, uses log-interval for all resources.")
		crdConfigs           = flag.String("crd-configs", "", "Comma-separated list of CRD configurations (e.g., 'msi-acrpull.microsoft.com/v1:acrpullbindings:spec.acrServer|spec.managedIdentityResourceID|status.lastTokenRefreshTime|status.tokenExpirationTime')")
		namespaces           = flag.String("namespaces", "", "Comma-separated list of namespaces to monitor (empty for all)")
		logLevel             = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		kubeconfig           = flag.String("kubeconfig", "", "Path to kubeconfig file (empty for in-cluster config)")
		containerEnvVars     = flag.String("container-envvars", "", "Comma-separated list of environment variable names to capture from containers (e.g., 'GOMAXPROCS,MY_FLAG'). Empty disables capturing.")
		configMapValues      = flag.Bool("configmap-include-values", false, "Include ConfigMap data values (data only, no binary data)")
		node                 = flag.String("node", "", "Filter pods to only those scheduled on this node (used for DaemonSet deployment mode)")
		trackUnscheduledPods = flag.Bool("track-unscheduled-pods", false, "Only collect pods that have not yet been scheduled to a node (used with advanced deployment mode)")
		useKubeletAPI        = flag.Bool("use-kubelet-api", false, "Use kubelet API instead of Kubernetes API for pod/container collection (requires --node and --node-ip)")
		kubeletPort          = flag.Int("kubelet-port", 10250, "Port for the kubelet API")
		nodeIP               = flag.String("node-ip", "", "IP address of the node for kubelet API access (required when --use-kubelet-api is set)")
		kubeletInsecureTLS   = flag.Bool("kubelet-insecure-skip-verify", false, "Disable kubelet TLS certificate verification (use only on trusted cluster networks)")
		leaderElect          = flag.Bool("leader-elect", false, "Enable leader election so only one replica collects and logs resource state")
		leaderElectionName   = flag.String("leader-election-lease-name", "kube-state-logs", "Name of the Lease used for leader election")
		leaderElectionNS     = flag.String("leader-election-lease-namespace", os.Getenv("POD_NAMESPACE"), "Namespace of the Lease used for leader election")
		leaderLeaseDuration  = flag.Duration("leader-election-lease-duration", config.DefaultLeaderElectionLeaseDuration, "Duration that non-leaders wait before attempting to acquire leadership")
		leaderRenewDeadline  = flag.Duration("leader-election-renew-deadline", config.DefaultLeaderElectionRenewDeadline, "Duration that the leader retries renewing its leadership")
		leaderRetryPeriod    = flag.Duration("leader-election-retry-period", config.DefaultLeaderElectionRetryPeriod, "Duration between leader election acquisition and renewal attempts")
	)
	flag.Parse()

	leaderElectionIdentity := ""
	if *leaderElect {
		var err error
		leaderElectionIdentity, err = os.Hostname()
		if err != nil {
			klog.Fatalf("Failed to determine leader election identity: %v", err)
		}
	}

	// Set log level
	if err := config.SetLogLevel(*logLevel); err != nil {
		klog.Fatalf("Failed to set log level: %v", err)
	}

	klog.Info("Starting kube-state-logs...")

	// Parse resource configurations
	resourceConfigsList, err := config.ParseResourceConfigs(*resourceConfigs, *logInterval)
	if err != nil {
		klog.Fatalf("Failed to parse resource configs: %v", err)
	}

	// If no specific resource configs provided, create default ones from resources list
	if len(resourceConfigsList) == 0 {
		resourcesList := config.ParseResourceList(*resources)
		for _, resource := range resourcesList {
			resourceConfigsList = append(resourceConfigsList, config.ResourceConfig{
				Name:     resource,
				Interval: *logInterval,
			})
		}
	}

	// Create configuration
	cfg := &config.Config{
		LogInterval:                  *logInterval,
		Resources:                    config.ParseResourceList(*resources),
		ResourceConfigs:              resourceConfigsList,
		CRDs:                         config.ParseCRDConfigs(*crdConfigs),
		Namespaces:                   config.ParseNamespaceList(*namespaces),
		Kubeconfig:                   *kubeconfig,
		ContainerEnvVars:             config.ParseContainerEnvVars(*containerEnvVars),
		ConfigMapIncludeValues:       *configMapValues,
		Node:                         *node,
		TrackUnscheduledPods:         *trackUnscheduledPods,
		UseKubeletAPI:                *useKubeletAPI,
		KubeletPort:                  *kubeletPort,
		NodeIP:                       *nodeIP,
		KubeletInsecureSkipVerify:    *kubeletInsecureTLS,
		LeaderElection:               *leaderElect,
		LeaderElectionLeaseName:      *leaderElectionName,
		LeaderElectionLeaseNamespace: *leaderElectionNS,
		LeaderElectionIdentity:       leaderElectionIdentity,
		LeaderElectionLeaseDuration:  *leaderLeaseDuration,
		LeaderElectionRenewDeadline:  *leaderRenewDeadline,
		LeaderElectionRetryPeriod:    *leaderRetryPeriod,
	}

	// Validate configuration to prevent runtime issues
	if err := cfg.Validate(); err != nil {
		klog.Fatalf("Configuration validation failed: %v", err)
	}

	// Create collector
	collector, err := collector.New(cfg)
	if err != nil {
		klog.Fatalf("Failed to create collector: %v", err)
	}

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		klog.Infof("Received signal %v, shutting down...", sig)
		cancel()
	}()

	// Start the collector
	if err := runCollector(ctx, collector, ":8080"); err != nil {
		klog.Fatalf("Collector failed: %v", err)
	}

	klog.Info("kube-state-logs stopped")
}

func runCollector(ctx context.Context, c *collector.Collector, address string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	listener, err := net.Listen("tcp", address)
	if err != nil {
		return fmt.Errorf("failed to listen for health probes: %w", err)
	}
	server := &http.Server{
		Handler:           probeHandler(ctx, c.Ready),
		ReadHeaderTimeout: 5 * time.Second,
	}
	serverErrors := make(chan error, 1)
	go func() {
		err := server.Serve(listener)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		} else if err != nil {
			err = fmt.Errorf("health probe server failed: %w", err)
		}
		serverErrors <- err
		cancel()
	}()

	runErr := c.Run(ctx)
	cancel()
	closeErr := server.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("failed to close health probe server: %w", closeErr)
	}
	return errors.Join(runErr, closeErr, <-serverErrors)
}

func probeHandler(ctx context.Context, ready func() bool) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /livez", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if ctx.Err() != nil || !ready() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	return mux
}
