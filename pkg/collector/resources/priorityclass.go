// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// PriorityClassHandler handles collection of priorityclass metrics
type PriorityClassHandler struct {
	utils.BaseHandler
}

// NewPriorityClassHandler creates a new PriorityClassHandler
func NewPriorityClassHandler(client kubernetes.Interface) *PriorityClassHandler {
	return &PriorityClassHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the priorityclass informer
func (h *PriorityClassHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create priorityclass informer
	informer := factory.Scheduling().V1().PriorityClasses().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers priorityclass metrics from the cluster (uses cache)
func (h *PriorityClassHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all priorityclasses from the cache
	priorityclasses := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range priorityclasses {
		priorityclass, ok := obj.(*schedulingv1.PriorityClass)
		if !ok {
			continue
		}

		entry := h.createLogEntry(priorityclass)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a PriorityClassData from a priorityclass
func (h *PriorityClassHandler) createLogEntry(pc *schedulingv1.PriorityClass) types.PriorityClassData {
	createdTimestamp := utils.ExtractCreationTimestamp(pc)

	preemptionPolicy := ""
	if pc.PreemptionPolicy != nil {
		preemptionPolicy = string(*pc.PreemptionPolicy)
	}

	data := types.PriorityClassData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "priorityclass",
				Name:             utils.ExtractName(pc),
				CreatedTimestamp: createdTimestamp,
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(pc),
				Annotations: utils.ExtractAnnotations(pc),
			},
		},
		Value:            pc.Value,
		GlobalDefault:    pc.GlobalDefault,
		Description:      pc.Description,
		PreemptionPolicy: preemptionPolicy,
	}

	return data
}
