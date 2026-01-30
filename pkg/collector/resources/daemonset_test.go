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
)

// createTestDaemonSet creates a test daemonset with various configurations
func createTestDaemonSet(name, namespace string) *appsv1.DaemonSet {
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test daemonset",
			},
			CreationTimestamp: metav1.Now(),
			Generation:        1,
		},
		Spec: appsv1.DaemonSetSpec{
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
			UpdateStrategy: appsv1.DaemonSetUpdateStrategy{
				Type: appsv1.RollingUpdateDaemonSetStrategyType,
			},
		},
		Status: appsv1.DaemonSetStatus{
			CurrentNumberScheduled: 3,
			NumberMisscheduled:     0,
			DesiredNumberScheduled: 3,
			NumberReady:            2,
			UpdatedNumberScheduled: 3,
			NumberAvailable:        2,
			NumberUnavailable:      1,
			ObservedGeneration:     1,
			Conditions: []appsv1.DaemonSetCondition{
				{
					Type:               "Available",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "DaemonSetAvailable",
					Message:            "DaemonSet is available",
				},
				{
					Type:               "Progressing",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "DaemonSetProgressing",
					Message:            "DaemonSet is progressing",
				},
			},
		},
	}

	return daemonSet
}

func TestDaemonSetHandler_Collect(t *testing.T) {
	// Create test daemonsets
	daemonSet1 := createTestDaemonSet("test-daemonset-1", "default")
	daemonSet2 := createTestDaemonSet("test-daemonset-2", "kube-system")

	// Create fake client with test daemonsets
	client := fake.NewSimpleClientset(daemonSet1, daemonSet2)
	handler := NewDaemonSetHandler(client)
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

	// Test collecting all daemonsets
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

	// Type assert to DaemonSetData for assertions
	entry, ok := entries[0].(types.DaemonSetData)
	if !ok {
		t.Fatalf("Expected DaemonSetData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestDaemonSetHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewDaemonSetHandler(client)
	daemonSet := createTestDaemonSet("test-daemonset", "default")
	entry := handler.createLogEntry(daemonSet)

	if entry.ResourceType != "daemonset" {
		t.Errorf("Expected resource type 'daemonset', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-daemonset" {
		t.Errorf("Expected name 'test-daemonset', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify daemonset-specific fields
	if entry.DesiredNumberScheduled != 3 {
		t.Errorf("Expected desired number scheduled 3, got %d", entry.DesiredNumberScheduled)
	}

	if entry.CurrentNumberScheduled != 3 {
		t.Errorf("Expected current number scheduled 3, got %d", entry.CurrentNumberScheduled)
	}

	if entry.NumberReady != 2 {
		t.Errorf("Expected number ready 2, got %d", entry.NumberReady)
	}

	if entry.NumberAvailable != 2 {
		t.Errorf("Expected number available 2, got %d", entry.NumberAvailable)
	}

	if entry.NumberUnavailable != 1 {
		t.Errorf("Expected number unavailable 1, got %d", entry.NumberUnavailable)
	}

	if entry.UpdatedNumberScheduled != 3 {
		t.Errorf("Expected updated number scheduled 3, got %d", entry.UpdatedNumberScheduled)
	}

	if entry.NumberMisscheduled != 0 {
		t.Errorf("Expected number misscheduled 0, got %d", entry.NumberMisscheduled)
	}

	if entry.ObservedGeneration != 1 {
		t.Errorf("Expected observed generation 1, got %d", entry.ObservedGeneration)
	}

	if entry.UpdateStrategy != "RollingUpdate" {
		t.Errorf("Expected update strategy 'RollingUpdate', got '%s'", entry.UpdateStrategy)
	}

	// Verify conditions
	if entry.ConditionAvailable == nil || !*entry.ConditionAvailable {
		t.Error("Expected ConditionAvailable to be true")
	}

	if entry.ConditionProgressing == nil || !*entry.ConditionProgressing {
		t.Error("Expected ConditionProgressing to be true")
	}

	if entry.ConditionReplicaFailure != nil && *entry.ConditionReplicaFailure {
		t.Error("Expected ConditionReplicaFailure to be false or nil")
	}

	// Verify metadata
	if entry.Labels["app"] != "test-daemonset" {
		t.Errorf("Expected label 'app' to be 'test-daemonset', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test daemonset" {
		t.Errorf("Expected annotation 'description' to be 'test daemonset', got '%s'", entry.Annotations["description"])
	}
}

func TestDaemonSetHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test daemonsets in different namespaces
	daemonSet1 := createTestDaemonSet("test-daemonset-1", "default")
	daemonSet2 := createTestDaemonSet("test-daemonset-2", "kube-system")
	daemonSet3 := createTestDaemonSet("test-daemonset-3", "monitoring")

	client := fake.NewSimpleClientset(daemonSet1, daemonSet2, daemonSet3)
	handler := NewDaemonSetHandler(client)
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
		entryData, ok := entry.(types.DaemonSetData)
		if !ok {
			t.Fatalf("Expected DaemonSetData type, got %T", entry)
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
