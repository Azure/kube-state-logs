// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// IngressClassHandler handles collection of ingressclass metrics
type IngressClassHandler struct {
	utils.BaseHandler
}

// NewIngressClassHandler creates a new IngressClassHandler
func NewIngressClassHandler(client kubernetes.Interface) *IngressClassHandler {
	return &IngressClassHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the ingressclass informer
func (h *IngressClassHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create ingressclass informer
	informer := factory.Networking().V1().IngressClasses().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers ingressclass metrics from the cluster (uses cache)
func (h *IngressClassHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all ingressclasses from the cache
	ingressclasses := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range ingressclasses {
		ingressclass, ok := obj.(*networkingv1.IngressClass)
		if !ok {
			continue
		}

		entry := h.createLogEntry(ingressclass)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a IngressClassData from an ingressclass
func (h *IngressClassHandler) createLogEntry(ic *networkingv1.IngressClass) types.IngressClassData {
	isDefault := false
	annotations := utils.ExtractAnnotations(ic)
	if annotations != nil {
		if _, exists := annotations["ingressclass.kubernetes.io/is-default-class"]; exists {
			isDefault = true
		}
	}

	data := types.IngressClassData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "ingressclass",
				Name:             utils.ExtractName(ic),
				CreatedTimestamp: utils.ExtractCreationTimestamp(ic),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(ic),
				Annotations: annotations,
			},
		},
		Controller: ic.Spec.Controller,
		IsDefault:  isDefault,
	}

	return data
}
