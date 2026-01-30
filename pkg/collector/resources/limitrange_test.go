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

func TestLimitRangeHandler(t *testing.T) {
	lr1 := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "compute-limits",
			Namespace:         "default",
			Labels:            map[string]string{"env": "prod"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Min: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Max: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
					Default: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
					DefaultRequest: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					MaxLimitRequestRatio: corev1.ResourceList{
						corev1.ResourceCPU: resource.MustParse("4"),
					},
				},
			},
		},
	}

	lr2 := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pod-limits",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypePod,
					Min: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("200m"),
						corev1.ResourceMemory: resource.MustParse("256Mi"),
					},
					Max: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("4"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			},
		},
	}

	lrEmpty := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "empty-limits",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{},
		},
	}

	tests := []struct {
		name          string
		limitRanges   []*corev1.LimitRange
		namespaces    []string
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "collect all limit ranges",
			limitRanges:   []*corev1.LimitRange{lr1, lr2},
			namespaces:    []string{},
			expectedCount: 2,
			expectedNames: []string{"compute-limits", "pod-limits"},
		},
		{
			name:          "collect limit ranges from specific namespace",
			limitRanges:   []*corev1.LimitRange{lr1, lr2},
			namespaces:    []string{"default"},
			expectedCount: 1,
			expectedNames: []string{"compute-limits"},
		},
		{
			name:          "collect limit range with container limits",
			limitRanges:   []*corev1.LimitRange{lr1},
			namespaces:    []string{},
			expectedCount: 1,
			expectedNames: []string{"compute-limits"},
		},
		{
			name:          "collect empty limit range",
			limitRanges:   []*corev1.LimitRange{lrEmpty},
			namespaces:    []string{},
			expectedCount: 1,
			expectedNames: []string{"empty-limits"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.limitRanges))
			for i, lr := range tt.limitRanges {
				objects[i] = lr
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewLimitRangeHandler(client)
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
				limitRangeData, ok := entry.(types.LimitRangeData)
				if !ok {
					t.Fatalf("Expected LimitRangeData type, got %T", entry)
				}
				entryNames[i] = limitRangeData.Name
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
					t.Errorf("Expected to find limit range with name %s", expectedName)
				}
			}

			for _, entry := range entries {
				limitRangeData, ok := entry.(types.LimitRangeData)
				if !ok {
					t.Fatalf("Expected LimitRangeData type, got %T", entry)
				}
				if limitRangeData.ResourceType != "limitrange" {
					t.Errorf("Expected resource type 'limitrange', got %s", limitRangeData.ResourceType)
				}
				if limitRangeData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if limitRangeData.Namespace == "" {
					t.Error("Entry namespace should not be empty")
				}
				if limitRangeData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if limitRangeData.Limits == nil {
					t.Error("limits should not be nil")
				}
			}
		})
	}
}

func TestLimitRangeHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewLimitRangeHandler(client)
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

func TestLimitRangeHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewLimitRangeHandler(client)
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

// createTestLimitRange creates a test LimitRange with various configurations
func createTestLimitRange(name, namespace string) *corev1.LimitRange {
	limitRange := &corev1.LimitRange{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test limit range",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.LimitRangeSpec{
			Limits: []corev1.LimitRangeItem{
				{
					Type: corev1.LimitTypeContainer,
					Min: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Max: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("1Gi"),
					},
				},
			},
		},
	}

	return limitRange
}

func TestLimitRangeHandler_Collect(t *testing.T) {
	limitRange1 := createTestLimitRange("test-limitrange-1", "default")
	limitRange2 := createTestLimitRange("test-limitrange-2", "kube-system")

	client := fake.NewSimpleClientset(limitRange1, limitRange2)
	handler := NewLimitRangeHandler(client)
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

	// Type assert to LimitRangeData for assertions
	entry, ok := entries[0].(types.LimitRangeData)
	if !ok {
		t.Fatalf("Expected LimitRangeData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if entry.Namespace == "" {
		t.Error("Expected namespace to not be empty")
	}
}
