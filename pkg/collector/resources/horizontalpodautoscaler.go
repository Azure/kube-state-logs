// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// HorizontalPodAutoscalerHandler handles collection of horizontalpodautoscaler metrics
type HorizontalPodAutoscalerHandler struct {
	utils.BaseHandler
}

// NewHorizontalPodAutoscalerHandler creates a new HorizontalPodAutoscalerHandler
func NewHorizontalPodAutoscalerHandler(client kubernetes.Interface) *HorizontalPodAutoscalerHandler {
	return &HorizontalPodAutoscalerHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the horizontalpodautoscaler informer
func (h *HorizontalPodAutoscalerHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create horizontalpodautoscaler informer
	informer := factory.Autoscaling().V2().HorizontalPodAutoscalers().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers horizontalpodautoscaler metrics from the cluster (uses cache)
func (h *HorizontalPodAutoscalerHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all horizontalpodautoscalers from the cache
	hpas := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range hpas {
		hpa, ok := obj.(*autoscalingv2.HorizontalPodAutoscaler)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, hpa.Namespace) {
			continue
		}

		entry := h.createLogEntry(hpa)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a HorizontalPodAutoscalerData from an HPA
func (h *HorizontalPodAutoscalerHandler) createLogEntry(hpa *autoscalingv2.HorizontalPodAutoscaler) types.HorizontalPodAutoscalerData {
	// Extract target CPU and memory utilization
	var targetCPUUtilizationPercentage *int32
	var targetMemoryUtilizationPercentage *int32

	for _, metric := range hpa.Spec.Metrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType {
			if metric.Resource.Name == "cpu" && metric.Resource.Target.Type == autoscalingv2.UtilizationMetricType {
				targetCPUUtilizationPercentage = metric.Resource.Target.AverageUtilization
			}
			if metric.Resource.Name == "memory" && metric.Resource.Target.Type == autoscalingv2.UtilizationMetricType {
				targetMemoryUtilizationPercentage = metric.Resource.Target.AverageUtilization
			}
		}
	}

	// Extract current CPU and memory utilization
	var currentCPUUtilizationPercentage *int32
	var currentMemoryUtilizationPercentage *int32

	for _, metric := range hpa.Status.CurrentMetrics {
		if metric.Type == autoscalingv2.ResourceMetricSourceType {
			if metric.Resource.Name == "cpu" {
				currentCPUUtilizationPercentage = metric.Resource.Current.AverageUtilization
			}
			if metric.Resource.Name == "memory" {
				currentMemoryUtilizationPercentage = metric.Resource.Current.AverageUtilization
			}
		}
	}

	// Get HPA conditions in a single loop
	var conditionAbleToScale, conditionScalingActive, conditionScalingLimited *bool
	conditions := make(map[string]*bool)

	for _, condition := range hpa.Status.Conditions {
		val := utils.ConvertCoreConditionStatus(condition.Status)

		switch condition.Type {
		case autoscalingv2.AbleToScale:
			conditionAbleToScale = val
		case autoscalingv2.ScalingActive:
			conditionScalingActive = val
		case autoscalingv2.ScalingLimited:
			conditionScalingLimited = val
		default:
			// Add unknown conditions to the map
			conditions[string(condition.Type)] = val
		}
	}

	// Create data structure
	// Default min replicas is 1 when spec.minReplicas is nil
	// See: https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/
	minReplicas := int32(1)
	if hpa.Spec.MinReplicas != nil {
		minReplicas = *hpa.Spec.MinReplicas
	}

	data := types.HorizontalPodAutoscalerData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "horizontalpodautoscaler",
					Name:             utils.ExtractName(hpa),
					CreatedTimestamp: utils.ExtractCreationTimestamp(hpa),
				},
				Namespace: utils.ExtractNamespace(hpa),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(hpa),
				Annotations: utils.ExtractAnnotations(hpa),
			},
		},
		MinReplicas:                        &minReplicas,
		MaxReplicas:                        hpa.Spec.MaxReplicas,
		TargetCPUUtilizationPercentage:     targetCPUUtilizationPercentage,
		TargetMemoryUtilizationPercentage:  targetMemoryUtilizationPercentage,
		CurrentReplicas:                    hpa.Status.CurrentReplicas,
		DesiredReplicas:                    hpa.Status.DesiredReplicas,
		CurrentCPUUtilizationPercentage:    currentCPUUtilizationPercentage,
		CurrentMemoryUtilizationPercentage: currentMemoryUtilizationPercentage,
		ConditionAbleToScale:               conditionAbleToScale,
		ConditionScalingActive:             conditionScalingActive,
		ConditionScalingLimited:            conditionScalingLimited,
		ScaleTargetRef:                     hpa.Spec.ScaleTargetRef.Kind + "/" + hpa.Spec.ScaleTargetRef.Name,
		ScaleTargetKind:                    hpa.Spec.ScaleTargetRef.Kind,
		Conditions:                         conditions,
	}

	return data
}
