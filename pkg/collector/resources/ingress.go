// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"strconv"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// IngressHandler handles collection of ingress metrics
type IngressHandler struct {
	utils.BaseHandler
}

// NewIngressHandler creates a new IngressHandler
func NewIngressHandler(client kubernetes.Interface) *IngressHandler {
	return &IngressHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the ingress informer
func (h *IngressHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create ingress informer
	informer := factory.Networking().V1().Ingresses().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers ingress metrics from the cluster (uses cache)
func (h *IngressHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all ingresses from the cache
	ingresses := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range ingresses {
		ingress, ok := obj.(*networkingv1.Ingress)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, ingress.Namespace) {
			continue
		}

		entry := h.createLogEntry(ingress)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates an IngressData from an ingress
func (h *IngressHandler) createLogEntry(ingress *networkingv1.Ingress) types.IngressData {
	// Extract ingress class name
	var ingressClassName *string
	if ingress.Spec.IngressClassName != nil {
		ingressClassName = ingress.Spec.IngressClassName
	}

	// Extract load balancer ingress
	var loadBalancerIngress []types.LoadBalancerIngressData
	for _, lb := range ingress.Status.LoadBalancer.Ingress {
		loadBalancerIngress = append(loadBalancerIngress, types.LoadBalancerIngressData{
			IP:       lb.IP,
			Hostname: lb.Hostname,
		})
	}

	// Extract rules
	var rules []types.IngressRuleData
	for _, rule := range ingress.Spec.Rules {
		var paths []types.IngressPathData
		if rule.HTTP != nil {
			for _, path := range rule.HTTP.Paths {
				pathType := ""
				if path.PathType != nil {
					pathType = string(*path.PathType)
				}
				// Default path type is "ImplementationSpecific" when not specified
				// See: https://kubernetes.io/docs/concepts/services-networking/ingress/#path-types

				port := ""
				if path.Backend.Service != nil {
					port = strconv.FormatInt(int64(path.Backend.Service.Port.Number), 10)
				}

				paths = append(paths, types.IngressPathData{
					Path:     path.Path,
					PathType: pathType,
					Service:  path.Backend.Service.Name,
					Port:     port,
				})
			}
		}

		rules = append(rules, types.IngressRuleData{
			Host:  rule.Host,
			Paths: paths,
		})
	}

	// Extract TLS configuration
	var tls []types.IngressTLSData
	for _, tlsConfig := range ingress.Spec.TLS {
		tls = append(tls, types.IngressTLSData{
			Hosts:      tlsConfig.Hosts,
			SecretName: tlsConfig.SecretName,
		})
	}

	// Check if load balancer is ready
	var conditionLoadBalancerReady *bool
	if len(ingress.Status.LoadBalancer.Ingress) > 0 {
		val := true
		conditionLoadBalancerReady = &val
	}

	// Create data structure
	data := types.IngressData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "ingress",
					Name:             utils.ExtractName(ingress),
					CreatedTimestamp: utils.ExtractCreationTimestamp(ingress),
				},
				Namespace: utils.ExtractNamespace(ingress),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(ingress),
				Annotations: utils.ExtractAnnotations(ingress),
			},
		},
		IngressClassName: ingressClassName,
		LoadBalancerIP:   "",
		LoadBalancerIngress: func() []types.LoadBalancerIngressData {
			if loadBalancerIngress == nil {
				return []types.LoadBalancerIngressData{}
			}
			return loadBalancerIngress
		}(),
		Rules: rules,
		TLS: func() []types.IngressTLSData {
			if tls == nil {
				return []types.IngressTLSData{}
			}
			return tls
		}(),
		ConditionLoadBalancerReady: conditionLoadBalancerReady,
		Conditions:                 make(map[string]*bool), // Ingress doesn't have conditions
	}

	return data
}
