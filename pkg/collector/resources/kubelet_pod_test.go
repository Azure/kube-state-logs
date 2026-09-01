// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/azure/kube-state-logs/pkg/kubelet"
	"github.com/azure/kube-state-logs/pkg/types"
)

type staticSnapshotSource struct {
	snapshot *kubelet.Snapshot
	err      error
}

func (s *staticSnapshotSource) GetSnapshot(context.Context, bool) (*kubelet.Snapshot, error) {
	return s.snapshot, s.err
}

func TestKubeletPodHandlerAppliesFiltersAndPromotesNodeLabels(t *testing.T) {
	source := &staticSnapshotSource{snapshot: &kubelet.Snapshot{Pods: []corev1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{Name: "included", Namespace: "team-a", Labels: map[string]string{"app": "api"}},
			Spec:       corev1.PodSpec{NodeName: "node-a"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "wrong-label", Namespace: "team-a", Labels: map[string]string{"app": "worker"}},
			Spec:       corev1.PodSpec{NodeName: "node-a"},
		},
		{
			ObjectMeta: metav1.ObjectMeta{Name: "wrong-namespace", Namespace: "team-b", Labels: map[string]string{"app": "api"}},
			Spec:       corev1.PodSpec{NodeName: "node-a"},
		},
	}}}
	client := fake.NewSimpleClientset(&corev1.Node{ObjectMeta: metav1.ObjectMeta{
		Name: "node-a",
		Labels: map[string]string{
			"topology.kubernetes.io/zone": "westus2-1",
			"ignored":                     "value",
		},
	}})
	handler := NewKubeletPodHandler(source, client, "node-a", "topology.kubernetes.io/zone")
	handler.SetSelectors(labels.SelectorFromSet(map[string]string{"app": "api"}), fields.OneTermEqualSelector("spec.nodeName", "node-a"))

	entries, err := handler.Collect(context.Background(), []string{"team-a"})
	if err != nil {
		t.Fatalf("Collect() error: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(entries))
	}
	podData := entries[0].(types.PodData)
	if podData.Name != "included" {
		t.Fatalf("Name = %q", podData.Name)
	}
	if podData.NodeLabels["topology.kubernetes.io/zone"] != "westus2-1" {
		t.Fatalf("NodeLabels = %#v", podData.NodeLabels)
	}
	if _, exists := podData.NodeLabels["ignored"]; exists {
		t.Fatalf("unexpected promoted label: %#v", podData.NodeLabels)
	}
}
