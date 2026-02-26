// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"sync/atomic"
	"time"

	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// PodDisruptionBudgetHandler handles collection of poddisruptionbudget metrics
type PodDisruptionBudgetHandler struct {
	utils.BaseHandler
}

// NewPodDisruptionBudgetHandler creates a new PodDisruptionBudgetHandler
func NewPodDisruptionBudgetHandler(client kubernetes.Interface) *PodDisruptionBudgetHandler {
	return &PodDisruptionBudgetHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the poddisruptionbudget informer
func (h *PodDisruptionBudgetHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create poddisruptionbudget informer
	informer := factory.Policy().V1().PodDisruptionBudgets().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers poddisruptionbudget metrics from the cluster (uses cache)
func (h *PodDisruptionBudgetHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all poddisruptionbudgets from the cache
	pdbs := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range pdbs {
		pdb, ok := obj.(*policyv1.PodDisruptionBudget)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, pdb.Namespace) {
			continue
		}

		entry := h.createLogEntry(pdb)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a PodDisruptionBudgetData from a PDB
func (h *PodDisruptionBudgetHandler) createLogEntry(pdb *policyv1.PodDisruptionBudget) types.PodDisruptionBudgetData {
	// Get min available and max unavailable
	// See: https://kubernetes.io/docs/concepts/workloads/pods/disruptions/#pod-disruption-budgets
	minAvailable := int32(0)
	maxUnavailable := int32(0)

	if pdb.Spec.MinAvailable != nil {
		minAvailable = pdb.Spec.MinAvailable.IntVal
	}
	if pdb.Spec.MaxUnavailable != nil {
		maxUnavailable = pdb.Spec.MaxUnavailable.IntVal
	}

	// Get status values
	currentHealthy := pdb.Status.CurrentHealthy
	desiredHealthy := pdb.Status.DesiredHealthy
	expectedPods := pdb.Status.ExpectedPods
	disruptionsAllowed := pdb.Status.DisruptionsAllowed
	disruptionAllowed := disruptionsAllowed > 0

	// Create data structure
	data := types.PodDisruptionBudgetData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:         time.Now(),
					ResourceType:      "poddisruptionbudget",
					Name:              utils.ExtractName(pdb),
					CreatedTimestamp:  utils.ExtractCreationTimestamp(pdb),
					EventType:         "snapshot",
					DeletionTimestamp: utils.ExtractDeletionTimestamp(pdb),
				},
				Namespace: utils.ExtractNamespace(pdb),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(pdb),
				Annotations: utils.ExtractAnnotations(pdb),
			},
		},
		MinAvailable:             minAvailable,
		MaxUnavailable:           maxUnavailable,
		CurrentHealthy:           currentHealthy,
		DesiredHealthy:           desiredHealthy,
		ExpectedPods:             expectedPods,
		DisruptionsAllowed:       disruptionsAllowed,
		TotalReplicas:            0,
		DisruptionAllowed:        disruptionAllowed,
		StatusCurrentHealthy:     currentHealthy,
		StatusDesiredHealthy:     desiredHealthy,
		StatusExpectedPods:       expectedPods,
		StatusDisruptionsAllowed: disruptionsAllowed,
		StatusTotalReplicas:      0,
		StatusDisruptionAllowed:  disruptionAllowed,
	}

	return data
}

// SetupEventHandlers registers informer event handlers for immediate
// logging on resource creation and deletion.
func (h *PodDisruptionBudgetHandler) SetupEventHandlers(logger interfaces.Logger, namespaces []string, hasSynced *atomic.Bool) {
	utils.SetupEventHandlers(h.GetInformer(), func(obj *policyv1.PodDisruptionBudget, eventType string) any {
		entry := h.createLogEntry(obj)
		entry.EventType = eventType
		return entry
	}, func(obj *policyv1.PodDisruptionBudget) bool {
		return utils.ShouldIncludeNamespace(namespaces, obj.Namespace)
	}, logger, hasSynced)
}
