// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package collector

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	statsv1alpha1 "k8s.io/kubelet/pkg/apis/stats/v1alpha1"

	"github.com/azure/kube-state-logs/pkg/config"
	"github.com/azure/kube-state-logs/pkg/kubelet"
)

func setupKubeletTestServer(t *testing.T) (*httptest.Server, *config.Config) {
	t.Helper()

	now := metav1.Now()
	cpuNano := uint64(100_000_000)
	memBytes := uint64(64 * 1024 * 1024)

	podList := &corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-pod",
					Namespace:         "default",
					UID:               "uid-1",
					CreationTimestamp: now,
				},
				Spec: corev1.PodSpec{NodeName: "test-node"},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					ContainerStatuses: []corev1.ContainerStatus{
						{
							Name:  "app",
							Image: "nginx:latest",
							Ready: true,
							State: corev1.ContainerState{
								Running: &corev1.ContainerStateRunning{StartedAt: now},
							},
						},
					},
				},
			},
		},
	}

	summary := &statsv1alpha1.Summary{
		Node: statsv1alpha1.NodeStats{NodeName: "test-node"},
		Pods: []statsv1alpha1.PodStats{
			{
				PodRef: statsv1alpha1.PodReference{
					Name: "test-pod", Namespace: "default", UID: "uid-1",
				},
				Containers: []statsv1alpha1.ContainerStats{
					{
						Name:   "app",
						CPU:    &statsv1alpha1.CPUStats{UsageNanoCores: &cpuNano},
						Memory: &statsv1alpha1.MemoryStats{WorkingSetBytes: &memBytes},
					},
				},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/pods":
			json.NewEncoder(w).Encode(podList)
		case "/stats/summary":
			json.NewEncoder(w).Encode(summary)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))

	tokenDir := t.TempDir()
	tokenPath := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0600); err != nil {
		t.Fatalf("failed to write token: %v", err)
	}

	cfg := &config.Config{
		Mode:        "daemonset",
		NodeName:    "test-node",
		KubeletURL:  server.URL,
		LogInterval: 100 * time.Millisecond,
	}

	return server, cfg
}

func TestKubeletCollector_CollectAndLog(t *testing.T) {
	server, cfg := setupKubeletTestServer(t)
	defer server.Close()

	collector, err := NewKubeletCollector(cfg)
	if err != nil {
		t.Fatalf("NewKubeletCollector() error = %v", err)
	}

	// Override kubelet client to use test server
	tokenDir := t.TempDir()
	tokenPath := filepath.Join(tokenDir, "token")
	os.WriteFile(tokenPath, []byte("test-token"), 0600)
	collector.kubeletClient = kubelet.NewClient(server.URL, tokenPath)
	collector.kubeletClient.SetHTTPClient(server.Client())

	// Collect once
	ctx := context.Background()
	collector.collectAndLog(ctx)

	// No panic = success; actual output goes to stdout
	// Detailed output validation is done in handler tests
}

func TestKubeletCollector_RunWithCancellation(t *testing.T) {
	server, cfg := setupKubeletTestServer(t)
	defer server.Close()

	collector, err := NewKubeletCollector(cfg)
	if err != nil {
		t.Fatalf("NewKubeletCollector() error = %v", err)
	}

	tokenDir := t.TempDir()
	tokenPath := filepath.Join(tokenDir, "token")
	os.WriteFile(tokenPath, []byte("test-token"), 0600)
	collector.kubeletClient = kubelet.NewClient(server.URL, tokenPath)
	collector.kubeletClient.SetHTTPClient(server.Client())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- collector.Run(ctx)
	}()

	// Let it run for a couple of poll cycles
	time.Sleep(250 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return after context cancellation")
	}
}
