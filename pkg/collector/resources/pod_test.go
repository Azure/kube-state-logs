package resources

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	testutils "github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
)

// createTestPod creates a test pod with various configurations
func createTestPod(name, namespace string, phase corev1.PodPhase) *corev1.Pod {
	now := metav1.Now()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test pod",
			},
			CreationTimestamp: now,
			Generation:        1,
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx:latest",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
			RestartPolicy:      corev1.RestartPolicyAlways,
			ServiceAccountName: "default",
			SchedulerName:      "default-scheduler",
		},
		Status: corev1.PodStatus{
			Phase:     phase,
			HostIP:    "192.168.1.1",
			PodIP:     "10.0.0.1",
			StartTime: &now,
			QOSClass:  corev1.PodQOSBurstable,
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: now,
						},
					},
				},
			},
			Conditions: []corev1.PodCondition{
				{
					Type:               corev1.PodReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             "PodReady",
					Message:            "Pod is ready",
				},
				{
					Type:               corev1.PodInitialized,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             "PodInitialized",
					Message:            "Pod is initialized",
				},
				{
					Type:               corev1.PodScheduled,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             "PodScheduled",
					Message:            "Pod is scheduled",
				},
				{
					Type:               corev1.ContainersReady,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: now,
					Reason:             "ContainersReady",
					Message:            "All containers are ready",
				},
			},
		},
	}

	return pod
}

func TestNewPodHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewPodHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}
}

func TestPodHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewPodHandler(client)
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

func TestPodHandler_Collect(t *testing.T) {
	// Create test pods
	pod1 := createTestPod("test-pod-1", "default", corev1.PodRunning)
	pod2 := createTestPod("test-pod-2", "kube-system", corev1.PodPending)

	// Create fake client with test pods
	client := fake.NewSimpleClientset(pod1, pod2)
	handler := NewPodHandler(client)
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

	// Test collecting all pods
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

	// Type assert to PodData for assertions
	entry, ok := entries[0].(types.PodData)
	if !ok {
		t.Fatalf("Expected PodData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestPodHandler_Collect_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewPodHandler(client)
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

	if len(entries) != 0 {
		t.Fatalf("Expected 0 entries, got %d", len(entries))
	}
}

func TestPodHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test pods in different namespaces
	pod1 := createTestPod("test-pod-1", "default", corev1.PodRunning)
	pod2 := createTestPod("test-pod-2", "kube-system", corev1.PodRunning)
	pod3 := createTestPod("test-pod-3", "monitoring", corev1.PodRunning)

	client := fake.NewSimpleClientset(pod1, pod2, pod3)
	handler := NewPodHandler(client)
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

	// Type assert to PodData for assertions
	entry, ok := entries[0].(types.PodData)
	if !ok {
		t.Fatalf("Expected PodData type, got %T", entries[0])
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
		podData, ok := entry.(types.PodData)
		if !ok {
			t.Fatalf("Expected PodData type, got %T", entry)
		}
		namespaces[podData.Namespace] = true
	}

	if !namespaces["default"] {
		t.Error("Expected to find pod in default namespace")
	}
	if !namespaces["kube-system"] {
		t.Error("Expected to find pod in kube-system namespace")
	}
}

func TestAggregateContainerResources_RegularContainersOnly(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "container1",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
				{
					Name: "container2",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	result := aggregateContainerResources(pod)

	if result.ContainerCount != 2 {
		t.Errorf("Expected ContainerCount to be 2, got %d", result.ContainerCount)
	}
	if result.InitContainerCount != 0 {
		t.Errorf("Expected InitContainerCount to be 0, got %d", result.InitContainerCount)
	}

	// Regular containers should sum: 100m + 50m = 150m
	expectedCPU := int64(150)
	if result.TotalRequestsCPUMillicore == nil {
		t.Error("Expected TotalRequestsCPUMillicore to be non-nil")
	} else if *result.TotalRequestsCPUMillicore != expectedCPU {
		t.Errorf("Expected TotalRequestsCPUMillicore to be %d, got %d", expectedCPU, *result.TotalRequestsCPUMillicore)
	}

	// Regular containers should sum: 128Mi + 64Mi = 192Mi
	expectedMemory := int64(192 * 1024 * 1024)
	if result.TotalRequestsMemoryBytes == nil {
		t.Error("Expected TotalRequestsMemoryBytes to be non-nil")
	} else if *result.TotalRequestsMemoryBytes != expectedMemory {
		t.Errorf("Expected TotalRequestsMemoryBytes to be %d, got %d", expectedMemory, *result.TotalRequestsMemoryBytes)
	}

	// Regular containers limits should sum: 200m + 100m = 300m
	expectedCPULimit := int64(300)
	if result.TotalLimitsCPUMillicore == nil {
		t.Error("Expected TotalLimitsCPUMillicore to be non-nil")
	} else if *result.TotalLimitsCPUMillicore != expectedCPULimit {
		t.Errorf("Expected TotalLimitsCPUMillicore to be %d, got %d", expectedCPULimit, *result.TotalLimitsCPUMillicore)
	}

	// Regular containers limits should sum: 256Mi + 128Mi = 384Mi
	expectedMemoryLimit := int64(384 * 1024 * 1024)
	if result.TotalLimitsMemoryBytes == nil {
		t.Error("Expected TotalLimitsMemoryBytes to be non-nil")
	} else if *result.TotalLimitsMemoryBytes != expectedMemoryLimit {
		t.Errorf("Expected TotalLimitsMemoryBytes to be %d, got %d", expectedMemoryLimit, *result.TotalLimitsMemoryBytes)
	}
}

func TestAggregateContainerResources_InitContainersOnly(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name: "init1",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
				{
					Name: "init2",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("150m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("250m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	result := aggregateContainerResources(pod)

	if result.ContainerCount != 0 {
		t.Errorf("Expected ContainerCount to be 0, got %d", result.ContainerCount)
	}
	if result.InitContainerCount != 2 {
		t.Errorf("Expected InitContainerCount to be 2, got %d", result.InitContainerCount)
	}

	// Init containers should use max: max(100m, 150m) = 150m
	expectedCPU := int64(150)
	if result.TotalRequestsCPUMillicore == nil {
		t.Error("Expected TotalRequestsCPUMillicore to be non-nil")
	} else if *result.TotalRequestsCPUMillicore != expectedCPU {
		t.Errorf("Expected TotalRequestsCPUMillicore to be %d, got %d", expectedCPU, *result.TotalRequestsCPUMillicore)
	}

	// Init containers should use max: max(128Mi, 64Mi) = 128Mi
	expectedMemory := int64(128 * 1024 * 1024)
	if result.TotalRequestsMemoryBytes == nil {
		t.Error("Expected TotalRequestsMemoryBytes to be non-nil")
	} else if *result.TotalRequestsMemoryBytes != expectedMemory {
		t.Errorf("Expected TotalRequestsMemoryBytes to be %d, got %d", expectedMemory, *result.TotalRequestsMemoryBytes)
	}

	// Init containers limits should use max: max(200m, 250m) = 250m
	expectedCPULimit := int64(250)
	if result.TotalLimitsCPUMillicore == nil {
		t.Error("Expected TotalLimitsCPUMillicore to be non-nil")
	} else if *result.TotalLimitsCPUMillicore != expectedCPULimit {
		t.Errorf("Expected TotalLimitsCPUMillicore to be %d, got %d", expectedCPULimit, *result.TotalLimitsCPUMillicore)
	}

	// Init containers limits should use max: max(256Mi, 128Mi) = 256Mi
	expectedMemoryLimit := int64(256 * 1024 * 1024)
	if result.TotalLimitsMemoryBytes == nil {
		t.Error("Expected TotalLimitsMemoryBytes to be non-nil")
	} else if *result.TotalLimitsMemoryBytes != expectedMemoryLimit {
		t.Errorf("Expected TotalLimitsMemoryBytes to be %d, got %d", expectedMemoryLimit, *result.TotalLimitsMemoryBytes)
	}
}

func TestAggregateContainerResources_MixedContainers_InitGreater(t *testing.T) {
	// Test scheduler logic: when init container requires more than sum of regular containers
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name: "init1",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1000m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name: "container1",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
				{
					Name: "container2",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	result := aggregateContainerResources(pod)

	// Should use init container max (500m) instead of regular sum (150m)
	expectedCPU := int64(500)
	if result.TotalRequestsCPUMillicore == nil {
		t.Error("Expected TotalRequestsCPUMillicore to be non-nil")
	} else if *result.TotalRequestsCPUMillicore != expectedCPU {
		t.Errorf("Expected TotalRequestsCPUMillicore to be %d, got %d", expectedCPU, *result.TotalRequestsCPUMillicore)
	}

	// Should use init container max (512Mi) instead of regular sum (192Mi)
	expectedMemory := int64(512 * 1024 * 1024)
	if result.TotalRequestsMemoryBytes == nil {
		t.Error("Expected TotalRequestsMemoryBytes to be non-nil")
	} else if *result.TotalRequestsMemoryBytes != expectedMemory {
		t.Errorf("Expected TotalRequestsMemoryBytes to be %d, got %d", expectedMemory, *result.TotalRequestsMemoryBytes)
	}

	// Should use init container limit (1000m) instead of regular sum (300m)
	expectedCPULimit := int64(1000)
	if result.TotalLimitsCPUMillicore == nil {
		t.Error("Expected TotalLimitsCPUMillicore to be non-nil")
	} else if *result.TotalLimitsCPUMillicore != expectedCPULimit {
		t.Errorf("Expected TotalLimitsCPUMillicore to be %d, got %d", expectedCPULimit, *result.TotalLimitsCPUMillicore)
	}

	// Should use init container limit (1Gi) instead of regular sum (384Mi)
	expectedMemoryLimit := int64(1024 * 1024 * 1024)
	if result.TotalLimitsMemoryBytes == nil {
		t.Error("Expected TotalLimitsMemoryBytes to be non-nil")
	} else if *result.TotalLimitsMemoryBytes != expectedMemoryLimit {
		t.Errorf("Expected TotalLimitsMemoryBytes to be %d, got %d", expectedMemoryLimit, *result.TotalLimitsMemoryBytes)
	}
}

func TestAggregateContainerResources_MixedContainers_RegularGreater(t *testing.T) {
	// Test scheduler logic: when sum of regular containers is greater than init max
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{
				{
					Name: "init1",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("50m"),
							corev1.ResourceMemory: resource.MustParse("64Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
			Containers: []corev1.Container{
				{
					Name: "container1",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("400m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
				{
					Name: "container2",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
					},
				},
			},
		},
	}

	result := aggregateContainerResources(pod)

	// Should use regular sum (300m) instead of init max (50m)
	expectedCPU := int64(300)
	if result.TotalRequestsCPUMillicore == nil {
		t.Error("Expected TotalRequestsCPUMillicore to be non-nil")
	} else if *result.TotalRequestsCPUMillicore != expectedCPU {
		t.Errorf("Expected TotalRequestsCPUMillicore to be %d, got %d", expectedCPU, *result.TotalRequestsCPUMillicore)
	}

	// Should use regular sum (384Mi) instead of init max (64Mi)
	expectedMemory := int64(384 * 1024 * 1024)
	if result.TotalRequestsMemoryBytes == nil {
		t.Error("Expected TotalRequestsMemoryBytes to be non-nil")
	} else if *result.TotalRequestsMemoryBytes != expectedMemory {
		t.Errorf("Expected TotalRequestsMemoryBytes to be %d, got %d", expectedMemory, *result.TotalRequestsMemoryBytes)
	}

	// Should use regular sum (600m) instead of init max (100m)
	expectedCPULimit := int64(600)
	if result.TotalLimitsCPUMillicore == nil {
		t.Error("Expected TotalLimitsCPUMillicore to be non-nil")
	} else if *result.TotalLimitsCPUMillicore != expectedCPULimit {
		t.Errorf("Expected TotalLimitsCPUMillicore to be %d, got %d", expectedCPULimit, *result.TotalLimitsCPUMillicore)
	}

	// Should use regular sum (768Mi) instead of init max (128Mi)
	expectedMemoryLimit := int64(768 * 1024 * 1024)
	if result.TotalLimitsMemoryBytes == nil {
		t.Error("Expected TotalLimitsMemoryBytes to be non-nil")
	} else if *result.TotalLimitsMemoryBytes != expectedMemoryLimit {
		t.Errorf("Expected TotalLimitsMemoryBytes to be %d, got %d", expectedMemoryLimit, *result.TotalLimitsMemoryBytes)
	}
}

func TestAggregateContainerResources_NoResources(t *testing.T) {
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "container1",
					// No resources specified
				},
			},
		},
	}

	result := aggregateContainerResources(pod)

	if result.ContainerCount != 1 {
		t.Errorf("Expected ContainerCount to be 1, got %d", result.ContainerCount)
	}

	// When containers exist but have no resources, fields should be set to 0 (not nil)
	if result.TotalRequestsCPUMillicore == nil {
		t.Error("Expected TotalRequestsCPUMillicore to be non-nil (0)")
	} else if *result.TotalRequestsCPUMillicore != 0 {
		t.Errorf("Expected TotalRequestsCPUMillicore to be 0, got %d", *result.TotalRequestsCPUMillicore)
	}

	if result.TotalRequestsMemoryBytes == nil {
		t.Error("Expected TotalRequestsMemoryBytes to be non-nil (0)")
	} else if *result.TotalRequestsMemoryBytes != 0 {
		t.Errorf("Expected TotalRequestsMemoryBytes to be 0, got %d", *result.TotalRequestsMemoryBytes)
	}

	if result.TotalLimitsCPUMillicore == nil {
		t.Error("Expected TotalLimitsCPUMillicore to be non-nil (0)")
	} else if *result.TotalLimitsCPUMillicore != 0 {
		t.Errorf("Expected TotalLimitsCPUMillicore to be 0, got %d", *result.TotalLimitsCPUMillicore)
	}

	if result.TotalLimitsMemoryBytes == nil {
		t.Error("Expected TotalLimitsMemoryBytes to be non-nil (0)")
	} else if *result.TotalLimitsMemoryBytes != 0 {
		t.Errorf("Expected TotalLimitsMemoryBytes to be 0, got %d", *result.TotalLimitsMemoryBytes)
	}
}

func TestAggregateContainerResources_PartialResources(t *testing.T) {
	// Test with only requests specified (no limits)
	pod := &corev1.Pod{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "container1",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	result := aggregateContainerResources(pod)

	expectedCPU := int64(100)
	if result.TotalRequestsCPUMillicore == nil {
		t.Error("Expected TotalRequestsCPUMillicore to be non-nil")
	} else if *result.TotalRequestsCPUMillicore != expectedCPU {
		t.Errorf("Expected TotalRequestsCPUMillicore to be %d, got %d", expectedCPU, *result.TotalRequestsCPUMillicore)
	}

	expectedMemory := int64(128 * 1024 * 1024)
	if result.TotalRequestsMemoryBytes == nil {
		t.Error("Expected TotalRequestsMemoryBytes to be non-nil")
	} else if *result.TotalRequestsMemoryBytes != expectedMemory {
		t.Errorf("Expected TotalRequestsMemoryBytes to be %d, got %d", expectedMemory, *result.TotalRequestsMemoryBytes)
	}

	// Limits should be 0 since they weren't specified (but not nil, since containers exist)
	if result.TotalLimitsCPUMillicore == nil {
		t.Error("Expected TotalLimitsCPUMillicore to be non-nil (0)")
	} else if *result.TotalLimitsCPUMillicore != 0 {
		t.Errorf("Expected TotalLimitsCPUMillicore to be 0, got %d", *result.TotalLimitsCPUMillicore)
	}

	if result.TotalLimitsMemoryBytes == nil {
		t.Error("Expected TotalLimitsMemoryBytes to be non-nil (0)")
	} else if *result.TotalLimitsMemoryBytes != 0 {
		t.Errorf("Expected TotalLimitsMemoryBytes to be 0, got %d", *result.TotalLimitsMemoryBytes)
	}
}
