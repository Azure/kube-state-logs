// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"sync/atomic"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// ValidatingWebhookConfigurationHandler handles collection of validatingwebhookconfiguration metrics
type ValidatingWebhookConfigurationHandler struct {
	utils.BaseHandler
}

// NewValidatingWebhookConfigurationHandler creates a new ValidatingWebhookConfigurationHandler
func NewValidatingWebhookConfigurationHandler(client kubernetes.Interface) *ValidatingWebhookConfigurationHandler {
	return &ValidatingWebhookConfigurationHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the validatingwebhookconfiguration informer
func (h *ValidatingWebhookConfigurationHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create validatingwebhookconfiguration informer
	informer := factory.Admissionregistration().V1().ValidatingWebhookConfigurations().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers validatingwebhookconfiguration metrics from the cluster (uses cache)
func (h *ValidatingWebhookConfigurationHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all validatingwebhookconfigurations from the cache
	webhooks := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range webhooks {
		webhook, ok := obj.(*admissionregistrationv1.ValidatingWebhookConfiguration)
		if !ok {
			continue
		}

		entry := h.createLogEntry(webhook)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a ValidatingWebhookConfigurationData from a validatingwebhookconfiguration
func (h *ValidatingWebhookConfigurationHandler) createLogEntry(webhook *admissionregistrationv1.ValidatingWebhookConfiguration) types.ValidatingWebhookConfigurationData {
	// Extract webhooks
	var webhooks []types.WebhookData
	for _, webhook := range webhook.Webhooks {
		// Extract client config
		var clientConfig types.WebhookClientConfigData
		if webhook.ClientConfig.URL != nil {
			clientConfig.URL = *webhook.ClientConfig.URL
		}
		if webhook.ClientConfig.Service != nil {
			path := ""
			if webhook.ClientConfig.Service.Path != nil {
				path = *webhook.ClientConfig.Service.Path
			}
			port := int32(0)
			if webhook.ClientConfig.Service.Port != nil {
				port = *webhook.ClientConfig.Service.Port
			}
			clientConfig.Service = &types.WebhookServiceData{
				Namespace: webhook.ClientConfig.Service.Namespace,
				Name:      webhook.ClientConfig.Service.Name,
				Path:      path,
				Port:      port,
			}
		}
		clientConfig.CABundle = webhook.ClientConfig.CABundle

		// Extract rules
		var rules []types.WebhookRuleData
		for _, rule := range webhook.Rules {
			rules = append(rules, types.WebhookRuleData{
				APIGroups:   rule.APIGroups,
				APIVersions: rule.APIVersions,
				Resources:   rule.Resources,
				Scope:       string(*rule.Scope),
			})
		}

		// Extract selectors
		var namespaceSelector map[string]string
		if webhook.NamespaceSelector != nil {
			namespaceSelector = webhook.NamespaceSelector.MatchLabels
		}

		var objectSelector map[string]string
		if webhook.ObjectSelector != nil {
			objectSelector = webhook.ObjectSelector.MatchLabels
		}

		webhooks = append(webhooks, types.WebhookData{
			Name:                    webhook.Name,
			ClientConfig:            clientConfig,
			Rules:                   rules,
			FailurePolicy:           string(*webhook.FailurePolicy),
			MatchPolicy:             string(*webhook.MatchPolicy),
			NamespaceSelector:       namespaceSelector,
			ObjectSelector:          objectSelector,
			SideEffects:             string(*webhook.SideEffects),
			TimeoutSeconds:          webhook.TimeoutSeconds,
			AdmissionReviewVersions: webhook.AdmissionReviewVersions,
		})
	}

	// Create data structure
	data := types.ValidatingWebhookConfigurationData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:         time.Now(),
				ResourceType:      "validatingwebhookconfiguration",
				Name:              utils.ExtractName(webhook),
				CreatedTimestamp:  utils.ExtractCreationTimestamp(webhook),
				EventType:         "snapshot",
				DeletionTimestamp: utils.ExtractDeletionTimestamp(webhook),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(webhook),
				Annotations: utils.ExtractAnnotations(webhook),
			},
		},
		Webhooks: func() []types.WebhookData {
			if webhooks == nil {
				return []types.WebhookData{}
			}
			return webhooks
		}(),
	}

	return data
}

// SetupEventHandlers registers informer event handlers for immediate
// logging on resource creation and deletion.
func (h *ValidatingWebhookConfigurationHandler) SetupEventHandlers(logger interfaces.Logger, namespaces []string, hasSynced *atomic.Bool) {
	utils.SetupEventHandlers(h.GetInformer(), func(obj *admissionregistrationv1.ValidatingWebhookConfiguration, eventType string) any {
		entry := h.createLogEntry(obj)
		entry.EventType = eventType
		return entry
	}, func(obj *admissionregistrationv1.ValidatingWebhookConfiguration) bool {
		return true
	}, logger, hasSynced)
}
