// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package collector

import (
	"context"
	"time"

	"k8s.io/klog/v2"

	"github.com/azure/kube-state-logs/pkg/collector/resources"
	"github.com/azure/kube-state-logs/pkg/config"
	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/kubelet"
)

// KubeletCollector collects pod and container data by polling the local kubelet API.
// It is used in daemonset mode where each node runs its own instance.
type KubeletCollector struct {
	config           *config.Config
	kubeletClient    *kubelet.Client
	logger           interfaces.Logger
	podHandler       *resources.KubeletPodHandler
	containerHandler *resources.KubeletContainerHandler
}

// NewKubeletCollector creates a new KubeletCollector instance.
func NewKubeletCollector(cfg *config.Config) (*KubeletCollector, error) {
	client := kubelet.NewClient(cfg.KubeletURL, "")
	logger := NewLogger()

	return &KubeletCollector{
		config:           cfg,
		kubeletClient:    client,
		logger:           logger,
		podHandler:       resources.NewKubeletPodHandler(),
		containerHandler: resources.NewKubeletContainerHandler(cfg.ContainerEnvVars),
	}, nil
}

// Run starts the polling loop that collects pod and container data from the kubelet.
// It blocks until the context is cancelled.
func (c *KubeletCollector) Run(ctx context.Context) error {
	interval := c.config.LogInterval
	if interval <= 0 {
		interval = time.Minute
	}

	klog.Infof("Starting kubelet collector in daemonset mode on node %s (interval: %v, kubelet: %s)",
		c.config.NodeName, interval, c.config.KubeletURL)

	// Perform initial collection immediately
	c.collectAndLog(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			klog.Info("Kubelet collector shutting down")
			return nil
		case <-ticker.C:
			c.collectAndLog(ctx)
		}
	}
}

// collectAndLog fetches data from kubelet and logs all entries.
func (c *KubeletCollector) collectAndLog(ctx context.Context) {
	// Fetch pods from kubelet
	podList, err := c.kubeletClient.GetPods(ctx)
	if err != nil {
		klog.Errorf("Failed to get pods from kubelet: %v", err)
		return
	}

	// Fetch stats from kubelet
	stats, err := c.kubeletClient.GetStatsSummary(ctx)
	if err != nil {
		// Stats are optional; continue with nil stats
		klog.Warningf("Failed to get stats from kubelet: %v", err)
		stats = nil
	}

	// Process pods
	podEntries, err := c.podHandler.Process(podList.Items)
	if err != nil {
		klog.Errorf("Failed to process pod data: %v", err)
	} else {
		for _, entry := range podEntries {
			if err := c.logger.Log(entry); err != nil {
				klog.Errorf("Failed to log pod entry: %v", err)
			}
		}
		klog.V(4).Infof("Logged %d pod entries from kubelet", len(podEntries))
	}

	// Process containers
	containerEntries, err := c.containerHandler.Process(podList.Items, stats)
	if err != nil {
		klog.Errorf("Failed to process container data: %v", err)
	} else {
		for _, entry := range containerEntries {
			if err := c.logger.Log(entry); err != nil {
				klog.Errorf("Failed to log container entry: %v", err)
			}
		}
		klog.V(4).Infof("Logged %d container entries from kubelet", len(containerEntries))
	}
}
