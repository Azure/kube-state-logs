// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	testutils "github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// createTestClusterRoleBinding creates a test cluster role binding with various configurations
func createTestClusterRoleBinding(name string) *rbacv1.ClusterRoleBinding {
	clusterRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test cluster role binding",
			},
			CreationTimestamp: metav1.Now(),
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "test-cluster-role",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "test-service-account",
				Namespace: "default",
			},
			{
				Kind: "User",
				Name: "test-user",
			},
			{
				Kind: "Group",
				Name: "test-group",
			},
		},
	}

	return clusterRoleBinding
}

func TestNewClusterRoleBindingHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewClusterRoleBindingHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestClusterRoleBindingHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewClusterRoleBindingHandler(client)
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

func TestClusterRoleBindingHandler_Collect(t *testing.T) {
	// Create test cluster role bindings
	clusterRoleBinding1 := createTestClusterRoleBinding("test-cluster-role-binding-1")
	clusterRoleBinding2 := createTestClusterRoleBinding("test-cluster-role-binding-2")

	// Create fake client with test cluster role bindings
	client := fake.NewSimpleClientset(clusterRoleBinding1, clusterRoleBinding2)
	handler := NewClusterRoleBindingHandler(client)
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

	// Test collecting all cluster role bindings
	ctx := context.Background()
	entries, err := handler.Collect(ctx, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Type assert to ClusterRoleBindingData for assertions
	entry, ok := entries[0].(types.ClusterRoleBindingData)
	if !ok {
		t.Fatalf("Expected ClusterRoleBindingData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}
}

func TestClusterRoleBindingHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewClusterRoleBindingHandler(client)
	clusterRoleBinding := createTestClusterRoleBinding("test-cluster-role-binding")
	entry := handler.createLogEntry(clusterRoleBinding)

	if entry.ResourceType != "clusterrolebinding" {
		t.Errorf("Expected resource type 'clusterrolebinding', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-cluster-role-binding" {
		t.Errorf("Expected name 'test-cluster-role-binding', got '%s'", entry.Name)
	}

	// Verify cluster role binding-specific fields
	if entry.RoleRef.Kind != "ClusterRole" {
		t.Errorf("Expected role ref kind 'ClusterRole', got '%s'", entry.RoleRef.Kind)
	}

	if entry.RoleRef.Name != "test-cluster-role" {
		t.Errorf("Expected role ref name 'test-cluster-role', got '%s'", entry.RoleRef.Name)
	}

	if entry.RoleRef.APIGroup != "rbac.authorization.k8s.io" {
		t.Errorf("Expected role ref API group 'rbac.authorization.k8s.io', got '%s'", entry.RoleRef.APIGroup)
	}

	if len(entry.Subjects) != 3 {
		t.Errorf("Expected 3 subjects, got %d", len(entry.Subjects))
	}

	// Verify subjects
	subject1 := entry.Subjects[0]
	if subject1.Kind != "ServiceAccount" {
		t.Errorf("Expected first subject kind 'ServiceAccount', got '%s'", subject1.Kind)
	}

	if subject1.Name != "test-service-account" {
		t.Errorf("Expected first subject name 'test-service-account', got '%s'", subject1.Name)
	}

	if subject1.Namespace != "default" {
		t.Errorf("Expected first subject namespace 'default', got '%s'", subject1.Namespace)
	}

	// Verify metadata
	if entry.Labels["app"] != "test-cluster-role-binding" {
		t.Errorf("Expected label 'app' to be 'test-cluster-role-binding', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test cluster role binding" {
		t.Errorf("Expected annotation 'description' to be 'test cluster role binding', got '%s'", entry.Annotations["description"])
	}
}
