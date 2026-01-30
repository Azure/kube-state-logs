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

// createTestClusterRole creates a test cluster role with various configurations
func createTestClusterRole(name string) *rbacv1.ClusterRole {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test cluster role",
			},
			CreationTimestamp: metav1.Now(),
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"apps"},
				Resources: []string{"deployments"},
				Verbs:     []string{"get", "list", "watch"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"pods"},
				Verbs:     []string{"get", "list", "watch", "create", "update", "patch", "delete"},
			},
			{
				APIGroups: []string{""},
				Resources: []string{"services"},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	return clusterRole
}

func TestNewClusterRoleHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewClusterRoleHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestClusterRoleHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewClusterRoleHandler(client)
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

func TestClusterRoleHandler_Collect(t *testing.T) {
	// Create test cluster roles
	clusterRole1 := createTestClusterRole("test-cluster-role-1")
	clusterRole2 := createTestClusterRole("test-cluster-role-2")

	// Create fake client with test cluster roles
	client := fake.NewSimpleClientset(clusterRole1, clusterRole2)
	handler := NewClusterRoleHandler(client)
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

	// Test collecting all cluster roles
	ctx := context.Background()
	entries, err := handler.Collect(ctx, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Type assert to ClusterRoleData for assertions
	entry, ok := entries[0].(types.ClusterRoleData)
	if !ok {
		t.Fatalf("Expected ClusterRoleData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}
}

func TestClusterRoleHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewClusterRoleHandler(client)
	clusterRole := createTestClusterRole("test-cluster-role")
	entry := handler.createLogEntry(clusterRole)

	if entry.ResourceType != "clusterrole" {
		t.Errorf("Expected resource type 'clusterrole', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-cluster-role" {
		t.Errorf("Expected name 'test-cluster-role', got '%s'", entry.Name)
	}

	// Verify cluster role-specific fields
	if len(entry.Rules) != 3 {
		t.Errorf("Expected 3 rules, got %d", len(entry.Rules))
	}

	// Verify first rule
	rule1 := entry.Rules[0]
	if len(rule1.APIGroups) != 1 || rule1.APIGroups[0] != "apps" {
		t.Errorf("Expected first rule to have APIGroups ['apps'], got %v", rule1.APIGroups)
	}

	if len(rule1.Resources) != 1 || rule1.Resources[0] != "deployments" {
		t.Errorf("Expected first rule to have Resources ['deployments'], got %v", rule1.Resources)
	}

	if len(rule1.Verbs) != 3 {
		t.Errorf("Expected first rule to have 3 verbs, got %d", len(rule1.Verbs))
	}

	// Verify metadata
	if entry.Labels["app"] != "test-cluster-role" {
		t.Errorf("Expected label 'app' to be 'test-cluster-role', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test cluster role" {
		t.Errorf("Expected annotation 'description' to be 'test cluster role', got '%s'", entry.Annotations["description"])
	}
}
