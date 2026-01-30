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
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	testutils "github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// createTestReplicaSet creates a test replicaset with various configurations
func createTestReplicaSet(name, namespace string, replicas int32) *appsv1.ReplicaSet {
	replicaSet := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test replicaset",
			},
			CreationTimestamp: metav1.Now(),
			Generation:        1,
		},
		Spec: appsv1.ReplicaSetSpec{
			Replicas: &replicas,
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
		Status: appsv1.ReplicaSetStatus{
			Replicas:             3,
			ReadyReplicas:        2,
			AvailableReplicas:    2,
			FullyLabeledReplicas: 3,
			ObservedGeneration:   1,
			Conditions: []appsv1.ReplicaSetCondition{
				{
					Type:               appsv1.ReplicaSetReplicaFailure,
					Status:             corev1.ConditionFalse,
					LastTransitionTime: metav1.Now(),
					Reason:             "ReplicaSetAvailable",
					Message:            "ReplicaSet is available",
				},
			},
		},
	}

	return replicaSet
}

func TestNewReplicaSetHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewReplicaSetHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestReplicaSetHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewReplicaSetHandler(client)
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

func TestReplicaSetHandler_Collect(t *testing.T) {
	// Create test replicasets
	replicaSet1 := createTestReplicaSet("test-replicaset-1", "default", 3)
	replicaSet2 := createTestReplicaSet("test-replicaset-2", "kube-system", 2)

	// Create fake client with test replicasets
	client := fake.NewSimpleClientset(replicaSet1, replicaSet2)
	handler := NewReplicaSetHandler(client)
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

	// Test collecting all replicasets
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

	// Type assert to ReplicaSetData for assertions
	entry, ok := entries[0].(types.ReplicaSetData)
	if !ok {
		t.Fatalf("Expected ReplicaSetData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestReplicaSetHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewReplicaSetHandler(client)
	replicaSet := createTestReplicaSet("test-replicaset", "default", 3)
	entry := handler.createLogEntry(replicaSet)

	if entry.ResourceType != "replicaset" {
		t.Errorf("Expected resource type 'replicaset', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-replicaset" {
		t.Errorf("Expected name 'test-replicaset', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify replicaset-specific fields
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

	if entry.FullyLabeledReplicas != 3 {
		t.Errorf("Expected fully labeled replicas 3, got %d", entry.FullyLabeledReplicas)
	}

	if entry.ObservedGeneration != 1 {
		t.Errorf("Expected observed generation 1, got %d", entry.ObservedGeneration)
	}

	// Verify conditions
	if entry.ConditionAvailable != nil && *entry.ConditionAvailable {
		t.Error("Expected ConditionAvailable to be false or nil")
	}

	if entry.ConditionProgressing != nil && *entry.ConditionProgressing {
		t.Error("Expected ConditionProgressing to be false or nil")
	}

	if entry.ConditionReplicaFailure != nil && *entry.ConditionReplicaFailure {
		t.Error("Expected ConditionReplicaFailure to be false or nil")
	}

	// Verify metadata
	if entry.Labels["app"] != "test-replicaset" {
		t.Errorf("Expected label 'app' to be 'test-replicaset', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test replicaset" {
		t.Errorf("Expected annotation 'description' to be 'test replicaset', got '%s'", entry.Annotations["description"])
	}
}

func TestReplicaSetHandler_createLogEntry_WithOwnerReference(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewReplicaSetHandler(client)
	replicaSet := createTestReplicaSet("test-replicaset", "default", 3)
	replicaSet.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "test-deployment",
			UID:        "test-uid",
		},
	}
	entry := handler.createLogEntry(replicaSet)

	if entry.CreatedByKind != "Deployment" {
		t.Errorf("Expected created by kind 'Deployment', got '%s'", entry.CreatedByKind)
	}

	if entry.CreatedByName != "test-deployment" {
		t.Errorf("Expected created by name 'test-deployment', got '%s'", entry.CreatedByName)
	}
}

func TestReplicaSetHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test replicasets in different namespaces
	replicaSet1 := createTestReplicaSet("test-replicaset-1", "default", 3)
	replicaSet2 := createTestReplicaSet("test-replicaset-2", "kube-system", 2)
	replicaSet3 := createTestReplicaSet("test-replicaset-3", "monitoring", 1)

	client := fake.NewSimpleClientset(replicaSet1, replicaSet2, replicaSet3)
	handler := NewReplicaSetHandler(client)
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
		entryData, ok := entry.(types.ReplicaSetData)
		if !ok {
			t.Fatalf("Expected ReplicaSetData type, got %T", entry)
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
