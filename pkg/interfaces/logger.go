package interfaces

import (
	"context"
	"time"

	"k8s.io/client-go/informers"
)

// Logger interface defines the logging contract
type Logger interface {
	Log(entry any) error
}

// ResourceHandler defines the interface for resource-specific collectors
type ResourceHandler interface {
	SetupInformer(factory informers.SharedInformerFactory, logger Logger, resyncPeriod time.Duration) error
	Collect(ctx context.Context, namespaces []string) ([]any, error)
}
