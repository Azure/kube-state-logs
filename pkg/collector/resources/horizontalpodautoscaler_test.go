// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	testutils "github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// createTestHorizontalPodAutoscaler creates a test HPA with various configurations
func createTestHorizontalPodAutoscaler(name, namespace string, minReplicas, maxReplicas int32) *autoscalingv2.HorizontalPodAutoscaler {
	hpa := &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test hpa",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{
				Kind:       "Deployment",
				Name:       "test-deployment",
				APIVersion: "apps/v1",
			},
			MinReplicas: &minReplicas,
			MaxReplicas: maxReplicas,
			Metrics: []autoscalingv2.MetricSpec{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceCPU,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &[]int32{80}[0],
						},
					},
				},
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricSource{
						Name: corev1.ResourceMemory,
						Target: autoscalingv2.MetricTarget{
							Type:               autoscalingv2.UtilizationMetricType,
							AverageUtilization: &[]int32{70}[0],
						},
					},
				},
			},
		},
		Status: autoscalingv2.HorizontalPodAutoscalerStatus{
			CurrentReplicas: 3,
			DesiredReplicas: 4,
			CurrentMetrics: []autoscalingv2.MetricStatus{
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricStatus{
						Name: corev1.ResourceCPU,
						Current: autoscalingv2.MetricValueStatus{
							AverageUtilization: &[]int32{85}[0],
						},
					},
				},
				{
					Type: autoscalingv2.ResourceMetricSourceType,
					Resource: &autoscalingv2.ResourceMetricStatus{
						Name: corev1.ResourceMemory,
						Current: autoscalingv2.MetricValueStatus{
							AverageUtilization: &[]int32{75}[0],
						},
					},
				},
			},
			Conditions: []autoscalingv2.HorizontalPodAutoscalerCondition{
				{
					Type:               autoscalingv2.AbleToScale,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "SucceededRescale",
					Message:            "the HPA controller was able to update the target scale to 4",
				},
				{
					Type:               autoscalingv2.ScalingActive,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "ValidMetricFound",
					Message:            "the HPA was able to successfully calculate a replica count from cpu resource utilization (percentage of request)",
				},
			},
		},
	}

	return hpa
}

func TestNewHorizontalPodAutoscalerHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewHorizontalPodAutoscalerHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestHorizontalPodAutoscalerHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewHorizontalPodAutoscalerHandler(client)
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

func TestHorizontalPodAutoscalerHandler_Collect(t *testing.T) {
	// Create test HPAs
	hpa1 := createTestHorizontalPodAutoscaler("test-hpa-1", "default", 2, 10)
	hpa2 := createTestHorizontalPodAutoscaler("test-hpa-2", "kube-system", 1, 5)

	// Create fake client with test HPAs
	client := fake.NewSimpleClientset(hpa1, hpa2)
	handler := NewHorizontalPodAutoscalerHandler(client)
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

	// Test collecting all HPAs
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

	// Type assert to HorizontalPodAutoscalerData for assertions
	entry, ok := entries[0].(types.HorizontalPodAutoscalerData)
	if !ok {
		t.Fatalf("Expected HorizontalPodAutoscalerData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestHorizontalPodAutoscalerHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewHorizontalPodAutoscalerHandler(client)
	hpa := createTestHorizontalPodAutoscaler("test-hpa", "default", 2, 10)
	entry := handler.createLogEntry(hpa)

	if entry.ResourceType != "horizontalpodautoscaler" {
		t.Errorf("Expected resource type 'horizontalpodautoscaler', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-hpa" {
		t.Errorf("Expected name 'test-hpa', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify HPA-specific fields
	if *entry.MinReplicas != 2 {
		t.Errorf("Expected min replicas 2, got %d", *entry.MinReplicas)
	}

	if entry.MaxReplicas != 10 {
		t.Errorf("Expected max replicas 10, got %d", entry.MaxReplicas)
	}

	if entry.CurrentReplicas != 3 {
		t.Errorf("Expected current replicas 3, got %d", entry.CurrentReplicas)
	}

	if entry.DesiredReplicas != 4 {
		t.Errorf("Expected desired replicas 4, got %d", entry.DesiredReplicas)
	}

	if *entry.TargetCPUUtilizationPercentage != 80 {
		t.Errorf("Expected target CPU utilization 80, got %d", *entry.TargetCPUUtilizationPercentage)
	}

	if *entry.TargetMemoryUtilizationPercentage != 70 {
		t.Errorf("Expected target memory utilization 70, got %d", *entry.TargetMemoryUtilizationPercentage)
	}

	if *entry.CurrentCPUUtilizationPercentage != 85 {
		t.Errorf("Expected current CPU utilization 85, got %d", *entry.CurrentCPUUtilizationPercentage)
	}

	if *entry.CurrentMemoryUtilizationPercentage != 75 {
		t.Errorf("Expected current memory utilization 75, got %d", *entry.CurrentMemoryUtilizationPercentage)
	}

	if entry.ScaleTargetRef != "Deployment/test-deployment" {
		t.Errorf("Expected scale target ref 'Deployment/test-deployment', got '%s'", entry.ScaleTargetRef)
	}

	if entry.ScaleTargetKind != "Deployment" {
		t.Errorf("Expected scale target kind 'Deployment', got '%s'", entry.ScaleTargetKind)
	}

	// Verify conditions
	if entry.ConditionAbleToScale == nil || !*entry.ConditionAbleToScale {
		t.Error("Expected ConditionAbleToScale to be true")
	}

	if entry.ConditionScalingActive == nil || !*entry.ConditionScalingActive {
		t.Error("Expected ConditionScalingActive to be true")
	}

	if entry.ConditionScalingLimited != nil && *entry.ConditionScalingLimited {
		t.Error("Expected ConditionScalingLimited to be false or nil")
	}

	// Verify metadata
	if entry.Labels["app"] != "test-hpa" {
		t.Errorf("Expected label 'app' to be 'test-hpa', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test hpa" {
		t.Errorf("Expected annotation 'description' to be 'test hpa', got '%s'", entry.Annotations["description"])
	}
}

func TestHorizontalPodAutoscalerHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test HPAs in different namespaces
	hpa1 := createTestHorizontalPodAutoscaler("test-hpa-1", "default", 2, 10)
	hpa2 := createTestHorizontalPodAutoscaler("test-hpa-2", "kube-system", 1, 5)
	hpa3 := createTestHorizontalPodAutoscaler("test-hpa-3", "monitoring", 3, 15)

	client := fake.NewSimpleClientset(hpa1, hpa2, hpa3)
	handler := NewHorizontalPodAutoscalerHandler(client)
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
		entryData, ok := entry.(types.HorizontalPodAutoscalerData)
		if !ok {
			t.Fatalf("Expected HorizontalPodAutoscalerData type, got %T", entry)
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

func TestHorizontalPodAutoscalerHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewHorizontalPodAutoscalerHandler(client)
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

func TestHorizontalPodAutoscalerHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewHorizontalPodAutoscalerHandler(client)
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
