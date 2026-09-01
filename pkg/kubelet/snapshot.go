// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package kubelet

import (
	"context"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
)

// Snapshot is one coherent view of the local kubelet's pods and usage stats.
// StatsError is non-nil when pods were available but optional usage data was not.
type Snapshot struct {
	Pods       []corev1.Pod
	Stats      *StatsSummary
	StatsError error
}

// SnapshotSource supplies kubelet snapshots to resource handlers.
type SnapshotSource interface {
	GetSnapshot(ctx context.Context, includeStats bool) (*Snapshot, error)
}

// CachedSnapshotSource coalesces pod and container handler requests that fire
// at approximately the same time, avoiding duplicate kubelet API calls.
type CachedSnapshotSource struct {
	client Interface
	maxAge time.Duration

	mu             sync.Mutex
	pods           []corev1.Pod
	podsFetchedAt  time.Time
	stats          *StatsSummary
	statsError     error
	statsFetchedAt time.Time
}

// NewCachedSnapshotSource creates a shared kubelet snapshot source.
func NewCachedSnapshotSource(client Interface, maxAge time.Duration) *CachedSnapshotSource {
	if maxAge < 0 {
		maxAge = 0
	}
	return &CachedSnapshotSource{
		client: client,
		maxAge: maxAge,
	}
}

// GetSnapshot returns a recent snapshot or refreshes it from the kubelet.
func (s *CachedSnapshotSource) GetSnapshot(ctx context.Context, includeStats bool) (*Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.podsFetchedAt.IsZero() || time.Since(s.podsFetchedAt) > s.maxAge {
		pods, err := s.client.GetPods(ctx)
		if err != nil {
			return nil, err
		}
		s.pods = pods
		s.podsFetchedAt = time.Now()
	}

	snapshot := &Snapshot{Pods: s.pods}
	if includeStats {
		if s.statsFetchedAt.IsZero() || time.Since(s.statsFetchedAt) > s.maxAge {
			s.stats, s.statsError = s.client.GetStatsSummary(ctx)
			s.statsFetchedAt = time.Now()
		}
		snapshot.Stats = s.stats
		snapshot.StatsError = s.statsError
	}
	return snapshot, nil
}
