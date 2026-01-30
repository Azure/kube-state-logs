// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

func TestSecretHandler(t *testing.T) {
	secret1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-secret",
			Namespace: "default",
			Labels:    map[string]string{"env": "prod"},
			Annotations: map[string]string{
				"purpose": "test",
			},
			CreationTimestamp: metav1.Now(),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			"password": []byte("supersecret"),
			"token":    []byte("tokendata"),
		},
	}

	secret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "docker-secret",
			Namespace:         "kube-system",
			Labels:            map[string]string{"env": "system"},
			CreationTimestamp: metav1.Now(),
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			".dockerconfigjson": []byte("{\"auths\":{}}"),
		},
	}

	tests := []struct {
		name           string
		secrets        []*corev1.Secret
		namespaces     []string
		expectedCount  int
		expectedNames  []string
		expectedFields map[string]interface{}
	}{
		{
			name:          "collect all secrets",
			secrets:       []*corev1.Secret{secret1, secret2},
			namespaces:    []string{},
			expectedCount: 2,
			expectedNames: []string{"my-secret", "docker-secret"},
		},
		{
			name:          "collect secrets from specific namespace",
			secrets:       []*corev1.Secret{secret1, secret2},
			namespaces:    []string{"default"},
			expectedCount: 1,
			expectedNames: []string{"my-secret"},
		},
		{
			name:          "collect secret with data keys",
			secrets:       []*corev1.Secret{secret1},
			namespaces:    []string{},
			expectedCount: 1,
			expectedNames: []string{"my-secret"},
			expectedFields: map[string]interface{}{
				"data_keys": []string{"password", "token"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.secrets))
			for i, s := range tt.secrets {
				objects[i] = s
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewSecretHandler(client)
			factory := informers.NewSharedInformerFactory(client, time.Hour)
			err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
			if err != nil {
				t.Fatalf("Failed to setup informer: %v", err)
			}
			factory.Start(context.Background().Done())
			if !cache.WaitForCacheSync(context.Background().Done(), handler.GetInformer().HasSynced) {
				t.Fatal("Failed to sync cache")
			}
			entries, err := handler.Collect(context.Background(), tt.namespaces)
			if err != nil {
				t.Fatalf("Failed to collect metrics: %v", err)
			}
			if len(entries) != tt.expectedCount {
				t.Errorf("Expected %d entries, got %d", tt.expectedCount, len(entries))
			}
			entryNames := make([]string, len(entries))
			for i, entry := range entries {
				secretData, ok := entry.(types.SecretData)
				if !ok {
					t.Fatalf("Expected SecretData type, got %T", entry)
				}
				entryNames[i] = secretData.Name
			}
			for _, expectedName := range tt.expectedNames {
				found := false
				for _, name := range entryNames {
					if name == expectedName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find secret with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				secretData, ok := entries[0].(types.SecretData)
				if !ok {
					t.Fatalf("Expected SecretData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "data_keys":
						expectedKeys := expectedValue.([]string)
						if len(secretData.DataKeys) != len(expectedKeys) {
							t.Errorf("Expected %d data keys, got %d", len(expectedKeys), len(secretData.DataKeys))
						}
						for i, key := range expectedKeys {
							if secretData.DataKeys[i] != key {
								t.Errorf("Expected data key %s at index %d, got %s", key, i, secretData.DataKeys[i])
							}
						}
					}
				}
			}
			for _, entry := range entries {
				secretData, ok := entry.(types.SecretData)
				if !ok {
					t.Fatalf("Expected SecretData type, got %T", entry)
				}
				if secretData.ResourceType != "secret" {
					t.Errorf("Expected resource type 'secret', got %s", secretData.ResourceType)
				}
				if secretData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if secretData.Namespace == "" {
					t.Error("Entry namespace should not be empty")
				}
				if secretData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if secretData.Type == "" {
					t.Error("Secret type should not be empty")
				}
				if secretData.DataKeys == nil {
					t.Error("dataKeys should not be nil")
				}
			}
		})
	}
}

func TestSecretHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewSecretHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	factory.Start(context.Background().Done())
	if !cache.WaitForCacheSync(context.Background().Done(), handler.GetInformer().HasSynced) {
		t.Fatal("Failed to sync cache")
	}
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

func TestSecretHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewSecretHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	invalidObj := &corev1.Pod{}
	handler.GetInformer().GetStore().Add(invalidObj)
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries with invalid object, got %d", len(entries))
	}
}

// createTestSecret creates a test secret with various configurations
func createTestSecret(name, namespace string, secretType corev1.SecretType) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test secret",
			},
			CreationTimestamp: metav1.Now(),
		},
		Type: secretType,
		Data: map[string][]byte{
			"username": []byte("admin"),
			"password": []byte("secret123"),
			"token":    []byte("eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9"),
		},
		StringData: map[string]string{
			"config": "apiVersion: v1\nkind: Config",
		},
	}

	return secret
}

func TestNewSecretHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewSecretHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestSecretHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewSecretHandler(client)
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

func TestSecretHandler_Collect(t *testing.T) {
	// Create test secrets
	secret1 := createTestSecret("test-secret-1", "default", corev1.SecretTypeOpaque)
	secret2 := createTestSecret("test-secret-2", "kube-system", corev1.SecretTypeTLS)

	// Create fake client with test secrets
	client := fake.NewSimpleClientset(secret1, secret2)
	handler := NewSecretHandler(client)
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

	// Test collecting all secrets
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

	// Type assert to SecretData for assertions
	entry, ok := entries[0].(types.SecretData)
	if !ok {
		t.Fatalf("Expected SecretData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestSecretHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewSecretHandler(client)
	secret := createTestSecret("test-secret", "default", corev1.SecretTypeOpaque)
	entry := handler.createLogEntry(secret)

	if entry.ResourceType != "secret" {
		t.Errorf("Expected resource type 'secret', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-secret" {
		t.Errorf("Expected name 'test-secret', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify secret-specific fields
	if entry.Type != "Opaque" {
		t.Errorf("Expected type 'Opaque', got '%s'", entry.Type)
	}

	if len(entry.DataKeys) != 4 {
		t.Errorf("Expected 4 data keys, got %d", len(entry.DataKeys))
	}

	// Check that all expected keys are present
	expectedKeys := map[string]bool{
		"username": true,
		"password": true,
		"token":    true,
		"config":   true,
	}

	for _, key := range entry.DataKeys {
		if !expectedKeys[key] {
			t.Errorf("Unexpected data key: %s", key)
		}
	}

	// Verify metadata
	if entry.Labels["app"] != "test-secret" {
		t.Errorf("Expected label 'app' to be 'test-secret', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test secret" {
		t.Errorf("Expected annotation 'description' to be 'test secret', got '%s'", entry.Annotations["description"])
	}
}

func TestSecretHandler_createLogEntry_TLS(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewSecretHandler(client)
	secret := createTestSecret("test-secret", "default", corev1.SecretTypeTLS)
	entry := handler.createLogEntry(secret)

	if entry.Type != "kubernetes.io/tls" {
		t.Errorf("Expected type 'kubernetes.io/tls', got '%s'", entry.Type)
	}
}

func TestSecretHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test secrets in different namespaces
	secret1 := createTestSecret("test-secret-1", "default", corev1.SecretTypeOpaque)
	secret2 := createTestSecret("test-secret-2", "kube-system", corev1.SecretTypeTLS)
	secret3 := createTestSecret("test-secret-3", "monitoring", corev1.SecretTypeDockerConfigJson)

	client := fake.NewSimpleClientset(secret1, secret2, secret3)
	handler := NewSecretHandler(client)
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
		entryData, ok := entry.(types.SecretData)
		if !ok {
			t.Fatalf("Expected SecretData type, got %T", entry)
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
