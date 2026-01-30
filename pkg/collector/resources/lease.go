// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// LeaseHandler handles collection of lease metrics
type LeaseHandler struct {
	utils.BaseHandler
}

// NewLeaseHandler creates a new LeaseHandler
func NewLeaseHandler(client kubernetes.Interface) *LeaseHandler {
	return &LeaseHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the lease informer
func (h *LeaseHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create lease informer
	informer := factory.Coordination().V1().Leases().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers lease metrics from the cluster (uses cache)
func (h *LeaseHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all leases from the cache
	leases := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range leases {
		lease, ok := obj.(*coordinationv1.Lease)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, lease.Namespace) {
			continue
		}

		entry := h.createLogEntry(lease)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a LeaseData from a lease
func (h *LeaseHandler) createLogEntry(lease *coordinationv1.Lease) types.LeaseData {
	// Extract timestamps
	var renewTime *time.Time
	var acquireTime *time.Time

	if lease.Spec.RenewTime != nil {
		renewTime = &lease.Spec.RenewTime.Time
	}
	if lease.Spec.AcquireTime != nil {
		acquireTime = &lease.Spec.AcquireTime.Time
	}

	// Extract holder identity
	holderIdentity := ""
	if lease.Spec.HolderIdentity != nil {
		holderIdentity = *lease.Spec.HolderIdentity
	}

	// Extract lease duration seconds with nil check
	leaseDurationSeconds := int32(0)
	if lease.Spec.LeaseDurationSeconds != nil {
		leaseDurationSeconds = *lease.Spec.LeaseDurationSeconds
	}

	// Extract lease transitions with nil check
	leaseTransitions := int32(0)
	if lease.Spec.LeaseTransitions != nil {
		leaseTransitions = *lease.Spec.LeaseTransitions
	}

	data := types.LeaseData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "lease",
					Name:             utils.ExtractName(lease),
					CreatedTimestamp: utils.ExtractCreationTimestamp(lease),
				},
				Namespace: utils.ExtractNamespace(lease),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(lease),
				Annotations: utils.ExtractAnnotations(lease),
			},
		},
		HolderIdentity:       holderIdentity,
		LeaseDurationSeconds: leaseDurationSeconds,
		RenewTime:            renewTime,
		AcquireTime:          acquireTime,
		LeaseTransitions:     leaseTransitions,
	}

	return data
}
