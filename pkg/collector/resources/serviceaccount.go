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

// ServiceAccountHandler handles collection of serviceaccount metrics
type ServiceAccountHandler struct {
	utils.BaseHandler
}

// NewServiceAccountHandler creates a new ServiceAccountHandler
func NewServiceAccountHandler(client kubernetes.Interface) *ServiceAccountHandler {
	return &ServiceAccountHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the serviceaccount informer
func (h *ServiceAccountHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create serviceaccount informer
	informer := factory.Core().V1().ServiceAccounts().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers serviceaccount metrics from the cluster (uses cache)
func (h *ServiceAccountHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all serviceaccounts from the cache
	serviceaccounts := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range serviceaccounts {
		serviceaccount, ok := obj.(*corev1.ServiceAccount)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, serviceaccount.Namespace) {
			continue
		}

		entry := h.createLogEntry(serviceaccount)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a ServiceAccountData from a serviceaccount
func (h *ServiceAccountHandler) createLogEntry(sa *corev1.ServiceAccount) types.ServiceAccountData {
	// Extract secrets
	var secrets []string
	for _, secret := range sa.Secrets {
		secrets = append(secrets, secret.Name)
	}

	// Extract image pull secrets
	var imagePullSecrets []string
	for _, secret := range sa.ImagePullSecrets {
		imagePullSecrets = append(imagePullSecrets, secret.Name)
	}

	// Get automount service account token setting
	// Default is true when automountServiceAccountToken is nil
	// See: https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#use-the-default-service-account-to-access-the-api-server
	automountToken := true
	if sa.AutomountServiceAccountToken != nil {
		automountToken = *sa.AutomountServiceAccountToken
	}

	// Create data structure
	data := types.ServiceAccountData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:         time.Now(),
					ResourceType:      "serviceaccount",
					Name:              utils.ExtractName(sa),
					CreatedTimestamp:  utils.ExtractCreationTimestamp(sa),
					EventType:         "snapshot",
					DeletionTimestamp: utils.ExtractDeletionTimestamp(sa),
				},
				Namespace: utils.ExtractNamespace(sa),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(sa),
				Annotations: utils.ExtractAnnotations(sa),
			},
		},
		Secrets: func() []string {
			if secrets == nil {
				return []string{}
			}
			return secrets
		}(),
		ImagePullSecrets: func() []string {
			if imagePullSecrets == nil {
				return []string{}
			}
			return imagePullSecrets
		}(),
		AutomountServiceAccountToken: &automountToken,
	}

	return data
}

// SetupEventHandlers registers informer event handlers for immediate
// logging on resource creation and deletion.
func (h *ServiceAccountHandler) SetupEventHandlers(logger interfaces.Logger, namespaces []string, hasSynced *atomic.Bool) {
	utils.SetupEventHandlers(h.GetInformer(), func(obj *corev1.ServiceAccount, eventType string) any {
		entry := h.createLogEntry(obj)
		entry.EventType = eventType
		return entry
	}, func(obj *corev1.ServiceAccount) bool {
		return utils.ShouldIncludeNamespace(namespaces, obj.Namespace)
	}, logger, hasSynced)
}
