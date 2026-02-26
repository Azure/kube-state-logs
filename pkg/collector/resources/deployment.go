// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// DeploymentHandler handles collection of deployment metrics
type DeploymentHandler struct {
	utils.BaseHandler
}

// NewDeploymentHandler creates a new DeploymentHandler
func NewDeploymentHandler(client kubernetes.Interface) *DeploymentHandler {
	return &DeploymentHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the deployment informer
func (h *DeploymentHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create deployment informer
	informer := factory.Apps().V1().Deployments().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers deployment metrics from the cluster (uses cache)
func (h *DeploymentHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all deployments from the cache
	deployments := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range deployments {
		deployment, ok := obj.(*appsv1.Deployment)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, deployment.Namespace) {
			continue
		}

		if !h.MatchesSelectors(deployment) {
			continue
		}

		entry := h.createLogEntry(deployment)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a DeploymentData from a deployment
func (h *DeploymentHandler) createLogEntry(deployment *appsv1.Deployment) types.DeploymentData {
	// Get desired replicas (default to 1 when spec.replicas is nil)
	desiredReplicas := int32(1)
	if deployment.Spec.Replicas != nil {
		desiredReplicas = *deployment.Spec.Replicas
	}

	// Get strategy info
	strategyType := string(deployment.Spec.Strategy.Type)
	strategyRollingUpdateMaxSurge := int32(0)
	strategyRollingUpdateMaxUnavailable := int32(0)

	// Calculate rolling update values correctly
	if deployment.Spec.Strategy.RollingUpdate != nil {
		if deployment.Spec.Strategy.RollingUpdate.MaxSurge != nil {
			maxSurge, err := intstr.GetScaledValueFromIntOrPercent(deployment.Spec.Strategy.RollingUpdate.MaxSurge, int(desiredReplicas), true)
			if err == nil {
				strategyRollingUpdateMaxSurge = int32(maxSurge)
			}
		}
		if deployment.Spec.Strategy.RollingUpdate.MaxUnavailable != nil {
			maxUnavailable, err := intstr.GetScaledValueFromIntOrPercent(deployment.Spec.Strategy.RollingUpdate.MaxUnavailable, int(desiredReplicas), false)
			if err == nil {
				strategyRollingUpdateMaxUnavailable = int32(maxUnavailable)
			}
		}
	}

	// Get replica counts
	currentReplicas := deployment.Status.Replicas
	readyReplicas := deployment.Status.ReadyReplicas
	availableReplicas := deployment.Status.AvailableReplicas
	unavailableReplicas := deployment.Status.UnavailableReplicas
	updatedReplicas := deployment.Status.UpdatedReplicas

	// Check conditions in a single loop
	var conditionAvailable, conditionProgressing, conditionReplicaFailure *bool
	conditions := make(map[string]*bool)

	for _, condition := range deployment.Status.Conditions {
		val := utils.ConvertCoreConditionStatus(condition.Status)

		switch condition.Type {
		case "Available":
			conditionAvailable = val
		case "Progressing":
			conditionProgressing = val
		case "ReplicaFailure":
			conditionReplicaFailure = val
		default:
			// Add unknown conditions to the map
			conditions[string(condition.Type)] = val
		}
	}

	// Get spec fields
	minReadySeconds := deployment.Spec.MinReadySeconds
	revisionHistoryLimit := int32(10) // Default value
	if deployment.Spec.RevisionHistoryLimit != nil {
		revisionHistoryLimit = *deployment.Spec.RevisionHistoryLimit
	}
	progressDeadlineSeconds := int32(600) // Default value
	if deployment.Spec.ProgressDeadlineSeconds != nil {
		progressDeadlineSeconds = *deployment.Spec.ProgressDeadlineSeconds
	}

	// Get status fields
	observedGeneration := deployment.Status.ObservedGeneration
	collisionCount := int32(0)
	if deployment.Status.CollisionCount != nil {
		collisionCount = *deployment.Status.CollisionCount
	}

	return types.DeploymentData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "deployment",
					Name:             utils.ExtractName(deployment),
					CreatedTimestamp: utils.ExtractCreationTimestamp(deployment),
				},
				Namespace: utils.ExtractNamespace(deployment),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(deployment),
				Annotations: utils.ExtractAnnotations(deployment),
			},
		},
		// Replica counts
		DesiredReplicas:     desiredReplicas,
		CurrentReplicas:     currentReplicas,
		ReadyReplicas:       readyReplicas,
		AvailableReplicas:   availableReplicas,
		UnavailableReplicas: unavailableReplicas,
		UpdatedReplicas:     updatedReplicas,

		// Deployment status
		ObservedGeneration: observedGeneration,
		CollisionCount:     collisionCount,

		// Strategy info
		StrategyType:                        strategyType,
		StrategyRollingUpdateMaxSurge:       strategyRollingUpdateMaxSurge,
		StrategyRollingUpdateMaxUnavailable: strategyRollingUpdateMaxUnavailable,

		// Most common conditions (for easy access)
		ConditionAvailable:      conditionAvailable,
		ConditionProgressing:    conditionProgressing,
		ConditionReplicaFailure: conditionReplicaFailure,

		// All other conditions (excluding the top-level ones)
		Conditions: conditions,

		// Spec fields
		Paused:                  deployment.Spec.Paused,
		MinReadySeconds:         minReadySeconds,
		RevisionHistoryLimit:    revisionHistoryLimit,
		ProgressDeadlineSeconds: progressDeadlineSeconds,

		// Metadata
		MetadataGeneration: utils.ExtractGeneration(deployment),
	}
}
