// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/azure/kube-state-logs/pkg/types"
)

func TestKubeletPodHandler_Process(t *testing.T) {
	now := metav1.Now()
	handler := NewKubeletPodHandler()

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-pod",
				Namespace: "default",
				UID:       "uid-1",
				Labels:    map[string]string{"app": "test"},
				CreationTimestamp: now,
				OwnerReferences: []metav1.OwnerReference{
					{
						Kind: "ReplicaSet",
						Name: "test-rs",
					},
				},
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
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("200m"),
								corev1.ResourceMemory: resource.MustParse("256Mi"),
							},
						},
					},
				},
				RestartPolicy:      corev1.RestartPolicyAlways,
				ServiceAccountName: "default",
				SchedulerName:      "default-scheduler",
			},
			Status: corev1.PodStatus{
				Phase:     corev1.PodRunning,
				HostIP:    "192.168.1.1",
				PodIP:     "10.0.0.1",
				StartTime: &now,
				QOSClass:  corev1.PodQOSBurstable,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Name:         "app",
						Ready:        true,
						RestartCount: 2,
						State: corev1.ContainerState{
							Running: &corev1.ContainerStateRunning{StartedAt: now},
						},
					},
				},
				Conditions: []corev1.PodCondition{
					{Type: corev1.PodReady, Status: corev1.ConditionTrue, LastTransitionTime: now},
					{Type: corev1.PodInitialized, Status: corev1.ConditionTrue, LastTransitionTime: now},
					{Type: corev1.PodScheduled, Status: corev1.ConditionTrue, LastTransitionTime: now},
				},
			},
		},
	}

	entries, err := handler.Process(pods)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	podData, ok := entries[0].(types.PodData)
	if !ok {
		t.Fatalf("expected types.PodData, got %T", entries[0])
	}

	if podData.ResourceType != "pod" {
		t.Errorf("ResourceType = %q, want %q", podData.ResourceType, "pod")
	}
	if podData.Name != "test-pod" {
		t.Errorf("Name = %q, want %q", podData.Name, "test-pod")
	}
	if podData.Namespace != "default" {
		t.Errorf("Namespace = %q, want %q", podData.Namespace, "default")
	}
	if podData.NodeName != "test-node" {
		t.Errorf("NodeName = %q, want %q", podData.NodeName, "test-node")
	}
	if podData.Phase != "Running" {
		t.Errorf("Phase = %q, want %q", podData.Phase, "Running")
	}
	if podData.QoSClass != "Burstable" {
		t.Errorf("QoSClass = %q, want %q", podData.QoSClass, "Burstable")
	}
	if podData.RestartCount != 2 {
		t.Errorf("RestartCount = %d, want %d", podData.RestartCount, 2)
	}
	if podData.CreatedByKind != "ReplicaSet" {
		t.Errorf("CreatedByKind = %q, want %q", podData.CreatedByKind, "ReplicaSet")
	}
	if podData.CreatedByName != "test-rs" {
		t.Errorf("CreatedByName = %q, want %q", podData.CreatedByName, "test-rs")
	}
	if podData.Ready == nil || !*podData.Ready {
		t.Error("Ready should be true")
	}
	if podData.TotalRequestsCPUMillicore == nil || *podData.TotalRequestsCPUMillicore != 100 {
		t.Errorf("TotalRequestsCPUMillicore = %v, want 100", podData.TotalRequestsCPUMillicore)
	}
}

func TestKubeletPodHandler_ProcessEmpty(t *testing.T) {
	handler := NewKubeletPodHandler()

	entries, err := handler.Process(nil)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestKubeletPodHandler_QoSDefault(t *testing.T) {
	handler := NewKubeletPodHandler()

	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "best-effort-pod",
				Namespace: "default",
				CreationTimestamp: metav1.Now(),
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				// QOSClass intentionally not set
			},
		},
	}

	entries, err := handler.Process(pods)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	podData := entries[0].(types.PodData)
	if podData.QoSClass != "BestEffort" {
		t.Errorf("QoSClass = %q, want %q", podData.QoSClass, "BestEffort")
	}
}
