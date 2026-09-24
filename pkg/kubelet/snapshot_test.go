// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package kubelet

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
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
		pods:  []corev1.Pod{{Name: "pod-a"}},
		stats: &StatsSummary{},
	}
	source := NewCachedSnapshotSource(client, time.Minute)

	first, err := source.GetSnapshot(t.Context(), true)
	if err != nil {
		t.Fatalf("first GetSnapshot() error: %v", err)
	}
	second, err := source.GetSnapshot(t.Context(), true)
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

func TestCachedSnapshotSourceRefreshesStatsWithPods(t *testing.T) {
	for _, podOnlyRefresh := range []bool{false, true} {
		t.Run(fmt.Sprintf("podOnlyRefresh=%t", podOnlyRefresh), func(t *testing.T) {
			client := &fakeKubeletClient{
				pods:  []corev1.Pod{{Name: "old-pod"}},
				stats: &StatsSummary{},
			}
			source := NewCachedSnapshotSource(client, time.Hour)
			if _, err := source.GetSnapshot(t.Context(), true); err != nil {
				t.Fatal(err)
			}
			source.podsFetchedAt = time.Now().Add(-2 * time.Hour)
			client.pods = []corev1.Pod{{Name: "new-pod"}}
			client.stats = &StatsSummary{Pods: []PodStats{{PodRef: PodReference{Name: "new-pod"}}}}
			if podOnlyRefresh {
				if _, err := source.GetSnapshot(t.Context(), false); err != nil {
					t.Fatal(err)
				}
				if client.statsCalls != 1 {
					t.Fatalf("pod-only refresh fetched stats: %d calls", client.statsCalls)
				}
			}
			snapshot, err := source.GetSnapshot(t.Context(), true)
			if err != nil {
				t.Fatal(err)
			}
			if client.podCalls != 2 || client.statsCalls != 2 {
				t.Fatalf("calls = pods:%d stats:%d, want 2 each", client.podCalls, client.statsCalls)
			}
			if snapshot.Pods[0].Name != "new-pod" || snapshot.Stats != client.stats {
				t.Fatalf("snapshot did not refresh pods and stats: %#v", snapshot)
			}
		})
	}
}
func TestCachedSnapshotSourceKeepsPodsWhenStatsFail(t *testing.T) {
	client := &fakeKubeletClient{
		pods:     []corev1.Pod{{Name: "pod-a"}},
		statsErr: errors.New("stats unavailable"),
	}
	snapshot, err := NewCachedSnapshotSource(client, 0).GetSnapshot(t.Context(), true)
	if err != nil {
		t.Fatalf("GetSnapshot() error: %v", err)
	}
	if len(snapshot.Pods) != 1 || snapshot.StatsError == nil {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestCachedSnapshotSourceSkipsStatsWhenContainersDisabled(t *testing.T) {
	client := &fakeKubeletClient{pods: []corev1.Pod{{}}}
	if _, err := NewCachedSnapshotSource(client, 0).GetSnapshot(t.Context(), false); err != nil {
		t.Fatalf("GetSnapshot() error: %v", err)
	}
	if client.statsCalls != 0 {
		t.Fatalf("stats calls = %d, want 0", client.statsCalls)
	}
}

func TestCachedSnapshotSourceCachesEmptyPodList(t *testing.T) {
	client := &fakeKubeletClient{}
	source := NewCachedSnapshotSource(client, time.Minute)
	if _, err := source.GetSnapshot(t.Context(), false); err != nil {
		t.Fatalf("first GetSnapshot() error: %v", err)
	}
	if _, err := source.GetSnapshot(t.Context(), true); err != nil {
		t.Fatalf("second GetSnapshot() error: %v", err)
	}
	if client.podCalls != 1 {
		t.Fatalf("pod calls = %d, want 1", client.podCalls)
	}
}
