// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/interfaces"
)

// BaseHandler provides common fields and methods for resource handlers
type BaseHandler struct {
	client        kubernetes.Interface
	informer      cache.SharedIndexInformer
	logger        interfaces.Logger
	labelSelector labels.Selector
	fieldSelector fields.Selector
}

// NewBaseHandler creates a new BaseHandler
func NewBaseHandler(client kubernetes.Interface) BaseHandler {
	return BaseHandler{
		client:        client,
		labelSelector: labels.Everything(),
		fieldSelector: fields.Everything(),
	}
}

// SetupBaseInformer sets up the base informer with common configuration
func (h *BaseHandler) SetupBaseInformer(informer cache.SharedIndexInformer, logger interfaces.Logger) {
	h.informer = informer
	h.logger = logger
}

// GetClient returns the Kubernetes client
func (h *BaseHandler) GetClient() kubernetes.Interface {
	return h.client
}

// GetInformer returns the informer
func (h *BaseHandler) GetInformer() cache.SharedIndexInformer {
	return h.informer
}

// GetLogger returns the logger
func (h *BaseHandler) GetLogger() interfaces.Logger {
	return h.logger
}

// SetSelectors configures label and field selectors for filtering.
func (h *BaseHandler) SetSelectors(labelSelector labels.Selector, fieldSelector fields.Selector) {
	if labelSelector == nil {
		labelSelector = labels.Everything()
	}
	if fieldSelector == nil {
		fieldSelector = fields.Everything()
	}

	h.labelSelector = labelSelector
	h.fieldSelector = fieldSelector
}

// MatchesSelectors checks whether the object matches the configured selectors.
func (h *BaseHandler) MatchesSelectors(obj metav1.Object) bool {
	if obj == nil {
		return false
	}

	if h.labelSelector != nil && !h.labelSelector.Matches(labels.Set(obj.GetLabels())) {
		return false
	}

	if h.fieldSelector != nil {
		fieldSet := fields.Set{
			"metadata.name":      obj.GetName(),
			"metadata.namespace": obj.GetNamespace(),
		}
		if !h.fieldSelector.Matches(fieldSet) {
			return false
		}
	}

	return true
}
