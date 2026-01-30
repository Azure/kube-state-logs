// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
)

func TestPriorityClassHandler(t *testing.T) {
	preemptionPolicy := corev1.PreemptionPolicy("PreemptLowerOrEqualPriority")

	pc1 := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "high-priority",
			Labels:            map[string]string{"tier": "critical"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Value:            1000000,
		GlobalDefault:    false,
		Description:      "High priority class for critical workloads",
		PreemptionPolicy: &preemptionPolicy,
	}

	pc2 := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "system-cluster-critical",
			CreationTimestamp: metav1.Now(),
		},
		Value:         2000000000,
		GlobalDefault: true,
		Description:   "System cluster critical priority class",
	}

	pcNilPreemption := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "nil-preemption",
			CreationTimestamp: metav1.Now(),
		},
		Value:         100000,
		GlobalDefault: false,
		Description:   "Priority class with nil preemption policy",
	}

	tests := []struct {
		name            string
		priorityClasses []*schedulingv1.PriorityClass
		expectedCount   int
		expectedNames   []string
		expectedFields  map[string]interface{}
	}{
		{
			name:            "collect all priority classes",
			priorityClasses: []*schedulingv1.PriorityClass{pc1, pc2},
			expectedCount:   2,
			expectedNames:   []string{"high-priority", "system-cluster-critical"},
		},
		{
			name:            "collect priority class with preemption policy",
			priorityClasses: []*schedulingv1.PriorityClass{pc1},
			expectedCount:   1,
			expectedNames:   []string{"high-priority"},
			expectedFields: map[string]interface{}{
				"preemption_policy": "PreemptLowerOrEqualPriority",
			},
		},
		{
			name:            "collect priority class with nil preemption policy",
			priorityClasses: []*schedulingv1.PriorityClass{pcNilPreemption},
			expectedCount:   1,
			expectedNames:   []string{"nil-preemption"},
			expectedFields: map[string]interface{}{
				"preemption_policy": "",
			},
		},
		{
			name:            "collect global default priority class",
			priorityClasses: []*schedulingv1.PriorityClass{pc2},
			expectedCount:   1,
			expectedNames:   []string{"system-cluster-critical"},
			expectedFields: map[string]interface{}{
				"global_default": true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.priorityClasses))
			for i, pc := range tt.priorityClasses {
				objects[i] = pc
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewPriorityClassHandler(client)
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
			if len(entries) != tt.expectedCount {
				t.Errorf("Expected %d entries, got %d", tt.expectedCount, len(entries))
			}
			entryNames := make([]string, len(entries))
			for i, entry := range entries {
				priorityClassData, ok := entry.(types.PriorityClassData)
				if !ok {
					t.Fatalf("Expected PriorityClassData type, got %T", entry)
				}
				entryNames[i] = priorityClassData.Name
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
					t.Errorf("Expected to find priority class with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				priorityClassData, ok := entries[0].(types.PriorityClassData)
				if !ok {
					t.Fatalf("Expected PriorityClassData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "preemption_policy":
						if priorityClassData.PreemptionPolicy != expectedValue.(string) {
							t.Errorf("Expected preemption_policy %s, got %v", expectedValue, priorityClassData.PreemptionPolicy)
						}
					case "global_default":
						if priorityClassData.GlobalDefault != expectedValue.(bool) {
							t.Errorf("Expected global_default %v, got %v", expectedValue, priorityClassData.GlobalDefault)
						}
					}
				}
			}
			for _, entry := range entries {
				priorityClassData, ok := entry.(types.PriorityClassData)
				if !ok {
					t.Fatalf("Expected PriorityClassData type, got %T", entry)
				}
				if priorityClassData.ResourceType != "priorityclass" {
					t.Errorf("Expected resource type 'priorityclass', got %s", priorityClassData.ResourceType)
				}
				if priorityClassData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if priorityClassData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if priorityClassData.Value == 0 {
					t.Error("value should not be zero")
				}
				if priorityClassData.Description == "" {
					t.Error("description should not be empty")
				}
			}
		})
	}
}

func TestPriorityClassHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewPriorityClassHandler(client)
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

func TestPriorityClassHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewPriorityClassHandler(client)
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

// createTestPriorityClass creates a test PriorityClass with various configurations
func createTestPriorityClass(name string) *schedulingv1.PriorityClass {
	pc := &schedulingv1.PriorityClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test priority class",
			},
			CreationTimestamp: metav1.Now(),
		},
		Value:         100000,
		GlobalDefault: false,
		Description:   "Test priority class",
	}

	return pc
}

func TestPriorityClassHandler_Collect(t *testing.T) {
	pc1 := createTestPriorityClass("test-pc-1")
	pc2 := createTestPriorityClass("test-pc-2")

	client := fake.NewSimpleClientset(pc1, pc2)
	handler := NewPriorityClassHandler(client)
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

	// Type assert to PriorityClassData for assertions
	entry, ok := entries[0].(types.PriorityClassData)
	if !ok {
		t.Fatalf("Expected PriorityClassData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if entry.Value == 0 {
		t.Error("Expected value to not be zero")
	}
}
