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

// ReplicaSetHandler handles collection of replicaset metrics
type ReplicaSetHandler struct {
	utils.BaseHandler
}

// NewReplicaSetHandler creates a new ReplicaSetHandler
func NewReplicaSetHandler(client kubernetes.Interface) *ReplicaSetHandler {
	return &ReplicaSetHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the replicaset informer
func (h *ReplicaSetHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create replicaset informer
	informer := factory.Apps().V1().ReplicaSets().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers replicaset metrics from the cluster (uses cache)
func (h *ReplicaSetHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all replicasets from the cache
	replicasets := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range replicasets {
		replicaset, ok := obj.(*appsv1.ReplicaSet)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, replicaset.Namespace) {
			continue
		}

		// Skip replicasets with 0 desired replicas to avoid showing historical revisions
		if replicaset.Spec.Replicas != nil {
			if *replicaset.Spec.Replicas <= 0 {
				continue
			}
		}

		entry := h.createLogEntry(replicaset)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a ReplicaSetData from a replicaset
func (h *ReplicaSetHandler) createLogEntry(rs *appsv1.ReplicaSet) types.ReplicaSetData {
	// Get desired replicas with nil check
	desiredReplicas := int32(1)
	if rs.Spec.Replicas != nil {
		desiredReplicas = *rs.Spec.Replicas
	}

	// Get status fields
	observedGeneration := rs.Status.ObservedGeneration

	// Check conditions in a single loop
	var conditionAvailable, conditionProgressing, conditionReplicaFailure *bool
	conditions := make(map[string]*bool)

	for _, condition := range rs.Status.Conditions {
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

	createdByKind, createdByName := utils.GetOwnerReferenceInfo(rs)

	return types.ReplicaSetData{
		ControllerCreatedResourceMetadata: types.ControllerCreatedResourceMetadata{
			NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
				NamespacedMetadata: types.NamespacedMetadata{
					BaseMetadata: types.BaseMetadata{
						Timestamp:        time.Now(),
						ResourceType:     "replicaset",
						Name:             utils.ExtractName(rs),
						CreatedTimestamp: utils.ExtractCreationTimestamp(rs),
					},
					Namespace: utils.ExtractNamespace(rs),
				},
				LabeledMetadata: types.LabeledMetadata{
					Labels:      utils.ExtractLabels(rs),
					Annotations: utils.ExtractAnnotations(rs),
				},
			},
			ControllerCreatedMetadata: types.ControllerCreatedMetadata{
				CreatedByKind: createdByKind,
				CreatedByName: createdByName,
			},
		},
		// Replica counts
		DesiredReplicas:      desiredReplicas,
		CurrentReplicas:      rs.Status.Replicas,
		ReadyReplicas:        rs.Status.ReadyReplicas,
		AvailableReplicas:    rs.Status.AvailableReplicas,
		FullyLabeledReplicas: rs.Status.FullyLabeledReplicas,

		// Replicaset status
		ObservedGeneration: observedGeneration,

		// Most common conditions (for easy access)
		ConditionAvailable:      conditionAvailable,
		ConditionProgressing:    conditionProgressing,
		ConditionReplicaFailure: conditionReplicaFailure,

		// All other conditions (excluding the top-level ones)
		Conditions: conditions,
	}
}
