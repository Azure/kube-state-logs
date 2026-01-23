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

// createTestStatefulSet creates a test statefulset with various configurations
func createTestStatefulSet(name, namespace string, replicas int32) *appsv1.StatefulSet {
	statefulSet := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test statefulset",
			},
			CreationTimestamp: metav1.Now(),
			Generation:        1,
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: name + "-service",
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
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.RollingUpdateStatefulSetStrategyType,
			},
			PodManagementPolicy: appsv1.OrderedReadyPodManagement,
		},
		Status: appsv1.StatefulSetStatus{
			Replicas:           3,
			ReadyReplicas:      2,
			UpdatedReplicas:    3,
			CurrentRevision:    "test-revision-1",
			UpdateRevision:     "test-revision-2",
			ObservedGeneration: 1,
			Conditions: []appsv1.StatefulSetCondition{
				{
					Type:               "Available",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "StatefulSetAvailable",
					Message:            "StatefulSet is available",
				},
				{
					Type:               "Progressing",
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "StatefulSetProgressing",
					Message:            "StatefulSet is progressing",
				},
			},
		},
	}

	return statefulSet
}

func TestNewStatefulSetHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewStatefulSetHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}
}

func TestStatefulSetHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewStatefulSetHandler(client)
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

func TestStatefulSetHandler_Collect(t *testing.T) {
	// Create test statefulsets
	statefulSet1 := createTestStatefulSet("test-statefulset-1", "default", 3)
	statefulSet2 := createTestStatefulSet("test-statefulset-2", "kube-system", 2)

	// Create fake client with test statefulsets
	client := fake.NewSimpleClientset(statefulSet1, statefulSet2)
	handler := NewStatefulSetHandler(client)
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

	// Test collecting all statefulsets
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

	// Type assert to StatefulSetData for assertions
	entry, ok := entries[0].(types.StatefulSetData)
	if !ok {
		t.Fatalf("Expected StatefulSetData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestStatefulSetHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewStatefulSetHandler(client)
	statefulSet := createTestStatefulSet("test-statefulset", "default", 3)
	entry := handler.createLogEntry(statefulSet)

	if entry.ResourceType != "statefulset" {
		t.Errorf("Expected resource type 'statefulset', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-statefulset" {
		t.Errorf("Expected name 'test-statefulset', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify statefulset-specific fields
	if entry.DesiredReplicas != 3 {
		t.Errorf("Expected desired replicas 3, got %d", entry.DesiredReplicas)
	}

	if entry.CurrentReplicas != 3 {
		t.Errorf("Expected current replicas 3, got %d", entry.CurrentReplicas)
	}

	if entry.ReadyReplicas != 2 {
		t.Errorf("Expected ready replicas 2, got %d", entry.ReadyReplicas)
	}

	if entry.UpdatedReplicas != 3 {
		t.Errorf("Expected updated replicas 3, got %d", entry.UpdatedReplicas)
	}

	if entry.CurrentRevision != "test-revision-1" {
		t.Errorf("Expected current revision 'test-revision-1', got '%s'", entry.CurrentRevision)
	}

	if entry.UpdateRevision != "test-revision-2" {
		t.Errorf("Expected update revision 'test-revision-2', got '%s'", entry.UpdateRevision)
	}

	if entry.ServiceName != "test-statefulset-service" {
		t.Errorf("Expected service name 'test-statefulset-service', got '%s'", entry.ServiceName)
	}

	if entry.PodManagementPolicy != "OrderedReady" {
		t.Errorf("Expected pod management policy 'OrderedReady', got '%s'", entry.PodManagementPolicy)
	}

	if entry.UpdateStrategy != "RollingUpdate" {
		t.Errorf("Expected update strategy 'RollingUpdate', got '%s'", entry.UpdateStrategy)
	}

	if entry.ConditionAvailable == nil || !*entry.ConditionAvailable {
		t.Error("Expected condition available to be true")
	}

	if entry.ConditionProgressing == nil || !*entry.ConditionProgressing {
		t.Error("Expected condition progressing to be true")
	}
}

func TestStatefulSetHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test statefulsets in different namespaces
	statefulSet1 := createTestStatefulSet("test-statefulset-1", "default", 3)
	statefulSet2 := createTestStatefulSet("test-statefulset-2", "kube-system", 2)
	statefulSet3 := createTestStatefulSet("test-statefulset-3", "monitoring", 1)

	client := fake.NewSimpleClientset(statefulSet1, statefulSet2, statefulSet3)
	handler := NewStatefulSetHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}

	factory.Start(nil)
	factory.WaitForCacheSync(nil)

	ctx := context.Background()

	// Test filtering by specific namespace
	entries, err := handler.Collect(ctx, []string{"default"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for default namespace, got %d", len(entries))
	}

	// Type assert to StatefulSetData for assertions
	entry, ok := entries[0].(types.StatefulSetData)
	if !ok {
		t.Fatalf("Expected StatefulSetData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Test filtering by multiple namespaces
	entries, err = handler.Collect(ctx, []string{"default", "kube-system"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries for default and kube-system namespaces, got %d", len(entries))
	}

	// Verify both namespaces are present
	namespaces := make(map[string]bool)
	for _, entry := range entries {
		statefulSetData, ok := entry.(types.StatefulSetData)
		if !ok {
			t.Fatalf("Expected StatefulSetData type, got %T", entry)
		}
		namespaces[statefulSetData.Namespace] = true
	}

	if !namespaces["default"] {
		t.Error("Expected to find statefulset in default namespace")
	}
	if !namespaces["kube-system"] {
		t.Error("Expected to find statefulset in kube-system namespace")
	}
}
