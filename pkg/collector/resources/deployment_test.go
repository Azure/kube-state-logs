// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	testutils "github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// createTestDeployment creates a test deployment with various configurations
func createTestDeployment(name, namespace string, replicas int32, strategyType appsv1.DeploymentStrategyType) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test deployment",
			},
			CreationTimestamp: metav1.Now(),
			Generation:        1,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: strategyType,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": name,
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
		Status: appsv1.DeploymentStatus{
			Replicas:            3,
			ReadyReplicas:       2,
			AvailableReplicas:   2,
			UnavailableReplicas: 1,
			UpdatedReplicas:     3,
			ObservedGeneration:  1,
			Conditions: []appsv1.DeploymentCondition{
				{
					Type:               appsv1.DeploymentAvailable,
					Status:             corev1.ConditionTrue,
					LastUpdateTime:     metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "MinimumReplicasAvailable",
					Message:            "Deployment has minimum availability.",
				},
				{
					Type:               appsv1.DeploymentProgressing,
					Status:             corev1.ConditionTrue,
					LastUpdateTime:     metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "NewReplicaSetAvailable",
					Message:            "ReplicaSet is available.",
				},
			},
		},
	}

	// Add rolling update strategy if specified
	if strategyType == appsv1.RollingUpdateDeploymentStrategyType {
		deployment.Spec.Strategy.RollingUpdate = &appsv1.RollingUpdateDeployment{
			MaxSurge:       &intstr.IntOrString{Type: intstr.Int, IntVal: 1},
			MaxUnavailable: &intstr.IntOrString{Type: intstr.Int, IntVal: 0},
		}
	}

	return deployment
}

func TestNewDeploymentHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewDeploymentHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestDeploymentHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewDeploymentHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify informer is set up
	if handler.GetInformer() == nil {
		t.Error("Expected informer to be set up")
	}
}

func TestDeploymentHandler_Collect(t *testing.T) {
	// Create test deployments
	deployment1 := createTestDeployment("test-deployment-1", "default", 3, appsv1.RollingUpdateDeploymentStrategyType)
	deployment2 := createTestDeployment("test-deployment-2", "kube-system", 2, appsv1.RecreateDeploymentStrategyType)

	// Create fake client with test deployments
	client := fake.NewSimpleClientset(deployment1, deployment2)
	handler := NewDeploymentHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	// Setup informer
	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}

	// Start the factory to populate the cache
	factory.Start(nil)
	factory.WaitForCacheSync(nil)

	// Test collecting all deployments
	ctx := context.Background()
	entries, err := handler.Collect(ctx, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Test collecting from specific namespace
	entries, err = handler.Collect(ctx, []string{"default"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for default namespace, got %d", len(entries))
	}

	// Type assert to DeploymentData for assertions
	entry, ok := entries[0].(types.DeploymentData)
	if !ok {
		t.Fatalf("Expected DeploymentData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestDeploymentHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewDeploymentHandler(client)

	// Test deployment with rolling update strategy
	deployment := createTestDeployment("test-deployment", "default", 3, appsv1.RollingUpdateDeploymentStrategyType)
	entry := handler.createLogEntry(deployment)

	// Verify basic fields
	if entry.ResourceType != "deployment" {
		t.Errorf("Expected resource type 'deployment', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-deployment" {
		t.Errorf("Expected name 'test-deployment', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify deployment-specific fields
	if entry.DesiredReplicas != 3 {
		t.Errorf("Expected desired replicas 3, got %d", entry.DesiredReplicas)
	}

	if entry.CurrentReplicas != 3 {
		t.Errorf("Expected current replicas 3, got %d", entry.CurrentReplicas)
	}

	if entry.ReadyReplicas != 2 {
		t.Errorf("Expected ready replicas 2, got %d", entry.ReadyReplicas)
	}

	if entry.AvailableReplicas != 2 {
		t.Errorf("Expected available replicas 2, got %d", entry.AvailableReplicas)
	}

	if entry.UnavailableReplicas != 1 {
		t.Errorf("Expected unavailable replicas 1, got %d", entry.UnavailableReplicas)
	}

	if entry.UpdatedReplicas != 3 {
		t.Errorf("Expected updated replicas 3, got %d", entry.UpdatedReplicas)
	}

	if entry.StrategyType != "RollingUpdate" {
		t.Errorf("Expected strategy type 'RollingUpdate', got '%s'", entry.StrategyType)
	}

	if entry.StrategyRollingUpdateMaxSurge != 1 {
		t.Errorf("Expected max surge 1, got %d", entry.StrategyRollingUpdateMaxSurge)
	}

	if entry.StrategyRollingUpdateMaxUnavailable != 0 {
		t.Errorf("Expected max unavailable 0, got %d", entry.StrategyRollingUpdateMaxUnavailable)
	}

	// Verify conditions
	if entry.ConditionAvailable == nil || !*entry.ConditionAvailable {
		t.Error("Expected condition Available to be true")
	}

	if entry.ConditionProgressing == nil || !*entry.ConditionProgressing {
		t.Error("Expected condition Progressing to be true")
	}

	if entry.ConditionReplicaFailure != nil && *entry.ConditionReplicaFailure {
		t.Error("Expected condition ReplicaFailure to be false or nil")
	}

	// Verify metadata
	if entry.Labels["app"] != "test-deployment" {
		t.Errorf("Expected label 'app' to be 'test-deployment', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test deployment" {
		t.Errorf("Expected annotation 'description' to be 'test deployment', got '%s'", entry.Annotations["description"])
	}
}

func TestDeploymentHandler_createLogEntry_RecreateStrategy(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewDeploymentHandler(client)

	// Test deployment with recreate strategy
	deployment := createTestDeployment("test-deployment", "default", 2, appsv1.RecreateDeploymentStrategyType)
	entry := handler.createLogEntry(deployment)

	if entry.StrategyType != "Recreate" {
		t.Errorf("Expected strategy type 'Recreate', got '%s'", entry.StrategyType)
	}

	// Rolling update fields should be 0 for recreate strategy
	if entry.StrategyRollingUpdateMaxSurge != 0 {
		t.Errorf("Expected max surge 0 for recreate strategy, got %d", entry.StrategyRollingUpdateMaxSurge)
	}

	if entry.StrategyRollingUpdateMaxUnavailable != 0 {
		t.Errorf("Expected max unavailable 0 for recreate strategy, got %d", entry.StrategyRollingUpdateMaxUnavailable)
	}
}

func TestDeploymentHandler_createLogEntry_NilReplicas(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewDeploymentHandler(client)

	// Create deployment with nil replicas (should default to 1)
	deployment := createTestDeployment("test-deployment", "default", 0, appsv1.RollingUpdateDeploymentStrategyType)
	deployment.Spec.Replicas = nil // Set to nil explicitly

	entry := handler.createLogEntry(deployment)

	// Should default to 1 when replicas is nil
	if entry.DesiredReplicas != 1 {
		t.Errorf("Expected desired replicas 1 when nil, got %d", entry.DesiredReplicas)
	}
}

func getConditionStatus(conditions []appsv1.DeploymentCondition, conditionType string) bool {
	for _, condition := range conditions {
		if string(condition.Type) == conditionType {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func TestDeploymentHandler_getConditionStatus(t *testing.T) {
	conditions := []appsv1.DeploymentCondition{
		{
			Type:   appsv1.DeploymentAvailable,
			Status: corev1.ConditionTrue,
		},
		{
			Type:   appsv1.DeploymentProgressing,
			Status: corev1.ConditionFalse,
		},
	}

	// Test available condition
	if !getConditionStatus(conditions, "Available") {
		t.Error("Expected Available condition to be true")
	}

	// Test progressing condition
	if getConditionStatus(conditions, "Progressing") {
		t.Error("Expected Progressing condition to be false")
	}

	// Test non-existent condition
	if getConditionStatus(conditions, "NonExistent") {
		t.Error("Expected non-existent condition to be false")
	}
}

func TestDeploymentHandler_Collect_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewDeploymentHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}

	factory.Start(nil)
	factory.WaitForCacheSync(nil)

	ctx := context.Background()
	entries, err := handler.Collect(ctx, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("Expected 0 entries for empty cache, got %d", len(entries))
	}
}

func TestDeploymentHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test deployments in different namespaces
	deployment1 := createTestDeployment("test-deployment-1", "default", 3, appsv1.RollingUpdateDeploymentStrategyType)
	deployment2 := createTestDeployment("test-deployment-2", "kube-system", 2, appsv1.RecreateDeploymentStrategyType)
	deployment3 := createTestDeployment("test-deployment-3", "monitoring", 1, appsv1.RollingUpdateDeploymentStrategyType)

	client := fake.NewSimpleClientset(deployment1, deployment2, deployment3)
	handler := NewDeploymentHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}

	factory.Start(nil)
	factory.WaitForCacheSync(nil)

	ctx := context.Background()

	// Test multiple namespace filtering
	entries, err := handler.Collect(ctx, []string{"default", "monitoring"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries for default and monitoring namespaces, got %d", len(entries))
	}

	// Verify correct namespaces
	namespaces := make(map[string]bool)
	for _, entry := range entries {
		entryData, ok := entry.(types.DeploymentData)
		if !ok {
			t.Fatalf("Expected DeploymentData type, got %T", entry)
		}
		namespaces[entryData.Namespace] = true
	}

	if !namespaces["default"] {
		t.Error("Expected entry from default namespace")
	}

	if !namespaces["monitoring"] {
		t.Error("Expected entry from monitoring namespace")
	}

	if namespaces["kube-system"] {
		t.Error("Did not expect entry from kube-system namespace")
	}
}
