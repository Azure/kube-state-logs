// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// ServiceHandler handles collection of service metrics
type ServiceHandler struct {
	utils.BaseHandler
	endpointsInformer cache.SharedIndexInformer
}

// NewServiceHandler creates a new ServiceHandler
func NewServiceHandler(client kubernetes.Interface) *ServiceHandler {
	return &ServiceHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the service informer
func (h *ServiceHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create service informer
	serviceInformer := factory.Core().V1().Services().Informer()
	h.SetupBaseInformer(serviceInformer, logger)

	// Create endpoints informer
	h.endpointsInformer = factory.Core().V1().Endpoints().Informer()

	return nil
}

// Collect gathers service metrics from the cluster (uses cache)
func (h *ServiceHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all services from the cache
	services := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range services {
		service, ok := obj.(*corev1.Service)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, service.Namespace) {
			continue
		}

		entry := h.createLogEntry(service)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a ServiceData from a service
func (h *ServiceHandler) createLogEntry(service *corev1.Service) types.ServiceData {
	// Convert ports
	var ports []types.ServicePortData
	for _, port := range service.Spec.Ports {
		targetPort := int32(0)
		if port.TargetPort.Type == intstr.Int {
			targetPort = port.TargetPort.IntVal
		} else if port.TargetPort.Type == intstr.String {
			// For string target ports, we'll use 0 as default
			// In a real implementation, you might want to resolve the port name
			// See: https://kubernetes.io/docs/concepts/services-networking/service/#defining-a-service
			targetPort = 0
		}

		ports = append(ports, types.ServicePortData{
			Name:       port.Name,
			Protocol:   string(port.Protocol),
			Port:       port.Port,
			TargetPort: targetPort,
			NodePort:   port.NodePort,
		})
	}

	// Get external IP
	var externalIP string
	if len(service.Spec.ExternalIPs) > 0 {
		externalIP = service.Spec.ExternalIPs[0]
	}

	// Get load balancer info
	var loadBalancerIngress []types.LoadBalancerIngressData
	if service.Spec.Type == corev1.ServiceTypeLoadBalancer && service.Status.LoadBalancer.Ingress != nil {
		for _, ingress := range service.Status.LoadBalancer.Ingress {
			loadBalancerIngress = append(loadBalancerIngress, types.LoadBalancerIngressData{
				IP:       ingress.IP,
				Hostname: ingress.Hostname,
			})
		}
	}

	// Count endpoints for this service
	endpointsCount := h.countEndpointsForService(service.Namespace, service.Name)

	// Get traffic policies
	externalTrafficPolicy := string(service.Spec.ExternalTrafficPolicy)

	// Get session affinity timeout
	sessionAffinityTimeout := int32(0)
	if service.Spec.SessionAffinityConfig != nil &&
		service.Spec.SessionAffinityConfig.ClientIP != nil &&
		service.Spec.SessionAffinityConfig.ClientIP.TimeoutSeconds != nil {
		sessionAffinityTimeout = *service.Spec.SessionAffinityConfig.ClientIP.TimeoutSeconds
	}

	// Get additional service spec fields
	var allocateLoadBalancerNodePorts *bool
	if service.Spec.AllocateLoadBalancerNodePorts != nil {
		allocateLoadBalancerNodePorts = service.Spec.AllocateLoadBalancerNodePorts
	}

	var loadBalancerClass *string
	if service.Spec.LoadBalancerClass != nil {
		loadBalancerClass = service.Spec.LoadBalancerClass
	}

	data := types.ServiceData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "service",
					Name:             utils.ExtractName(service),
					CreatedTimestamp: utils.ExtractCreationTimestamp(service),
				},
				Namespace: utils.ExtractNamespace(service),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(service),
				Annotations: utils.ExtractAnnotations(service),
			},
		},
		Type:                                  string(service.Spec.Type),
		ClusterIP:                             service.Spec.ClusterIP,
		ExternalIP:                            externalIP,
		LoadBalancerIP:                        service.Spec.LoadBalancerIP,
		Ports:                                 ports,
		Selector:                              service.Spec.Selector,
		EndpointsCount:                        endpointsCount,
		LoadBalancerIngress:                   loadBalancerIngress,
		SessionAffinity:                       string(service.Spec.SessionAffinity),
		ExternalName:                          service.Spec.ExternalName,
		ExternalTrafficPolicy:                 externalTrafficPolicy,
		SessionAffinityClientIPTimeoutSeconds: sessionAffinityTimeout,
		AllocateLoadBalancerNodePorts:         allocateLoadBalancerNodePorts,
		LoadBalancerClass:                     loadBalancerClass,
		LoadBalancerSourceRanges:              service.Spec.LoadBalancerSourceRanges,
	}

	return data
}

// countEndpointsForService counts the number of endpoints for a given service
func (h *ServiceHandler) countEndpointsForService(namespace, serviceName string) int {
	// Get all endpoints from the cache
	endpoints := utils.SafeGetStoreList(h.endpointsInformer)

	for _, obj := range endpoints {
		endpoint, ok := obj.(*corev1.Endpoints)
		if !ok {
			continue
		}

		// Check if this endpoint matches the service
		if endpoint.Namespace == namespace && endpoint.Name == serviceName {
			// Count all addresses across all subsets
			totalAddresses := 0
			for _, subset := range endpoint.Subsets {
				if subset.Addresses != nil {
					totalAddresses += len(subset.Addresses)
				}
			}
			return totalAddresses
		}
	}

	return 0
}
