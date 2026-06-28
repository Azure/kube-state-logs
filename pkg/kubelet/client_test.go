// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package kubelet

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	statsv1alpha1 "k8s.io/kubelet/pkg/apis/stats/v1alpha1"
)

func setupTestServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	server := httptest.NewServer(handler)

	// Create a temp token file
	tokenDir := t.TempDir()
	tokenPath := filepath.Join(tokenDir, "token")
	if err := os.WriteFile(tokenPath, []byte("test-token"), 0600); err != nil {
		t.Fatalf("failed to write token file: %v", err)
	}

	client := NewClient(server.URL, tokenPath)
	// Override to use plain HTTP for tests
	client.httpClient = server.Client()

	return server, client
}

func TestGetPods(t *testing.T) {
	podList := &corev1.PodList{
		Items: []corev1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-pod",
					Namespace: "default",
					UID:       "uid-1",
				},
				Spec: corev1.PodSpec{
					NodeName: "test-node",
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
				},
			},
		},
	}

	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/pods" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected Authorization header: %s", r.Header.Get("Authorization"))
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(podList)
	})
	defer server.Close()

	result, err := client.GetPods(context.Background())
	if err != nil {
		t.Fatalf("GetPods() error = %v", err)
	}

	if len(result.Items) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(result.Items))
	}

	if result.Items[0].Name != "test-pod" {
		t.Errorf("expected pod name 'test-pod', got %q", result.Items[0].Name)
	}

	if result.Items[0].Spec.NodeName != "test-node" {
		t.Errorf("expected node name 'test-node', got %q", result.Items[0].Spec.NodeName)
	}
}

func TestGetStatsSummary(t *testing.T) {
	cpuNanoCores := uint64(500_000_000) // 500m
	memWorkingSet := uint64(1024 * 1024 * 256)
	summary := &statsv1alpha1.Summary{
		Node: statsv1alpha1.NodeStats{
			NodeName: "test-node",
			CPU: &statsv1alpha1.CPUStats{
				UsageNanoCores: &cpuNanoCores,
			},
			Memory: &statsv1alpha1.MemoryStats{
				WorkingSetBytes: &memWorkingSet,
			},
		},
		Pods: []statsv1alpha1.PodStats{
			{
				PodRef: statsv1alpha1.PodReference{
					Name:      "test-pod",
					Namespace: "default",
					UID:       "uid-1",
				},
				Containers: []statsv1alpha1.ContainerStats{
					{
						Name: "app",
						CPU: &statsv1alpha1.CPUStats{
							UsageNanoCores: &cpuNanoCores,
						},
						Memory: &statsv1alpha1.MemoryStats{
							WorkingSetBytes: &memWorkingSet,
						},
					},
				},
			},
		},
	}

	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats/summary" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(summary)
	})
	defer server.Close()

	result, err := client.GetStatsSummary(context.Background())
	if err != nil {
		t.Fatalf("GetStatsSummary() error = %v", err)
	}

	if result.Node.NodeName != "test-node" {
		t.Errorf("expected node name 'test-node', got %q", result.Node.NodeName)
	}

	if len(result.Pods) != 1 {
		t.Fatalf("expected 1 pod, got %d", len(result.Pods))
	}

	if result.Pods[0].PodRef.Name != "test-pod" {
		t.Errorf("expected pod name 'test-pod', got %q", result.Pods[0].PodRef.Name)
	}

	if result.Pods[0].Containers[0].CPU.UsageNanoCores == nil || *result.Pods[0].Containers[0].CPU.UsageNanoCores != 500_000_000 {
		t.Error("expected container CPU nanocores to be 500000000")
	}
}

func TestGetPodsServerError(t *testing.T) {
	server, client := setupTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	})
	defer server.Close()

	_, err := client.GetPods(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestGetPodsMissingToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"items":[]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "/nonexistent/token/path")
	client.httpClient = server.Client()

	_, err := client.GetPods(context.Background())
	if err == nil {
		t.Fatal("expected error when token file is missing")
	}
}

func TestNanoCoresToMilliCores(t *testing.T) {
	tests := []struct {
		name       string
		nanoCores  uint64
		milliCores int64
	}{
		{"zero", 0, 0},
		{"500m", 500_000_000, 500},
		{"1 core", 1_000_000_000, 1000},
		{"250m", 250_000_000, 250},
		{"sub-millicore rounds down", 999_999, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NanoCoresToMilliCores(tt.nanoCores)
			if result != tt.milliCores {
				t.Errorf("NanoCoresToMilliCores(%d) = %d, want %d", tt.nanoCores, result, tt.milliCores)
			}
		})
	}
}
