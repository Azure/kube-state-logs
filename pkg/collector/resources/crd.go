// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// CRDHandler handles collection of generic CRD metrics
type CRDHandler struct {
	client       dynamic.Interface
	informer     cache.SharedIndexInformer
	logger       interfaces.Logger
	gvr          schema.GroupVersionResource
	resourceName string
	customFields []string // JSONPath-like field paths to extract
}

// NewCRDHandler creates a new CRDHandler for a specific CRD
func NewCRDHandler(client dynamic.Interface, gvr schema.GroupVersionResource, resourceName string, customFields []string) *CRDHandler {
	return &CRDHandler{
		client:       client,
		gvr:          gvr,
		resourceName: resourceName,
		customFields: customFields,
	}
}

// SetupInformer sets up the CRD informer
func (h *CRDHandler) SetupInformer(factory dynamicinformer.DynamicSharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	h.logger = logger

	// Create dynamic informer for the CRD
	h.informer = factory.ForResource(h.gvr).Informer()

	return nil
}

// Collect gathers CRD metrics from the cluster (uses cache)
func (h *CRDHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all CRDs from the cache
	crds := utils.SafeGetStoreList(h.informer)
	listTime := time.Now()

	for _, obj := range crds {
		unstructuredObj, ok := obj.(*unstructured.Unstructured)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, unstructuredObj.GetNamespace()) {
			continue
		}

		entry := h.createLogEntry(unstructuredObj)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a CRDData from a CRD resource
func (h *CRDHandler) createLogEntry(obj *unstructured.Unstructured) types.CRDData {
	// Extract custom fields based on JSONPath-like paths
	customFields := make(map[string]any)
	for _, fieldPath := range h.customFields {
		if value := h.extractField(obj.Object, fieldPath); value != nil {
			customFields[fieldPath] = value
		}
	}

	// Create data structure - only include custom fields
	data := types.CRDData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     strings.ToLower(obj.GetKind()), // Use lowercase Kind as ResourceType
					Name:             utils.ExtractName(obj),
					CreatedTimestamp: utils.ExtractCreationTimestamp(obj),
				},
				Namespace: utils.ExtractNamespace(obj),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(obj),
				Annotations: utils.ExtractAnnotations(obj),
			},
		},
		APIVersion:   obj.GetAPIVersion(),
		CustomFields: customFields,
	}

	return data
}

// extractField extracts a field from an object using a dot-separated path
func (h *CRDHandler) extractField(obj map[string]any, path string) any {
	return utils.ExtractField(obj, path)
}
