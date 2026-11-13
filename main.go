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
	cfg, logLevel, err := parseConfig(os.Args[0], os.Args[1:])
	if err != nil {
		klog.Fatalf("Invalid configuration: %v", err)
	}

	// Set log level
	if err := config.SetLogLevel(logLevel); err != nil {
		klog.Fatalf("Failed to set log level: %v", err)
	}

	klog.Info("Starting kube-state-logs...")

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

// parseConfig parses command-line arguments into a validated configuration and
// the requested log level. Invalid flags and -h exit as with flag.Parse.
func parseConfig(name string, args []string) (*config.Config, string, error) {
	fs := flag.NewFlagSet(name, flag.ExitOnError)
	var (
		logInterval          = fs.Duration("log-interval", 1*time.Minute, "Default interval between log outputs")
		resources            = fs.String("resources", config.DefaultResourceList, "Comma-separated list of resources to monitor")
		resourceConfigs      = fs.String("resource-configs", "", "Comma-separated list of resource configs: 'resource:interval[:labels=...][:fields=...][:promote-node-labels=label|label]'. Use '\\\\,' to escape commas in selectors (e.g., 'configmap:1m:labels=app=foo\\\\,env=prod'). If not specified, uses log-interval for all resources.")
		crdConfigs           = fs.String("crd-configs", "", "Comma-separated list of CRD configurations in the format 'apiVersion:resource[:customFields[:dropFields]]', where customFields and dropFields are '|'-separated. dropFields are dot-separated path patterns whose segments are globs ('*', '?', '[...]', '\\' escapes) or '**' for any depth (e.g., 'msi-acrpull.microsoft.com/v1:acrpullbindings:spec.acrServer|status.tokenExpirationTime:**.token')")
		namespaces           = fs.String("namespaces", "", "Comma-separated list of namespaces to monitor (empty for all)")
		logLevel             = fs.String("log-level", "info", "Log level (debug, info, warn, error)")
		kubeconfig           = fs.String("kubeconfig", "", "Path to kubeconfig file (empty for in-cluster config)")
		containerEnvVars     = fs.String("container-envvars", "", "Comma-separated list of environment variable names to capture from containers (e.g., 'GOMAXPROCS,MY_FLAG'). Empty disables capturing.")
		configMapValues      = fs.Bool("configmap-include-values", false, "Include ConfigMap data values (data only, no binary data)")
		node                 = fs.String("node", "", "Filter pods to only those scheduled on this node (used for DaemonSet deployment mode)")
		trackUnscheduledPods = fs.Bool("track-unscheduled-pods", false, "Only collect pods that have not yet been scheduled to a node (used with advanced deployment mode)")
		useKubeletAPI        = fs.Bool("use-kubelet-api", false, "Use kubelet API instead of Kubernetes API for pod/container collection (requires --node and --node-ip)")
		kubeletPort          = fs.Int("kubelet-port", 10250, "Port for the kubelet API")
		nodeIP               = fs.String("node-ip", "", "IP address of the node for kubelet API access (required when --use-kubelet-api is set)")
		kubeletInsecureTLS   = fs.Bool("kubelet-insecure-skip-verify", false, "Disable kubelet TLS certificate verification (use only on trusted cluster networks)")
		leaderElect          = fs.Bool("leader-elect", false, "Enable leader election so only one replica collects and logs resource state")
		leaderElectionName   = fs.String("leader-election-lease-name", "kube-state-logs", "Name of the Lease used for leader election")
		leaderElectionNS     = fs.String("leader-election-lease-namespace", os.Getenv("POD_NAMESPACE"), "Namespace of the Lease used for leader election")
		leaderLeaseDuration  = fs.Duration("leader-election-lease-duration", config.DefaultLeaderElectionLeaseDuration, "Duration that non-leaders wait before attempting to acquire leadership")
		leaderRenewDeadline  = fs.Duration("leader-election-renew-deadline", config.DefaultLeaderElectionRenewDeadline, "Duration that the leader retries renewing its leadership")
		leaderRetryPeriod    = fs.Duration("leader-election-retry-period", config.DefaultLeaderElectionRetryPeriod, "Duration between leader election acquisition and renewal attempts")
	)
	_ = fs.Parse(args)

	leaderElectionIdentity := ""
	if *leaderElect {
		var err error
		leaderElectionIdentity, err = os.Hostname()
		if err != nil {
			return nil, "", fmt.Errorf("failed to determine leader election identity: %w", err)
		}
	}

	// Parse resource configurations
	resourceConfigsList, err := config.ParseResourceConfigs(*resourceConfigs, *logInterval)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse resource configs: %w", err)
	}

	crdConfigsList, err := config.ParseCRDConfigs(*crdConfigs)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse CRD configs: %w", err)
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
		CRDs:                         crdConfigsList,
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

	if err := cfg.Validate(); err != nil {
		return nil, "", fmt.Errorf("configuration validation failed: %w", err)
	}
	return cfg, *logLevel, nil
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
