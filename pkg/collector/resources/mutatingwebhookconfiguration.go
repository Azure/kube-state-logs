// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// MutatingWebhookConfigurationHandler handles collection of mutatingwebhookconfiguration metrics
type MutatingWebhookConfigurationHandler struct {
	utils.BaseHandler
}

// NewMutatingWebhookConfigurationHandler creates a new MutatingWebhookConfigurationHandler
func NewMutatingWebhookConfigurationHandler(client kubernetes.Interface) *MutatingWebhookConfigurationHandler {
	return &MutatingWebhookConfigurationHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the mutatingwebhookconfiguration informer
func (h *MutatingWebhookConfigurationHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create mutatingwebhookconfiguration informer
	informer := factory.Admissionregistration().V1().MutatingWebhookConfigurations().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers mutatingwebhookconfiguration metrics from the cluster (uses cache)
func (h *MutatingWebhookConfigurationHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all mutatingwebhookconfigurations from the cache
	webhooks := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range webhooks {
		webhook, ok := obj.(*admissionregistrationv1.MutatingWebhookConfiguration)
		if !ok {
			continue
		}

		entry := h.createLogEntry(webhook)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a MutatingWebhookConfigurationData from a mutatingwebhookconfiguration
func (h *MutatingWebhookConfigurationHandler) createLogEntry(webhook *admissionregistrationv1.MutatingWebhookConfiguration) types.MutatingWebhookConfigurationData {
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
	data := types.MutatingWebhookConfigurationData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "mutatingwebhookconfiguration",
				Name:             utils.ExtractName(webhook),
				CreatedTimestamp: utils.ExtractCreationTimestamp(webhook),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(webhook),
				Annotations: utils.ExtractAnnotations(webhook),
			},
		},
		Webhooks: webhooks,
	}

	return data
}
