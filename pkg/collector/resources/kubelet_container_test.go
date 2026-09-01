// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/azure/kube-state-logs/pkg/kubelet"
	"github.com/azure/kube-state-logs/pkg/types"
)

func TestKubeletContainerHandlerUsesLocalStatsAndPreservesFields(t *testing.T) {
	started := metav1.Now()
	cpu := uint64(250_000_000)
	memory := uint64(128 * 1024 * 1024)
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "team-a", UID: "uid-a", Labels: map[string]string{"app": "api"}},
		Spec: corev1.PodSpec{
			NodeName: "node-a",
			Containers: []corev1.Container{{
				Name:            "app",
				ImagePullPolicy: corev1.PullIfNotPresent,
				Env: []corev1.EnvVar{
					{Name: "VISIBLE", Value: "yes"},
					{Name: "HIDDEN", Value: "no"},
				},
			}},
		},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "app",
			Image: "example/app:v1",
			Ready: true,
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: started}},
		}}},
	}
	source := &staticSnapshotSource{snapshot: &kubelet.Snapshot{
		Pods: []corev1.Pod{pod},
		Stats: &kubelet.StatsSummary{Pods: []kubelet.PodStats{{
			PodRef: kubelet.PodReference{UID: "uid-a"},
			Containers: []kubelet.ContainerStats{{
				Name:   "app",
				CPU:    &kubelet.CPUStats{UsageNanoCores: &cpu},
				Memory: &kubelet.MemoryStats{WorkingSetBytes: &memory},
			}},
		}}},
	}}
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name:   "node-a",
		Labels: map[string]string{"kubernetes.io/arch": "amd64"},
	}})
	handler := NewKubeletContainerHandler(source, client, "node-a", []string{"VISIBLE"}, "kubernetes.io/arch")
	handler.SetSelectors(labels.SelectorFromSet(map[string]string{"app": "api"}), fields.Everything())

	entries, err := handler.Collect(context.Background(), []string{"team-a"})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	data := entries[0].(types.ContainerData)
	if data.PodUID != "uid-a" || data.ImagePullPolicy != string(corev1.PullIfNotPresent) {
		t.Fatalf("output identity fields = %#v", data)
	}
	if data.NodeLabels["kubernetes.io/arch"] != "amd64" {
		t.Fatalf("NodeLabels = %#v", data.NodeLabels)
	}
	if data.UsageCPUMillicore == nil || *data.UsageCPUMillicore != 250 {
		t.Fatalf("UsageCPUMillicore = %v", data.UsageCPUMillicore)
	}
	if data.UsageMemoryBytes == nil || *data.UsageMemoryBytes != int64(memory) {
		t.Fatalf("UsageMemoryBytes = %v", data.UsageMemoryBytes)
	}
	if len(data.EnvironmentVariables) != 1 || data.EnvironmentVariables["VISIBLE"] != "yes" {
		t.Fatalf("EnvironmentVariables = %#v", data.EnvironmentVariables)
	}
}

func TestKubeletContainerHandlerTracksRecreatedPodsByUID(t *testing.T) {
	handler := NewKubeletContainerHandler(&staticSnapshotSource{}, nil, "", nil)
	finished := metav1.NewTime(time.Now())
	terminatedPod := func(uid string) corev1.Pod {
		return corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "same-name", Namespace: "default", UID: k8stypes.UID(uid)},
			Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
				Name: "app",
				State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{
					FinishedAt: finished,
				}},
			}}},
		}
	}

	if entries := handler.processPods([]corev1.Pod{terminatedPod("uid-1")}, nil, nil); len(entries) != 1 {
		t.Fatalf("first pod entries = %d, want 1", len(entries))
	}
	if entries := handler.processPods([]corev1.Pod{terminatedPod("uid-2")}, nil, nil); len(entries) != 1 {
		t.Fatalf("recreated pod entries = %d, want 1", len(entries))
	}
}

func TestKubeletContainerHandlerAppliesNamespaceAndSelectors(t *testing.T) {
	running := corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}
	pods := []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "included", Namespace: "team-a", UID: "uid-1", Labels: map[string]string{"app": "api"}},
			Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", State: running}}},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "excluded", Namespace: "team-b", UID: "uid-2", Labels: map[string]string{"app": "api"}},
			Status:     corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{Name: "app", State: running}}},
		},
	}
	handler := NewKubeletContainerHandler(&staticSnapshotSource{snapshot: &kubelet.Snapshot{Pods: pods}}, nil, "", nil)
	handler.SetSelectors(labels.SelectorFromSet(map[string]string{"app": "api"}), fields.Everything())

	entries, err := handler.Collect(context.Background(), []string{"team-a"})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if len(entries) != 1 || entries[0].(types.ContainerData).PodName != "included" {
		t.Fatalf("entries = %#v", entries)
	}
}
