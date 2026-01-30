// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/interfaces"
)

// BaseHandler provides common fields and methods for resource handlers
type BaseHandler struct {
	client   kubernetes.Interface
	informer cache.SharedIndexInformer
	logger   interfaces.Logger
}

// NewBaseHandler creates a new BaseHandler
func NewBaseHandler(client kubernetes.Interface) BaseHandler {
	return BaseHandler{
		client: client,
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
