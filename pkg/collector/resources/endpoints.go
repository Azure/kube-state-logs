// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// EndpointsHandler handles collection of endpoints metrics
type EndpointsHandler struct {
	utils.BaseHandler
}

// NewEndpointsHandler creates a new EndpointsHandler
func NewEndpointsHandler(client kubernetes.Interface) *EndpointsHandler {
	return &EndpointsHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the endpoints informer
func (h *EndpointsHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create endpoints informer
	informer := factory.Core().V1().Endpoints().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers endpoints metrics from the cluster (uses cache)
func (h *EndpointsHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all endpoints from the cache
	endpoints := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range endpoints {
		endpoint, ok := obj.(*corev1.Endpoints)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, endpoint.Namespace) {
			continue
		}

		if !h.MatchesSelectors(endpoint) {
			continue
		}

		entry := h.createLogEntry(endpoint)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates an EndpointsData from endpoints
func (h *EndpointsHandler) createLogEntry(endpoints *corev1.Endpoints) types.EndpointsData {
	// Extract addresses and ports from all subsets
	var addresses []types.EndpointAddressData
	var ports []types.EndpointPortData

	for _, subset := range endpoints.Subsets {
		// Extract addresses
		for _, address := range subset.Addresses {
			targetRef := ""
			if address.TargetRef != nil {
				targetRef = address.TargetRef.Name
			}

			nodeName := ""
			if address.NodeName != nil {
				nodeName = *address.NodeName
			}

			addresses = append(addresses, types.EndpointAddressData{
				IP:        address.IP,
				Hostname:  address.Hostname,
				NodeName:  nodeName,
				TargetRef: targetRef,
			})
		}

		// Extract ports
		for _, port := range subset.Ports {
			ports = append(ports, types.EndpointPortData{
				Name:     port.Name,
				Protocol: string(port.Protocol),
				Port:     port.Port,
			})
		}
	}

	// Determine if endpoints are ready (have addresses)
	ready := len(addresses) > 0

	// Create data structure
	data := types.EndpointsData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "endpoints",
					Name:             utils.ExtractName(endpoints),
					CreatedTimestamp: utils.ExtractCreationTimestamp(endpoints),
				},
				Namespace: utils.ExtractNamespace(endpoints),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(endpoints),
				Annotations: utils.ExtractAnnotations(endpoints),
			},
		},
		Addresses: func() []types.EndpointAddressData {
			if addresses == nil {
				return []types.EndpointAddressData{}
			}
			return addresses
		}(),
		Ports: func() []types.EndpointPortData {
			if ports == nil {
				return []types.EndpointPortData{}
			}
			return ports
		}(),
		Ready: &ready,
	}

	return data
}
