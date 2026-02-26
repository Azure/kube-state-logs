// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package main

import (
	"context"
	"flag"
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
		logInterval        = flag.Duration("log-interval", 1*time.Minute, "Default interval between log outputs")
		resources          = flag.String("resources", "pod,container,service,node,deployment,job,cronjob,configmap,secret,persistentvolumeclaim,ingress,horizontalpodautoscaler,serviceaccount,endpoints,persistentvolume,resourcequota,poddisruptionbudget,storageclass,networkpolicy,replicationcontroller,limitrange,lease,role,clusterrole,rolebinding,clusterrolebinding,volumeattachment,certificatesigningrequest,namespace,daemonset,statefulset,replicaset,mutatingwebhookconfiguration,validatingwebhookconfiguration,ingressclass,priorityclass,runtimeclass,validatingadmissionpolicy,validatingadmissionpolicybinding", "Comma-separated list of resources to monitor")
		resourceConfigs    = flag.String("resource-configs", "", "Comma-separated list of resource:interval pairs (e.g., 'deployments:5m,pods:1m,services:2m'). If not specified, uses log-interval for all resources.")
		crdConfigs         = flag.String("crd-configs", "", "Comma-separated list of CRD configurations (e.g., 'msi-acrpull.microsoft.com/v1:acrpullbindings:spec.acrServer|spec.managedIdentityResourceID|status.lastTokenRefreshTime|status.tokenExpirationTime')")
		namespaces         = flag.String("namespaces", "", "Comma-separated list of namespaces to monitor (empty for all)")
		logLevel           = flag.String("log-level", "info", "Log level (debug, info, warn, error)")
		kubeconfig         = flag.String("kubeconfig", "", "Path to kubeconfig file (empty for in-cluster config)")
		containerEnvVars   = flag.String("container-envvars", "", "Comma-separated list of environment variable names to capture from containers (e.g., 'GOMAXPROCS,MY_FLAG'). Empty disables capturing.")
		enableEventLogging = flag.Bool("enable-event-logging", false, "Enable immediate log entries on resource creation and deletion")
	)
	flag.Parse()

	// Set log level
	if err := config.SetLogLevel(*logLevel); err != nil {
		klog.Fatalf("Failed to set log level: %v", err)
	}

	klog.Info("Starting kube-state-logs...")

	// Parse resource configurations
	resourceConfigsList := config.ParseResourceConfigs(*resourceConfigs, *logInterval)

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
		LogInterval:        *logInterval,
		Resources:          config.ParseResourceList(*resources),
		ResourceConfigs:    resourceConfigsList,
		CRDs:               config.ParseCRDConfigs(*crdConfigs),
		Namespaces:         config.ParseNamespaceList(*namespaces),
		Kubeconfig:         *kubeconfig,
		ContainerEnvVars:   config.ParseContainerEnvVars(*containerEnvVars),
		EnableEventLogging: *enableEventLogging,
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
	if err := collector.Run(ctx); err != nil {
		klog.Fatalf("Collector failed: %v", err)
	}

	klog.Info("kube-state-logs stopped")
}
