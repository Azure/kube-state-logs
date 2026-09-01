// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package kubelet

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type fakeKubeletClient struct {
	mu         sync.Mutex
	podCalls   int
	statsCalls int
	pods       []corev1.Pod
	stats      *StatsSummary
	statsErr   error
}

func (f *fakeKubeletClient) GetPods(context.Context) ([]corev1.Pod, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.podCalls++
	return f.pods, nil
}

func (f *fakeKubeletClient) GetStatsSummary(context.Context) (*StatsSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statsCalls++
	return f.stats, f.statsErr
}

func TestCachedSnapshotSourceCoalescesRequests(t *testing.T) {
	client := &fakeKubeletClient{
		pods:  []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod-a"}}},
		stats: &StatsSummary{},
	}
	source := NewCachedSnapshotSource(client, time.Minute)

	first, err := source.GetSnapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("first GetSnapshot() error: %v", err)
	}
	second, err := source.GetSnapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("second GetSnapshot() error: %v", err)
	}
	if len(first.Pods) != 1 || len(second.Pods) != 1 {
		t.Fatalf("cached snapshots = %#v, %#v", first, second)
	}
	if client.podCalls != 1 || client.statsCalls != 1 {
		t.Fatalf("calls = pods:%d stats:%d, want 1 each", client.podCalls, client.statsCalls)
	}
}

func TestCachedSnapshotSourceKeepsPodsWhenStatsFail(t *testing.T) {
	client := &fakeKubeletClient{
		pods:     []corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "pod-a"}}},
		statsErr: errors.New("stats unavailable"),
	}
	snapshot, err := NewCachedSnapshotSource(client, 0).GetSnapshot(context.Background(), true)
	if err != nil {
		t.Fatalf("GetSnapshot() error: %v", err)
	}
	if len(snapshot.Pods) != 1 || snapshot.StatsError == nil {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestCachedSnapshotSourceSkipsStatsWhenContainersDisabled(t *testing.T) {
	client := &fakeKubeletClient{pods: []corev1.Pod{{}}}
	if _, err := NewCachedSnapshotSource(client, 0).GetSnapshot(context.Background(), false); err != nil {
		t.Fatalf("GetSnapshot() error: %v", err)
	}
	if client.statsCalls != 0 {
		t.Fatalf("stats calls = %d, want 0", client.statsCalls)
	}
}

func TestCachedSnapshotSourceCachesEmptyPodList(t *testing.T) {
	client := &fakeKubeletClient{}
	source := NewCachedSnapshotSource(client, time.Minute)
	if _, err := source.GetSnapshot(context.Background(), false); err != nil {
		t.Fatalf("first GetSnapshot() error: %v", err)
	}
	if _, err := source.GetSnapshot(context.Background(), true); err != nil {
		t.Fatalf("second GetSnapshot() error: %v", err)
	}
	if client.podCalls != 1 {
		t.Fatalf("pod calls = %d, want 1", client.podCalls)
	}
}
