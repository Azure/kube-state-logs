// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"sync/atomic"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// NetworkPolicyHandler handles collection of networkpolicy metrics
type NetworkPolicyHandler struct {
	utils.BaseHandler
}

// NewNetworkPolicyHandler creates a new NetworkPolicyHandler
func NewNetworkPolicyHandler(client kubernetes.Interface) *NetworkPolicyHandler {
	return &NetworkPolicyHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the networkpolicy informer
func (h *NetworkPolicyHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create networkpolicy informer
	informer := factory.Networking().V1().NetworkPolicies().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers networkpolicy metrics from the cluster (uses cache)
func (h *NetworkPolicyHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all networkpolicies from the cache
	networkpolicies := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range networkpolicies {
		networkpolicy, ok := obj.(*networkingv1.NetworkPolicy)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, networkpolicy.Namespace) {
			continue
		}

		entry := h.createLogEntry(networkpolicy)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a NetworkPolicyData from a networkpolicy
func (h *NetworkPolicyHandler) createLogEntry(np *networkingv1.NetworkPolicy) types.NetworkPolicyData {
	// Get policy types
	// Default includes "Ingress" when policyTypes is not specified
	// See: https://kubernetes.io/docs/concepts/services-networking/network-policies/#default-policies
	policyTypes := make([]string, len(np.Spec.PolicyTypes))
	for i, policyType := range np.Spec.PolicyTypes {
		policyTypes[i] = string(policyType)
	}

	// Convert ingress rules
	var ingressRules []types.NetworkPolicyIngressRule
	if np.Spec.Ingress != nil && len(np.Spec.Ingress) > 0 {
		for _, rule := range np.Spec.Ingress {
			ingressRule := types.NetworkPolicyIngressRule{
				Ports: h.convertPorts(rule.Ports),
				From:  h.convertPeers(rule.From),
			}
			ingressRules = append(ingressRules, ingressRule)
		}
	} else {
		ingressRules = make([]types.NetworkPolicyIngressRule, 0)
	}

	// Convert egress rules
	var egressRules []types.NetworkPolicyEgressRule
	if np.Spec.Egress != nil && len(np.Spec.Egress) > 0 {
		for _, rule := range np.Spec.Egress {
			egressRule := types.NetworkPolicyEgressRule{
				Ports: h.convertPorts(rule.Ports),
				To:    h.convertPeers(rule.To),
			}
			egressRules = append(egressRules, egressRule)
		}
	} else {
		egressRules = make([]types.NetworkPolicyEgressRule, 0)
	}

	// Create data structure
	data := types.NetworkPolicyData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:         time.Now(),
					ResourceType:      "networkpolicy",
					Name:              utils.ExtractName(np),
					CreatedTimestamp:  utils.ExtractCreationTimestamp(np),
					EventType:         "snapshot",
					DeletionTimestamp: utils.ExtractDeletionTimestamp(np),
				},
				Namespace: utils.ExtractNamespace(np),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(np),
				Annotations: utils.ExtractAnnotations(np),
			},
		},
		PolicyTypes:  policyTypes,
		IngressRules: ingressRules,
		EgressRules:  egressRules,
	}

	return data
}

// convertPorts converts networkingv1.NetworkPolicyPort to types.NetworkPolicyPort
func (h *NetworkPolicyHandler) convertPorts(ports []networkingv1.NetworkPolicyPort) []types.NetworkPolicyPort {
	var result []types.NetworkPolicyPort
	for _, port := range ports {
		npPort := types.NetworkPolicyPort{}
		if port.Protocol != nil {
			npPort.Protocol = string(*port.Protocol)
		}
		if port.Port != nil {
			npPort.Port = port.Port.IntVal
		}
		if port.EndPort != nil {
			npPort.EndPort = *port.EndPort
		}
		result = append(result, npPort)
	}
	return result
}

// convertPeers converts networkingv1.NetworkPolicyPeer to types.NetworkPolicyPeer
func (h *NetworkPolicyHandler) convertPeers(peers []networkingv1.NetworkPolicyPeer) []types.NetworkPolicyPeer {
	var result []types.NetworkPolicyPeer
	for _, peer := range peers {
		npPeer := types.NetworkPolicyPeer{}

		if peer.PodSelector != nil {
			npPeer.PodSelector = peer.PodSelector.MatchLabels
		}
		if peer.NamespaceSelector != nil {
			npPeer.NamespaceSelector = peer.NamespaceSelector.MatchLabels
		}
		if peer.IPBlock != nil {
			npPeer.IPBlock = map[string]any{
				"cidr":   peer.IPBlock.CIDR,
				"except": peer.IPBlock.Except,
			}
		}
		result = append(result, npPeer)
	}
	return result
}

// SetupEventHandlers registers informer event handlers for immediate
// logging on resource creation and deletion.
func (h *NetworkPolicyHandler) SetupEventHandlers(logger interfaces.Logger, namespaces []string, hasSynced *atomic.Bool) {
	utils.SetupEventHandlers(h.GetInformer(), func(obj *networkingv1.NetworkPolicy, eventType string) any {
		entry := h.createLogEntry(obj)
		entry.EventType = eventType
		return entry
	}, func(obj *networkingv1.NetworkPolicy) bool {
		return utils.ShouldIncludeNamespace(namespaces, obj.Namespace)
	}, logger, hasSynced)
}
