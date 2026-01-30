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
)

func TestServiceAccountHandler(t *testing.T) {
	automountTrue := true
	automountFalse := false

	sa1 := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "default",
			Namespace:         "default",
			Labels:            map[string]string{"env": "prod"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Secrets: []corev1.ObjectReference{
			{Name: "default-token-abc123"},
		},
		ImagePullSecrets: []corev1.LocalObjectReference{
			{Name: "docker-registry-secret"},
		},
		AutomountServiceAccountToken: &automountTrue,
	}

	sa2 := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "app-sa",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Secrets: []corev1.ObjectReference{
			{Name: "app-sa-token-xyz789"},
			{Name: "app-sa-token-def456"},
		},
		AutomountServiceAccountToken: &automountFalse,
	}

	saDefault := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "default-automount",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		},
		// AutomountServiceAccountToken is nil, should default to true
	}

	tests := []struct {
		name            string
		serviceAccounts []*corev1.ServiceAccount
		namespaces      []string
		expectedCount   int
		expectedNames   []string
		expectedFields  map[string]interface{}
	}{
		{
			name:            "collect all service accounts",
			serviceAccounts: []*corev1.ServiceAccount{sa1, sa2},
			namespaces:      []string{},
			expectedCount:   2,
			expectedNames:   []string{"default", "app-sa"},
		},
		{
			name:            "collect service accounts from specific namespace",
			serviceAccounts: []*corev1.ServiceAccount{sa1, sa2},
			namespaces:      []string{"default"},
			expectedCount:   1,
			expectedNames:   []string{"default"},
		},
		{
			name:            "collect service account with secrets and image pull secrets",
			serviceAccounts: []*corev1.ServiceAccount{sa1},
			namespaces:      []string{},
			expectedCount:   1,
			expectedNames:   []string{"default"},
			expectedFields: map[string]interface{}{
				"secrets":                         []string{"default-token-abc123"},
				"image_pull_secrets":              []string{"docker-registry-secret"},
				"automount_service_account_token": true,
			},
		},
		{
			name:            "collect service account with multiple secrets and disabled automount",
			serviceAccounts: []*corev1.ServiceAccount{sa2},
			namespaces:      []string{},
			expectedCount:   1,
			expectedNames:   []string{"app-sa"},
			expectedFields: map[string]interface{}{
				"secrets":                         []string{"app-sa-token-xyz789", "app-sa-token-def456"},
				"automount_service_account_token": false,
			},
		},
		{
			name:            "collect service account with default automount (nil)",
			serviceAccounts: []*corev1.ServiceAccount{saDefault},
			namespaces:      []string{},
			expectedCount:   1,
			expectedNames:   []string{"default-automount"},
			expectedFields: map[string]interface{}{
				"automount_service_account_token": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.serviceAccounts))
			for i, sa := range tt.serviceAccounts {
				objects[i] = sa
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewServiceAccountHandler(client)
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
				serviceAccountData, ok := entry.(types.ServiceAccountData)
				if !ok {
					t.Fatalf("Expected ServiceAccountData type, got %T", entry)
				}
				entryNames[i] = serviceAccountData.Name
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
					t.Errorf("Expected to find service account with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				serviceAccountData, ok := entries[0].(types.ServiceAccountData)
				if !ok {
					t.Fatalf("Expected ServiceAccountData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "secrets":
						expectedSecrets := expectedValue.([]string)
						if len(serviceAccountData.Secrets) != len(expectedSecrets) {
							t.Errorf("Expected %d secrets, got %d", len(expectedSecrets), len(serviceAccountData.Secrets))
						}
						for i, secret := range expectedSecrets {
							if serviceAccountData.Secrets[i] != secret {
								t.Errorf("Expected secret %s at index %d, got %s", secret, i, serviceAccountData.Secrets[i])
							}
						}
					case "image_pull_secrets":
						expectedImagePullSecrets := expectedValue.([]string)
						if len(serviceAccountData.ImagePullSecrets) != len(expectedImagePullSecrets) {
							t.Errorf("Expected %d image pull secrets, got %d", len(expectedImagePullSecrets), len(serviceAccountData.ImagePullSecrets))
						}
						for i, secret := range expectedImagePullSecrets {
							if serviceAccountData.ImagePullSecrets[i] != secret {
								t.Errorf("Expected image pull secret %s at index %d, got %s", secret, i, serviceAccountData.ImagePullSecrets[i])
							}
						}
					case "automount_service_account_token":
						if *serviceAccountData.AutomountServiceAccountToken != expectedValue.(bool) {
							t.Errorf("Expected automount_service_account_token %v, got %v", expectedValue, *serviceAccountData.AutomountServiceAccountToken)
						}
					}
				}
			}
			for _, entry := range entries {
				serviceAccountData, ok := entry.(types.ServiceAccountData)
				if !ok {
					t.Fatalf("Expected ServiceAccountData type, got %T", entry)
				}
				if serviceAccountData.ResourceType != "serviceaccount" {
					t.Errorf("Expected resource type 'serviceaccount', got %s", serviceAccountData.ResourceType)
				}
				if serviceAccountData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if serviceAccountData.Namespace == "" {
					t.Error("Entry namespace should not be empty")
				}
				if serviceAccountData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if serviceAccountData.Secrets == nil {
					t.Error("Secrets should not be nil")
				}
				if serviceAccountData.ImagePullSecrets == nil {
					t.Error("ImagePullSecrets should not be nil")
				}
				if serviceAccountData.AutomountServiceAccountToken == nil {
					t.Error("AutomountServiceAccountToken should not be nil")
				}
			}
		})
	}
}

func TestServiceAccountHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewServiceAccountHandler(client)
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

func TestServiceAccountHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewServiceAccountHandler(client)
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

// createTestServiceAccount creates a test ServiceAccount with various configurations
func createTestServiceAccount(name, namespace string) *corev1.ServiceAccount {
	automountTrue := true
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test service account",
			},
			CreationTimestamp: metav1.Now(),
		},
		Secrets: []corev1.ObjectReference{
			{Name: name + "-token-123"},
		},
		AutomountServiceAccountToken: &automountTrue,
	}

	return sa
}

func TestServiceAccountHandler_Collect(t *testing.T) {
	sa1 := createTestServiceAccount("test-sa-1", "default")
	sa2 := createTestServiceAccount("test-sa-2", "kube-system")

	client := fake.NewSimpleClientset(sa1, sa2)
	handler := NewServiceAccountHandler(client)
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

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Type assert to ServiceAccountData for assertions
	entry, ok := entries[0].(types.ServiceAccountData)
	if !ok {
		t.Fatalf("Expected ServiceAccountData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if len(entry.Secrets) == 0 {
		t.Error("Expected secrets to not be empty")
	}
}
