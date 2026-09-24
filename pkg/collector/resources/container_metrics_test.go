// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"

	"github.com/azure/kube-state-logs/pkg/types"
)

func TestContainerHandlerDoesNotReusePreviousMetrics(t *testing.T) {
	for _, test := range []struct {
		name     string
		canceled bool
		recreate bool
	}{
		{name: "failed request"},
		{name: "canceled collection", canceled: true},
		{name: "recreated pod with failed request", recreate: true},
		{name: "recreated pod with canceled collection", canceled: true, recreate: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			pod := createTestPodWithContainers("local", "default", []corev1.Container{{Name: "app"}})
			pod.Spec.NodeName = "node-a"
			pod.Spec.InitContainers = []corev1.Container{{Name: "setup"}}
			pod.Status.InitContainerStatuses = []corev1.ContainerStatus{{Name: "setup", State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}
			metrics := &metricsv1beta1.PodMetrics{Name: pod.Name, Namespace: pod.Namespace}
			for _, name := range []string{"app", "setup"} {
				metrics.Containers = append(metrics.Containers, metricsv1beta1.ContainerMetrics{Name: name, Usage: corev1.ResourceList{
					corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("32Mi"),
				}})
			}
			metricsClient := metricsfake.NewSimpleClientset()
			metricsResource := metricsv1beta1.SchemeGroupVersion.WithResource("pods")
			if err := metricsClient.Tracker().Create(metricsResource, metrics, pod.Namespace); err != nil {
				t.Fatal(err)
			}
			handler := NewContainerHandler(fake.NewSimpleClientset(), metricsClient, nil)
			handler.SetNodeFilter("node-a")
			first, err := handler.processPods(t.Context(), []any{pod}, nil)
			if err != nil || len(first) != 2 {
				t.Fatalf("first collection: entries=%d, error=%v", len(first), err)
			}
			for _, entry := range first {
				data := entry.(types.ContainerData)
				if data.UsageCPUMillicore == nil || *data.UsageCPUMillicore != 100 || data.UsageMemoryBytes == nil || *data.UsageMemoryBytes != 32*1024*1024 {
					t.Fatalf("first collection missing expected usage: %#v", data)
				}
			}
			if len(handler.metricsCache.ListKeys()) != 0 {
				t.Fatal("metrics cache retained entries after collection")
			}
			if err := metricsClient.Tracker().Delete(metricsResource, pod.Namespace, pod.Name); err != nil {
				t.Fatal(err)
			}
			if test.recreate {
				pod = pod.DeepCopy()
				pod.UID = "replacement-uid"
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if test.canceled {
				cancel()
			}
			second, err := handler.processPods(ctx, []any{pod}, nil)
			if err != nil || len(second) != 2 {
				t.Fatalf("second collection: entries=%d, error=%v", len(second), err)
			}
			for _, entry := range second {
				data := entry.(types.ContainerData)
				if data.PodUID != string(pod.UID) || data.UsageCPUMillicore != nil || data.UsageMemoryBytes != nil {
					t.Fatalf("second collection has stale identity or usage: %#v", data)
				}
			}
		})
	}
}

func TestContainerHandlerMetricsScope(t *testing.T) {
	for _, test := range []struct {
		name           string
		nodeName       string
		emptyPods      bool
		missingMetrics bool
		wantVerb       string
		wantActions    int
	}{
		{name: "node-local GET", nodeName: "node-a", wantVerb: "get", wantActions: 1},
		{name: "cluster-wide list", wantVerb: "list", wantActions: 1},
		{name: "empty local cache", nodeName: "node-a", emptyPods: true},
		{name: "missing local metrics", nodeName: "node-a", missingMetrics: true, wantVerb: "get", wantActions: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			localPod := createTestPodWithContainers("local", "default", []corev1.Container{{Name: "app"}})
			localPod.Spec.NodeName = "node-a"
			remotePod := localPod.DeepCopy()
			remotePod.Name = "remote"
			remotePod.Spec.NodeName = "node-b"
			otherNamespace := localPod.DeepCopy()
			otherNamespace.Namespace = "excluded"
			otherLabel := localPod.DeepCopy()
			otherLabel.Name = "other-label"
			otherLabel.Labels = map[string]string{"app": "excluded"}
			otherField := localPod.DeepCopy()
			otherField.Name = "other-field"
			podMetrics := &metricsv1beta1.PodMetrics{
				Name: "local", Namespace: "default",
				Containers: []metricsv1beta1.ContainerMetrics{{Name: "app", Usage: corev1.ResourceList{
					corev1.ResourceCPU:    resource.MustParse("100m"),
					corev1.ResourceMemory: resource.MustParse("128Mi"),
				}}},
			}
			metricsClient := metricsfake.NewSimpleClientset()
			if !test.missingMetrics {
				if err := metricsClient.Tracker().Create(metricsv1beta1.SchemeGroupVersion.WithResource("pods"), podMetrics, "default"); err != nil {
					t.Fatal(err)
				}
			}
			handler := NewContainerHandler(fake.NewSimpleClientset(), metricsClient, nil)
			handler.SetNodeFilter(test.nodeName)
			handler.SetSelectors(labels.SelectorFromSet(labels.Set{"app": "local"}), fields.OneTermNotEqualSelector("metadata.name", "other-field"))
			pods := []any{localPod, remotePod, otherNamespace, otherLabel, otherField}
			if test.emptyPods {
				pods = nil
			}
			entries, err := handler.processPods(t.Context(), pods, []string{"default"})
			if err != nil {
				t.Fatal(err)
			}
			actions := metricsClient.Actions()
			if len(actions) != test.wantActions {
				t.Fatalf("metrics actions = %#v, want %d", actions, test.wantActions)
			}
			for _, action := range actions {
				if action.GetVerb() != test.wantVerb || action.GetNamespace() != "default" {
					t.Fatalf("unexpected metrics action: %#v", action)
				}
				if getAction, ok := action.(clienttesting.GetAction); ok && getAction.GetName() != "local" {
					t.Fatalf("requested metrics for %q, want local", getAction.GetName())
				}
			}
			if test.emptyPods {
				if len(entries) != 0 {
					t.Fatalf("entries = %d, want 0", len(entries))
				}
				return
			}
			wantEntries := 1
			if test.nodeName == "" {
				wantEntries = 2
			}
			if len(entries) != wantEntries {
				t.Fatalf("entries = %d, want %d", len(entries), wantEntries)
			}
			entry := entries[0].(types.ContainerData)
			if test.missingMetrics {
				if entry.UsageCPUMillicore != nil || entry.UsageMemoryBytes != nil {
					t.Fatalf("unexpected usage without metrics: %#v", entry)
				}
			} else if entry.UsageCPUMillicore == nil || *entry.UsageCPUMillicore != 100 || entry.UsageMemoryBytes == nil || *entry.UsageMemoryBytes != 128*1024*1024 {
				t.Fatalf("unexpected container usage: %#v", entry)
			}
		})
	}
}
