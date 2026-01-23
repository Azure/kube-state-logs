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

func TestNamespaceHandler(t *testing.T) {
	namespace1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "default",
			Labels:            map[string]string{"env": "prod"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Status: corev1.NamespaceStatus{
			Phase: corev1.NamespaceActive,
			Conditions: []corev1.NamespaceCondition{
				{Type: "Active", Status: corev1.ConditionTrue},
			},
		},
	}

	namespace2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Status: corev1.NamespaceStatus{
			Phase: corev1.NamespaceActive,
			Conditions: []corev1.NamespaceCondition{
				{Type: "Active", Status: corev1.ConditionTrue},
			},
		},
	}

	namespaceTerminating := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "terminating-ns",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			CreationTimestamp: metav1.Now(),
			Finalizers:        []string{"kubernetes"},
		},
		Status: corev1.NamespaceStatus{
			Phase: corev1.NamespaceTerminating,
			Conditions: []corev1.NamespaceCondition{
				{Type: "Terminating", Status: corev1.ConditionTrue},
			},
		},
	}

	tests := []struct {
		name             string
		namespaces       []*corev1.Namespace
		filterNamespaces []string
		expectedCount    int
		expectedNames    []string
		expectedFields   map[string]interface{}
	}{
		{
			name:             "collect all namespaces",
			namespaces:       []*corev1.Namespace{namespace1, namespace2},
			filterNamespaces: []string{},
			expectedCount:    2,
			expectedNames:    []string{"default", "kube-system"},
		},
		{
			name:             "collect namespaces with filter",
			namespaces:       []*corev1.Namespace{namespace1, namespace2},
			filterNamespaces: []string{"default"},
			expectedCount:    1,
			expectedNames:    []string{"default"},
		},
		{
			name:             "collect terminating namespace",
			namespaces:       []*corev1.Namespace{namespaceTerminating},
			filterNamespaces: []string{},
			expectedCount:    1,
			expectedNames:    []string{"terminating-ns"},
			expectedFields: map[string]interface{}{
				"phase":                 "Terminating",
				"condition_terminating": true,
				"finalizers":            []string{"kubernetes"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.namespaces))
			for i, ns := range tt.namespaces {
				objects[i] = ns
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewNamespaceHandler(client)
			factory := informers.NewSharedInformerFactory(client, time.Hour)
			err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
			if err != nil {
				t.Fatalf("Failed to setup informer: %v", err)
			}
			factory.Start(context.Background().Done())
			if !cache.WaitForCacheSync(context.Background().Done(), handler.GetInformer().HasSynced) {
				t.Fatal("Failed to sync cache")
			}
			entries, err := handler.Collect(context.Background(), tt.filterNamespaces)
			if err != nil {
				t.Fatalf("Failed to collect metrics: %v", err)
			}
			if len(entries) != tt.expectedCount {
				t.Errorf("Expected %d entries, got %d", tt.expectedCount, len(entries))
			}
			entryNames := make([]string, len(entries))
			for i, entry := range entries {
				namespaceData, ok := entry.(types.NamespaceData)
				if !ok {
					t.Fatalf("Expected NamespaceData type, got %T", entry)
				}
				entryNames[i] = namespaceData.Name
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
					t.Errorf("Expected to find namespace with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				namespaceData, ok := entries[0].(types.NamespaceData)
				if !ok {
					t.Fatalf("Expected NamespaceData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "phase":
						if namespaceData.Phase != expectedValue.(string) {
							t.Errorf("Expected phase %s, got %v", expectedValue, namespaceData.Phase)
						}
					case "condition_terminating":
						expected := expectedValue.(bool)
						if namespaceData.ConditionTerminating == nil {
							if expected {
								t.Errorf("Expected condition_terminating %v, got nil", expectedValue)
							}
						} else if *namespaceData.ConditionTerminating != expected {
							t.Errorf("Expected condition_terminating %v, got %v", expectedValue, *namespaceData.ConditionTerminating)
						}
					case "finalizers":
						expectedFinalizers := expectedValue.([]string)
						if len(namespaceData.Finalizers) != len(expectedFinalizers) {
							t.Errorf("Expected %d finalizers, got %d", len(expectedFinalizers), len(namespaceData.Finalizers))
						} else {
							for i, f := range expectedFinalizers {
								if namespaceData.Finalizers[i] != f {
									t.Errorf("Expected finalizer[%d] %s, got %s", i, f, namespaceData.Finalizers[i])
								}
							}
						}
					}
				}
			}
			for _, entry := range entries {
				namespaceData, ok := entry.(types.NamespaceData)
				if !ok {
					t.Fatalf("Expected NamespaceData type, got %T", entry)
				}
				if namespaceData.ResourceType != "namespace" {
					t.Errorf("Expected resource type 'namespace', got %s", namespaceData.ResourceType)
				}
				if namespaceData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if namespaceData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if namespaceData.Phase == "" {
					t.Error("phase should not be empty")
				}
			}
		})
	}
}

func TestNamespaceHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewNamespaceHandler(client)
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

func TestNamespaceHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewNamespaceHandler(client)
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

// createTestNamespace creates a test Namespace with various configurations
func createTestNamespace(name string, phase corev1.NamespacePhase) *corev1.Namespace {
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test namespace",
			},
			CreationTimestamp: metav1.Now(),
		},
		Status: corev1.NamespaceStatus{
			Phase: phase,
			Conditions: []corev1.NamespaceCondition{
				{Type: "Active", Status: corev1.ConditionTrue},
			},
		},
	}

	return namespace
}

func TestNamespaceHandler_Collect(t *testing.T) {
	namespace1 := createTestNamespace("test-namespace-1", corev1.NamespaceActive)
	namespace2 := createTestNamespace("test-namespace-2", corev1.NamespaceActive)

	client := fake.NewSimpleClientset(namespace1, namespace2)
	handler := NewNamespaceHandler(client)
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

	// Type assert to NamespaceData for assertions
	entry, ok := entries[0].(types.NamespaceData)
	if !ok {
		t.Fatalf("Expected NamespaceData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if entry.Phase == "" {
		t.Error("Expected phase to not be empty")
	}
}

func TestNamespaceHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewNamespaceHandler(client)
	namespace := createTestNamespace("test-namespace", corev1.NamespaceActive)
	entry := handler.createLogEntry(namespace)

	if entry.ResourceType != "namespace" {
		t.Errorf("Expected resource type 'namespace', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-namespace" {
		t.Errorf("Expected name 'test-namespace', got '%s'", entry.Name)
	}

	if entry.Phase != "Active" {
		t.Errorf("Expected phase 'Active', got '%s'", entry.Phase)
	}

	if entry.ConditionActive == nil || !*entry.ConditionActive {
		t.Error("Expected ConditionActive to be true")
	}

	// Verify metadata
	if entry.Labels["app"] != "test-namespace" {
		t.Errorf("Expected label 'app' to be 'test-namespace', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test namespace" {
		t.Errorf("Expected annotation 'description' to be 'test namespace', got '%s'", entry.Annotations["description"])
	}
}

func TestNamespaceHandler_createLogEntry_Terminating(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewNamespaceHandler(client)
	namespace := createTestNamespace("test-namespace", corev1.NamespaceTerminating)
	namespace.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	// Add the Terminating condition
	namespace.Status.Conditions = append(namespace.Status.Conditions, corev1.NamespaceCondition{
		Type:   "Terminating",
		Status: corev1.ConditionTrue,
	})
	entry := handler.createLogEntry(namespace)

	if entry.Phase != "Terminating" {
		t.Errorf("Expected phase 'Terminating', got '%s'", entry.Phase)
	}

	if entry.ConditionTerminating == nil || !*entry.ConditionTerminating {
		t.Error("Expected ConditionTerminating to be true")
	}

	if entry.DeletionTimestamp == nil {
		t.Error("Expected DeletionTimestamp to not be nil")
	}
}

func TestNamespaceHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test namespaces
	namespace1 := createTestNamespace("test-namespace-1", corev1.NamespaceActive)
	namespace2 := createTestNamespace("test-namespace-2", corev1.NamespaceActive)
	namespace3 := createTestNamespace("test-namespace-3", corev1.NamespaceActive)

	client := fake.NewSimpleClientset(namespace1, namespace2, namespace3)
	handler := NewNamespaceHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}

	factory.Start(nil)
	factory.WaitForCacheSync(nil)

	ctx := context.Background()

	// Test namespace filtering
	entries, err := handler.Collect(ctx, []string{"test-namespace-1", "test-namespace-3"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries for filtered namespaces, got %d", len(entries))
	}

	// Verify correct namespaces
	namespaces := make(map[string]bool)
	for _, entry := range entries {
		namespaceData, ok := entry.(types.NamespaceData)
		if !ok {
			t.Fatalf("Expected NamespaceData type, got %T", entry)
		}
		namespaces[namespaceData.Name] = true
	}

	if !namespaces["test-namespace-1"] {
		t.Error("Expected entry from test-namespace-1")
	}

	if !namespaces["test-namespace-3"] {
		t.Error("Expected entry from test-namespace-3")
	}

	if namespaces["test-namespace-2"] {
		t.Error("Did not expect entry from test-namespace-2")
	}
}
