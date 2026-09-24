// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package interfaces

import (
	"context"
	"time"

	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

// Logger interface defines the logging contract
type Logger interface {
	Log(entry any) error
}

// ResourceHandler defines the interface for resource-specific collectors
type ResourceHandler interface {
	SetupInformer(factory informers.SharedInformerFactory, logger Logger, resyncPeriod time.Duration) error
	GetInformers() []cache.SharedIndexInformer
	Collect(ctx context.Context, namespaces []string) ([]any, error)
}

// KubeletHandler defines the interface for kubelet-based resource collectors.
// Unlike ResourceHandler, this doesn't use informers since the kubelet API
// doesn't support watches - it polls the kubelet API directly.
type KubeletHandler interface {
	// Collect gathers resource data by polling the kubelet API.
	// The namespaces parameter filters results to specific namespaces (empty = all).
	Collect(ctx context.Context, namespaces []string) ([]any, error)
}
