// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// DaemonSetHandler handles collection of daemonset metrics
type DaemonSetHandler struct {
	utils.BaseHandler
}

// NewDaemonSetHandler creates a new DaemonSetHandler
func NewDaemonSetHandler(client kubernetes.Interface) *DaemonSetHandler {
	return &DaemonSetHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the daemonset informer
func (h *DaemonSetHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create daemonset informer
	informer := factory.Apps().V1().DaemonSets().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers daemonset metrics from the cluster (uses cache)
func (h *DaemonSetHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all daemonsets from the cache
	daemonsets := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range daemonsets {
		daemonset, ok := obj.(*appsv1.DaemonSet)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, daemonset.Namespace) {
			continue
		}

		entry := h.createLogEntry(daemonset)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a DaemonSetData from a daemonset
func (h *DaemonSetHandler) createLogEntry(ds *appsv1.DaemonSet) types.DaemonSetData {
	// Get status fields
	observedGeneration := ds.Status.ObservedGeneration

	// Check conditions in a single loop
	var conditionAvailable, conditionProgressing, conditionReplicaFailure *bool
	conditions := make(map[string]*bool)

	for _, condition := range ds.Status.Conditions {
		val := utils.ConvertCoreConditionStatus(condition.Status)

		switch condition.Type {
		case "Available":
			conditionAvailable = val
		case "Progressing":
			conditionProgressing = val
		case "ReplicaFailure":
			conditionReplicaFailure = val
		default:
			// Add unknown conditions to the map
			conditions[string(condition.Type)] = val
		}
	}

	return types.DaemonSetData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "daemonset",
					Name:             utils.ExtractName(ds),
					CreatedTimestamp: utils.ExtractCreationTimestamp(ds),
				},
				Namespace: utils.ExtractNamespace(ds),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(ds),
				Annotations: utils.ExtractAnnotations(ds),
			},
		},
		// Replica counts
		DesiredNumberScheduled: ds.Status.DesiredNumberScheduled,
		CurrentNumberScheduled: ds.Status.CurrentNumberScheduled,
		NumberReady:            ds.Status.NumberReady,
		NumberAvailable:        ds.Status.NumberAvailable,
		NumberUnavailable:      ds.Status.NumberUnavailable,
		NumberMisscheduled:     ds.Status.NumberMisscheduled,
		UpdatedNumberScheduled: ds.Status.UpdatedNumberScheduled,

		// Daemonset status
		ObservedGeneration: observedGeneration,

		// Most common conditions (for easy access)
		ConditionAvailable:      conditionAvailable,
		ConditionProgressing:    conditionProgressing,
		ConditionReplicaFailure: conditionReplicaFailure,

		// All other conditions (excluding the top-level ones)
		Conditions: conditions,

		// Daemonset specific
		UpdateStrategy:     string(ds.Spec.UpdateStrategy.Type),
		MetadataGeneration: ds.ObjectMeta.Generation,
		CollisionCount:     ds.Status.CollisionCount,
	}
}
