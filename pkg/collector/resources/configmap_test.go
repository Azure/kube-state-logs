// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// createTestConfigMap creates a test configmap with various configurations
func createTestConfigMap(name, namespace string) *corev1.ConfigMap {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test configmap",
			},
			CreationTimestamp: metav1.Now(),
		},
		Data: map[string]string{
			"config.yaml": "apiVersion: v1\nkind: Config",
			"settings.json": `{
				"debug": true,
				"port": 8080
			}`,
			"database.conf": "host=localhost\nport=5432",
		},
		BinaryData: map[string][]byte{
			"binary.dat": []byte{0x01, 0x02, 0x03, 0x04},
		},
	}

	return configMap
}

func TestNewConfigMapHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewConfigMapHandler(client, false)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestConfigMapHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewConfigMapHandler(client, false)
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

func TestConfigMapHandler_Collect(t *testing.T) {
	// Create test configmaps
	configMap1 := createTestConfigMap("test-config-1", "default")
	configMap2 := createTestConfigMap("test-config-2", "kube-system")

	// Create fake client with test configmaps
	client := fake.NewSimpleClientset(configMap1, configMap2)
	handler := NewConfigMapHandler(client, false)
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

	// Test collecting all configmaps
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

	// Type assert to ConfigMapData for assertions
	entry, ok := entries[0].(types.ConfigMapData)
	if !ok {
		t.Fatalf("Expected ConfigMapData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestConfigMapHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewConfigMapHandler(client, false)
	configMap := createTestConfigMap("test-config", "default")
	entry := handler.createLogEntry(configMap)

	if entry.ResourceType != "configmap" {
		t.Errorf("Expected resource type 'configmap', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-config" {
		t.Errorf("Expected name 'test-config', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify configmap-specific fields
	if len(entry.DataKeys) != 4 {
		t.Errorf("Expected 4 data keys, got %d", len(entry.DataKeys))
	}
	if entry.Data != nil {
		t.Errorf("Expected data values to be omitted, got %v", entry.Data)
	}

	// Check that all expected keys are present
	expectedKeys := map[string]bool{
		"config.yaml":   true,
		"settings.json": true,
		"database.conf": true,
		"binary.dat":    true,
	}

	for _, key := range entry.DataKeys {
		if !expectedKeys[key] {
			t.Errorf("Unexpected data key: %s", key)
		}
	}

	// Verify metadata
	if entry.Labels["app"] != "test-config" {
		t.Errorf("Expected label 'app' to be 'test-config', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test configmap" {
		t.Errorf("Expected annotation 'description' to be 'test configmap', got '%s'", entry.Annotations["description"])
	}
}

func TestConfigMapHandler_createLogEntry_WithValues(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewConfigMapHandler(client, true)
	configMap := createTestConfigMap("test-config", "default")
	entry := handler.createLogEntry(configMap)

	if entry.Data == nil {
		t.Fatal("Expected data values to be included, got nil")
	}

	if entry.Data["config.yaml"] != configMap.Data["config.yaml"] {
		t.Errorf("Expected value for config.yaml, got '%s'", entry.Data["config.yaml"])
	}

	if _, exists := entry.Data["binary.dat"]; exists {
		t.Errorf("Did not expect binary data to be included")
	}
}

func TestConfigMapHandler_createLogEntry_WithOwnerReference(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewConfigMapHandler(client, false)
	configMap := createTestConfigMap("test-config", "default")
	configMap.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       "test-deployment",
			UID:        "test-uid",
		},
	}
	entry := handler.createLogEntry(configMap)

	// ConfigMap uses NamespacedLabeledMetadata, which doesn't include CreatedByKind/CreatedByName
	// Verify that the entry was created successfully with owner reference information
	if entry.Name != "test-config" {
		t.Errorf("Expected name 'test-config', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestConfigMapHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test configmaps in different namespaces
	configmap1 := createTestConfigMap("test-configmap-1", "default")
	configmap2 := createTestConfigMap("test-configmap-2", "kube-system")
	configmap3 := createTestConfigMap("test-configmap-3", "monitoring")

	client := fake.NewSimpleClientset(configmap1, configmap2, configmap3)
	handler := NewConfigMapHandler(client, false)
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
		entryData, ok := entry.(types.ConfigMapData)
		if !ok {
			t.Fatalf("Expected ConfigMapData type, got %T", entry)
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
