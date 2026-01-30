// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	nodev1 "k8s.io/api/node/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// RuntimeClassHandler handles collection of runtimeclass metrics
type RuntimeClassHandler struct {
	utils.BaseHandler
}

// NewRuntimeClassHandler creates a new RuntimeClassHandler
func NewRuntimeClassHandler(client kubernetes.Interface) *RuntimeClassHandler {
	return &RuntimeClassHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the runtimeclass informer
func (h *RuntimeClassHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create runtimeclass informer
	informer := factory.Node().V1().RuntimeClasses().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers runtimeclass metrics from the cluster (uses cache)
func (h *RuntimeClassHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all runtimeclasses from the cache
	runtimeclasses := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range runtimeclasses {
		runtimeclass, ok := obj.(*nodev1.RuntimeClass)
		if !ok {
			continue
		}

		entry := h.createLogEntry(runtimeclass)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a RuntimeClassData from a runtimeclass
func (h *RuntimeClassHandler) createLogEntry(rc *nodev1.RuntimeClass) types.RuntimeClassData {
	// Create data structure
	// See: https://kubernetes.io/docs/concepts/containers/runtime-class/
	data := types.RuntimeClassData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "runtimeclass",
				Name:             utils.ExtractName(rc),
				CreatedTimestamp: utils.ExtractCreationTimestamp(rc),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(rc),
				Annotations: utils.ExtractAnnotations(rc),
			},
		},
		Handler: rc.Handler,
	}

	return data
}
