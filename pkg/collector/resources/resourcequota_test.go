// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
)

func TestResourceQuotaHandler(t *testing.T) {
	rq1 := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "compute-quota",
			Namespace:         "default",
			Labels:            map[string]string{"env": "prod"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:   resource.MustParse("10"),
			},
			Scopes: []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopeBestEffort,
				corev1.ResourceQuotaScopeNotBestEffort,
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Used: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
				corev1.ResourcePods:   resource.MustParse("5"),
			},
		},
	}

	rq2 := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "storage-quota",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourcePersistentVolumeClaims: resource.MustParse("5"),
				corev1.ResourceRequestsStorage:        resource.MustParse("100Gi"),
			},
			Scopes: []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopeTerminating,
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Used: corev1.ResourceList{
				corev1.ResourcePersistentVolumeClaims: resource.MustParse("2"),
				corev1.ResourceRequestsStorage:        resource.MustParse("50Gi"),
			},
		},
	}

	rqEmpty := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "empty-quota",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{},
		},
		Status: corev1.ResourceQuotaStatus{
			Used: corev1.ResourceList{},
		},
	}

	tests := []struct {
		name           string
		resourceQuotas []*corev1.ResourceQuota
		namespaces     []string
		expectedCount  int
		expectedNames  []string
		expectedFields map[string]interface{}
	}{
		{
			name:           "collect all resource quotas",
			resourceQuotas: []*corev1.ResourceQuota{rq1, rq2},
			namespaces:     []string{},
			expectedCount:  2,
			expectedNames:  []string{"compute-quota", "storage-quota"},
		},
		{
			name:           "collect resource quotas from specific namespace",
			resourceQuotas: []*corev1.ResourceQuota{rq1, rq2},
			namespaces:     []string{"default"},
			expectedCount:  1,
			expectedNames:  []string{"compute-quota"},
		},
		{
			name:           "collect resource quota with compute resources",
			resourceQuotas: []*corev1.ResourceQuota{rq1},
			namespaces:     []string{},
			expectedCount:  1,
			expectedNames:  []string{"compute-quota"},
			expectedFields: map[string]interface{}{
				"scopes": []string{"BestEffort", "NotBestEffort"},
			},
		},
		{
			name:           "collect empty resource quota",
			resourceQuotas: []*corev1.ResourceQuota{rqEmpty},
			namespaces:     []string{},
			expectedCount:  1,
			expectedNames:  []string{"empty-quota"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.resourceQuotas))
			for i, rq := range tt.resourceQuotas {
				objects[i] = rq
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewResourceQuotaHandler(client)
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
				resourceQuotaData, ok := entry.(types.ResourceQuotaData)
				if !ok {
					t.Fatalf("Expected ResourceQuotaData type, got %T", entry)
				}
				entryNames[i] = resourceQuotaData.Name
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
					t.Errorf("Expected to find resource quota with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				resourceQuotaData, ok := entries[0].(types.ResourceQuotaData)
				if !ok {
					t.Fatalf("Expected ResourceQuotaData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "scopes":
						expectedScopes := expectedValue.([]string)
						if len(resourceQuotaData.Scopes) != len(expectedScopes) {
							t.Errorf("Expected %d scopes, got %d", len(expectedScopes), len(resourceQuotaData.Scopes))
						}
						for i, scope := range expectedScopes {
							if resourceQuotaData.Scopes[i] != scope {
								t.Errorf("Expected scope %s at index %d, got %s", scope, i, resourceQuotaData.Scopes[i])
							}
						}
					}
				}
			}
			for _, entry := range entries {
				resourceQuotaData, ok := entry.(types.ResourceQuotaData)
				if !ok {
					t.Fatalf("Expected ResourceQuotaData type, got %T", entry)
				}
				if resourceQuotaData.ResourceType != "resourcequota" {
					t.Errorf("Expected resource type 'resourcequota', got %s", resourceQuotaData.ResourceType)
				}
				if resourceQuotaData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if resourceQuotaData.Namespace == "" {
					t.Error("Entry namespace should not be empty")
				}
				if resourceQuotaData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if resourceQuotaData.Hard == nil {
					t.Error("Hard limits should not be nil")
				}
				if resourceQuotaData.Used == nil {
					t.Error("Used resources should not be nil")
				}
			}
		})
	}
}

func TestResourceQuotaHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewResourceQuotaHandler(client)
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

func TestResourceQuotaHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewResourceQuotaHandler(client)
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

// createTestResourceQuota creates a test ResourceQuota with various configurations
func createTestResourceQuota(name, namespace string) *corev1.ResourceQuota {
	rq := &corev1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test resource quota",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.ResourceQuotaSpec{
			Hard: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
				corev1.ResourcePods:   resource.MustParse("5"),
			},
			Scopes: []corev1.ResourceQuotaScope{
				corev1.ResourceQuotaScopeBestEffort,
			},
		},
		Status: corev1.ResourceQuotaStatus{
			Used: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
				corev1.ResourcePods:   resource.MustParse("2"),
			},
		},
	}

	return rq
}

func TestResourceQuotaHandler_Collect(t *testing.T) {
	rq1 := createTestResourceQuota("test-rq-1", "default")
	rq2 := createTestResourceQuota("test-rq-2", "kube-system")

	client := fake.NewSimpleClientset(rq1, rq2)
	handler := NewResourceQuotaHandler(client)
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

	// Type assert to ResourceQuotaData for assertions
	entry, ok := entries[0].(types.ResourceQuotaData)
	if !ok {
		t.Fatalf("Expected ResourceQuotaData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if len(entry.Scopes) == 0 {
		t.Error("Expected scopes to not be empty")
	}
}
