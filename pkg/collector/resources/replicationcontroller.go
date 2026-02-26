// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// ReplicationControllerHandler handles collection of replicationcontroller metrics
type ReplicationControllerHandler struct {
	utils.BaseHandler
}

// NewReplicationControllerHandler creates a new ReplicationControllerHandler
func NewReplicationControllerHandler(client kubernetes.Interface) *ReplicationControllerHandler {
	return &ReplicationControllerHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the replicationcontroller informer
func (h *ReplicationControllerHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create replicationcontroller informer
	informer := factory.Core().V1().ReplicationControllers().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers replicationcontroller metrics from the cluster (uses cache)
func (h *ReplicationControllerHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all replicationcontrollers from the cache
	replicationcontrollers := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range replicationcontrollers {
		replicationcontroller, ok := obj.(*corev1.ReplicationController)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, replicationcontroller.Namespace) {
			continue
		}

		entry := h.createLogEntry(replicationcontroller)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a ReplicationControllerData from a replicationcontroller
func (h *ReplicationControllerHandler) createLogEntry(rc *corev1.ReplicationController) types.ReplicationControllerData {
	// Get desired replicas
	// Default to 1 when spec.replicas is nil (Kubernetes API default)
	// See: https://kubernetes.io/docs/concepts/workloads/controllers/replicationcontroller/#replicationcontroller
	desiredReplicas := int32(1) // Default value
	if rc.Spec.Replicas != nil {
		desiredReplicas = *rc.Spec.Replicas
	}

	// Create data structure
	data := types.ReplicationControllerData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:         time.Now(),
					ResourceType:      "replicationcontroller",
					Name:              utils.ExtractName(rc),
					CreatedTimestamp:  utils.ExtractCreationTimestamp(rc),
					EventType:         "snapshot",
					DeletionTimestamp: utils.ExtractDeletionTimestamp(rc),
				},
				Namespace: utils.ExtractNamespace(rc),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(rc),
				Annotations: utils.ExtractAnnotations(rc),
			},
		},
		DesiredReplicas:      desiredReplicas,
		CurrentReplicas:      rc.Status.Replicas,
		ReadyReplicas:        rc.Status.ReadyReplicas,
		AvailableReplicas:    rc.Status.AvailableReplicas,
		FullyLabeledReplicas: rc.Status.FullyLabeledReplicas,
		ObservedGeneration:   rc.Status.ObservedGeneration,
	}

	return data
}

// SetupEventHandlers registers informer event handlers for immediate
// logging on resource creation and deletion.
func (h *ReplicationControllerHandler) SetupEventHandlers(logger interfaces.Logger, namespaces []string, hasSynced *atomic.Bool) {
	utils.SetupEventHandlers(h.GetInformer(), func(obj *corev1.ReplicationController, eventType string) any {
		entry := h.createLogEntry(obj)
		entry.EventType = eventType
		return entry
	}, func(obj *corev1.ReplicationController) bool {
		return utils.ShouldIncludeNamespace(namespaces, obj.Namespace)
	}, logger, hasSynced)
}
