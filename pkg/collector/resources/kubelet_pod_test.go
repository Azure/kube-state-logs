// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"maps"
	"reflect"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/azure/kube-state-logs/pkg/kubelet"
	"github.com/azure/kube-state-logs/pkg/types"
)

type staticSnapshotSource struct {
	snapshot *kubelet.Snapshot
	err      error
}

func TestKubeletPodHandlerFiltersLocalAnnotations(t *testing.T) {
	for _, test := range []struct {
		name        string
		annotations map[string]string
		want        map[string]string
	}{
		{name: "nil"},
		{name: "empty", annotations: map[string]string{}},
		{
			name: "local only",
			annotations: map[string]string{
				"kubernetes.io/config.seen":   "2026-09-09T00:00:00Z",
				"kubernetes.io/config.source": "api",
			},
		},
		{
			name: "preserve API annotations",
			annotations: map[string]string{
				"kubernetes.io/config.seen":        "2026-09-09T00:00:00Z",
				"kubernetes.io/config.source":      "file",
				"kubernetes.io/config.hash":        "hash",
				"kubernetes.io/config.mirror":      "hash",
				"example.com/custom":               "value",
				corev1.LastAppliedConfigAnnotation: "{}",
			},
			want: map[string]string{
				"kubernetes.io/config.hash":   "hash",
				"kubernetes.io/config.mirror": "hash",
				"example.com/custom":          "value",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := maps.Clone(test.annotations)
			source := &staticSnapshotSource{snapshot: &kubelet.Snapshot{Pods: []corev1.Pod{{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Annotations: test.annotations},
			}}}}
			handler := NewKubeletPodHandler(source, nil, "")
			entries, err := handler.Collect(context.Background(), nil)
			if err != nil || len(entries) != 1 {
				t.Fatalf("Collect() = %v, %v; want one entry", entries, err)
			}
			if got := entries[0].(types.PodData).Annotations; !reflect.DeepEqual(got, test.want) {
				t.Errorf("Annotations = %#v, want %#v", got, test.want)
			}
			if !reflect.DeepEqual(source.snapshot.Pods[0].Annotations, original) {
				t.Fatal("Collect() mutated snapshot annotations")
			}
			apiEntry := CreatePodLogEntry(&source.snapshot.Pods[0], nil)
			for _, key := range []string{"kubernetes.io/config.seen", "kubernetes.io/config.source"} {
				if apiEntry.Annotations[key] != original[key] {
					t.Errorf("shared formatter changed annotation %q", key)
				}
			}
		})
	}
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

func TestKubeletPodHandlerCancelsNodeLabelLookup(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	source := &staticSnapshotSource{snapshot: &kubelet.Snapshot{Pods: []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "default"},
		Spec:       corev1.PodSpec{NodeName: "node-a"},
	}}}}
	client := fake.NewSimpleClientset()
	lookupStarted := make(chan struct{})
	client.PrependReactor("get", "nodes", func(k8stesting.Action) (bool, runtime.Object, error) {
		close(lookupStarted)
		<-ctx.Done()
		return true, nil, ctx.Err()
	})
	handler := NewKubeletPodHandler(source, client, "node-a", "kubernetes.io/arch")

	done := make(chan error, 1)
	go func() {
		_, err := handler.Collect(ctx, nil)
		done <- err
	}()

	select {
	case <-lookupStarted:
	case <-time.After(time.Second):
		t.Fatal("node label lookup did not start")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Collect() error after cancellation: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Collect() did not stop after cancellation")
	}
}
