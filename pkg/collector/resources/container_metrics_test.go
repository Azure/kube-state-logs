package resources

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"
	clienttesting "k8s.io/client-go/testing"
	metricsv1beta1 "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	metricsfake "k8s.io/metrics/pkg/client/clientset/versioned/fake"

	"github.com/azure/kube-state-logs/pkg/types"
)

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
				ObjectMeta: metav1.ObjectMeta{Name: "local", Namespace: "default"},
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
			entries, err := handler.processPods(context.Background(), pods, []string{"default"})
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
