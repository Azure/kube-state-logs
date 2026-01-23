package resources

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
)

func TestPodDisruptionBudgetHandler(t *testing.T) {
	minAvailable := intstrFromInt(2)
	maxUnavailable := intstrFromInt(1)

	pdb1 := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pdb-1",
			Namespace:         "default",
			Labels:            map[string]string{"app": "web"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: minAvailable,
		},
		Status: policyv1.PodDisruptionBudgetStatus{
			CurrentHealthy:     3,
			DesiredHealthy:     2,
			ExpectedPods:       4,
			DisruptionsAllowed: 1,
		},
	}

	pdb2 := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pdb-2",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MaxUnavailable: maxUnavailable,
		},
		Status: policyv1.PodDisruptionBudgetStatus{
			CurrentHealthy:     5,
			DesiredHealthy:     4,
			ExpectedPods:       6,
			DisruptionsAllowed: 0,
		},
	}

	pdbEmpty := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "empty-pdb",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: minAvailable,
		},
		Status: policyv1.PodDisruptionBudgetStatus{
			CurrentHealthy:     1,
			DesiredHealthy:     1,
			ExpectedPods:       1,
			DisruptionsAllowed: 0,
		},
	}

	tests := []struct {
		name          string
		pdbs          []*policyv1.PodDisruptionBudget
		namespaces    []string
		expectedCount int
		expectedNames []string
	}{
		{
			name:          "collect all pod disruption budgets",
			pdbs:          []*policyv1.PodDisruptionBudget{pdb1, pdb2},
			namespaces:    []string{},
			expectedCount: 2,
			expectedNames: []string{"pdb-1", "pdb-2"},
		},
		{
			name:          "collect pod disruption budgets from specific namespace",
			pdbs:          []*policyv1.PodDisruptionBudget{pdb1, pdb2},
			namespaces:    []string{"default"},
			expectedCount: 1,
			expectedNames: []string{"pdb-1"},
		},
		{
			name:          "collect empty pod disruption budget",
			pdbs:          []*policyv1.PodDisruptionBudget{pdbEmpty},
			namespaces:    []string{},
			expectedCount: 1,
			expectedNames: []string{"empty-pdb"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.pdbs))
			for i, pdb := range tt.pdbs {
				objects[i] = pdb
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewPodDisruptionBudgetHandler(client)
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
				podDisruptionBudgetData, ok := entry.(types.PodDisruptionBudgetData)
				if !ok {
					t.Fatalf("Expected PodDisruptionBudgetData type, got %T", entry)
				}
				entryNames[i] = podDisruptionBudgetData.Name
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
					t.Errorf("Expected to find pod disruption budget with name %s", expectedName)
				}
			}

			for _, entry := range entries {
				podDisruptionBudgetData, ok := entry.(types.PodDisruptionBudgetData)
				if !ok {
					t.Fatalf("Expected PodDisruptionBudgetData type, got %T", entry)
				}
				if podDisruptionBudgetData.ResourceType != "poddisruptionbudget" {
					t.Errorf("Expected resource type 'poddisruptionbudget', got %s", podDisruptionBudgetData.ResourceType)
				}
				if podDisruptionBudgetData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if podDisruptionBudgetData.Namespace == "" {
					t.Error("Entry namespace should not be empty")
				}
				if podDisruptionBudgetData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if podDisruptionBudgetData.MinAvailable == 0 && podDisruptionBudgetData.MaxUnavailable == 0 {
					t.Error("Either minAvailable or maxUnavailable should be set")
				}
			}
		})
	}
}

func intstrFromInt(i int32) *intstr.IntOrString {
	val := intstr.FromInt32(i)
	return &val
}

func TestPodDisruptionBudgetHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewPodDisruptionBudgetHandler(client)
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

func TestPodDisruptionBudgetHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewPodDisruptionBudgetHandler(client)
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

// createTestPodDisruptionBudget creates a test PodDisruptionBudget with various configurations
func createTestPodDisruptionBudget(name, namespace string) *policyv1.PodDisruptionBudget {
	minAvailable := intstrFromInt(2)
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test pod disruption budget",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: minAvailable,
		},
		Status: policyv1.PodDisruptionBudgetStatus{
			CurrentHealthy:     3,
			DesiredHealthy:     2,
			ExpectedPods:       4,
			DisruptionsAllowed: 1,
		},
	}

	return pdb
}

func TestPodDisruptionBudgetHandler_Collect(t *testing.T) {
	pdb1 := createTestPodDisruptionBudget("test-pdb-1", "default")
	pdb2 := createTestPodDisruptionBudget("test-pdb-2", "kube-system")

	client := fake.NewSimpleClientset(pdb1, pdb2)
	handler := NewPodDisruptionBudgetHandler(client)
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

	// Type assert to PodDisruptionBudgetData for assertions
	entry, ok := entries[0].(types.PodDisruptionBudgetData)
	if !ok {
		t.Fatalf("Expected PodDisruptionBudgetData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if entry.CurrentHealthy == 0 {
		t.Error("Expected current healthy to not be zero")
	}
}
