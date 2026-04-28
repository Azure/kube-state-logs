// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	statsv1alpha1 "k8s.io/kubelet/pkg/apis/stats/v1alpha1"

	"github.com/azure/kube-state-logs/pkg/types"
)

func TestKubeletContainerHandler_Process(t *testing.T) {
	now := metav1.Now()
	handler := NewKubeletContainerHandler(nil)

	cpuNano := uint64(250_000_000) // 250m
	memBytes := uint64(128 * 1024 * 1024)

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-pod",
				Namespace:         "default",
				UID:               "uid-1",
				CreationTimestamp: now,
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
				Containers: []corev1.Container{
					{
						Name:  "app",
						Image: "nginx:latest",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:         "app",
						Ready:        true,
						RestartCount: 1,
						Image:        "nginx:latest",
						ImageID:      "docker://sha256:abc123",
						ContainerID:  "containerd://def456",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{StartedAt: now},
						},
					},
				},
			},
		},
	}

	stats := &statsv1alpha1.Summary{
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
							UsageNanoCores: &cpuNano,
						},
						Memory: &statsv1alpha1.MemoryStats{
							WorkingSetBytes: &memBytes,
						},
					},
				},
			},
		},
	}

	entries, err := handler.Process(pods, stats)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	cd, ok := entries[0].(types.ContainerData)
	if !ok {
		t.Fatalf("expected types.ContainerData, got %T", entries[0])
	}

	if cd.ResourceType != "container" {
		t.Errorf("ResourceType = %q, want %q", cd.ResourceType, "container")
	}
	if cd.Name != "app" {
		t.Errorf("Name = %q, want %q", cd.Name, "app")
	}
	if cd.PodName != "test-pod" {
		t.Errorf("PodName = %q, want %q", cd.PodName, "test-pod")
	}
	if cd.NodeName != "test-node" {
		t.Errorf("NodeName = %q, want %q", cd.NodeName, "test-node")
	}
	if cd.State != ContainerStateRunning {
		t.Errorf("State = %q, want %q", cd.State, ContainerStateRunning)
	}
	if cd.RestartCount != 1 {
		t.Errorf("RestartCount = %d, want %d", cd.RestartCount, 1)
	}
	if cd.UsageCPUMillicore == nil || *cd.UsageCPUMillicore != 250 {
		t.Errorf("UsageCPUMillicore = %v, want 250", cd.UsageCPUMillicore)
	}
	if cd.UsageMemoryBytes == nil || *cd.UsageMemoryBytes != int64(128*1024*1024) {
		t.Errorf("UsageMemoryBytes = %v, want %d", cd.UsageMemoryBytes, 128*1024*1024)
	}
	if cd.RequestsCPUMillicore == nil || *cd.RequestsCPUMillicore != 100 {
		t.Errorf("RequestsCPUMillicore = %v, want 100", cd.RequestsCPUMillicore)
	}
}

func TestKubeletContainerHandler_NilStats(t *testing.T) {
	now := metav1.Now()
	handler := NewKubeletContainerHandler(nil)

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "test-pod",
				Namespace:         "default",
				UID:               "uid-1",
				CreationTimestamp: now,
			},
			Spec: corev1.PodSpec{NodeName: "test-node"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name: "app",
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{StartedAt: now},
						},
					},
				},
			},
		},
	}

	entries, err := handler.Process(pods, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	cd := entries[0].(types.ContainerData)
	if cd.UsageCPUMillicore != nil {
		t.Errorf("UsageCPUMillicore should be nil when no stats, got %v", cd.UsageCPUMillicore)
	}
	if cd.UsageMemoryBytes != nil {
		t.Errorf("UsageMemoryBytes should be nil when no stats, got %v", cd.UsageMemoryBytes)
	}
}

func TestKubeletContainerHandler_StateTracking(t *testing.T) {
	now := metav1.Now()
	handler := NewKubeletContainerHandler(nil)

	// First collection: running container
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pod", Namespace: "default", UID: "uid-1",
				CreationTimestamp: now,
			},
			Spec: corev1.PodSpec{NodeName: "test-node"},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}},
					},
				},
			},
		},
	}

	entries, err := handler.Process(pods, nil)
	if err != nil {
		t.Fatalf("First Process() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("first collection: expected 1 entry, got %d", len(entries))
	}

	// Second collection: container has terminated
	recentFinish := metav1.Now()
	pods[0].Status.ContainerStatuses[0].State = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{
			ExitCode:   1,
			Reason:     "Error",
			FinishedAt: recentFinish,
		},
	}

	entries, err = handler.Process(pods, nil)
	if err != nil {
		t.Fatalf("Second Process() error = %v", err)
	}
	// Should log the newly terminated container (transitioned from running)
	if len(entries) != 1 {
		t.Fatalf("second collection: expected 1 terminated entry, got %d", len(entries))
	}

	cd := entries[0].(types.ContainerData)
	if cd.State != ContainerStateTerminated {
		t.Errorf("State = %q, want %q", cd.State, ContainerStateTerminated)
	}

	// Third collection: same terminated container - should NOT be logged again
	entries, err = handler.Process(pods, nil)
	if err != nil {
		t.Fatalf("Third Process() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("third collection: expected 0 entries (already logged), got %d", len(entries))
	}
}

func TestKubeletContainerHandler_InitContainers(t *testing.T) {
	now := metav1.Now()
	handler := NewKubeletContainerHandler(nil)

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pod", Namespace: "default", UID: "uid-1",
				CreationTimestamp: now,
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
				InitContainers: []corev1.Container{
					{Name: "init-app", Image: "busybox:latest"},
				},
			},
			Status: corev1.PodStatus{
				InitContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "init-app",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}},
					},
				},
			},
		},
	}

	entries, err := handler.Process(pods, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	cd := entries[0].(types.ContainerData)
	if cd.ResourceType != "init_container" {
		t.Errorf("ResourceType = %q, want %q", cd.ResourceType, "init_container")
	}
	if cd.Name != "init-app" {
		t.Errorf("Name = %q, want %q", cd.Name, "init-app")
	}
}

func TestKubeletContainerHandler_EnvVarFilter(t *testing.T) {
	now := metav1.Now()
	handler := NewKubeletContainerHandler([]string{"GOMAXPROCS"})

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-pod", Namespace: "default", UID: "uid-1",
				CreationTimestamp: now,
			},
			Spec: corev1.PodSpec{
				NodeName: "test-node",
				Containers: []corev1.Container{
					{
						Name: "app",
						Env: []corev1.EnvVar{
							{Name: "GOMAXPROCS", Value: "4"},
							{Name: "SECRET", Value: "hidden"},
						},
					},
				},
			},
			Status: corev1.PodStatus{
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:  "app",
						State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: now}},
					},
				},
			},
		},
	}

	entries, err := handler.Process(pods, nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	cd := entries[0].(types.ContainerData)
	if cd.EnvironmentVariables == nil {
		t.Fatal("EnvironmentVariables should not be nil")
	}
	if cd.EnvironmentVariables["GOMAXPROCS"] != "4" {
		t.Errorf("GOMAXPROCS = %q, want %q", cd.EnvironmentVariables["GOMAXPROCS"], "4")
	}
	if _, ok := cd.EnvironmentVariables["SECRET"]; ok {
		t.Error("SECRET should not be captured")
	}
}
