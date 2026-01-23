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
	"github.com/azure/kube-state-logs/pkg/utils"
)

// createTestContainer creates a test container with various configurations
func createTestContainer(name, image string, ready bool) *corev1.Container {
	container := &corev1.Container{
		Name:  name,
		Image: image,
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
	}

	return container
}

// createTestPodWithContainers creates a test pod with containers
func createTestPodWithContainers(name, namespace string, containers []corev1.Container) *corev1.Pod {
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
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.PodSpec{
			Containers: containers,
		},
		Status: corev1.PodStatus{
			ContainerStatuses: make([]corev1.ContainerStatus, len(containers)),
		},
	}

	// Populate container statuses
	for i, container := range containers {
		pod.Status.ContainerStatuses[i] = corev1.ContainerStatus{
			Name:         container.Name,
			Image:        container.Image,
			ImageID:      "docker://sha256:test",
			ContainerID:  "docker://sha256:test",
			Ready:        true,
			RestartCount: 0,
			State: corev1.ContainerState{
				Running: &corev1.ContainerStateRunning{
					StartedAt: metav1.Now(),
				},
			},
		}
	}

	return pod
}

func TestNewContainerHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewContainerHandler(client, nil, []string{})

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestContainerHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewContainerHandler(client, nil, []string{})
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

func TestContainerHandler_Collect(t *testing.T) {
	// Create test containers
	container1 := createTestContainer("app", "nginx:latest", true)
	container2 := createTestContainer("sidecar", "busybox:latest", true)

	// Create test pods with containers
	pod1 := createTestPodWithContainers("test-pod-1", "default", []corev1.Container{*container1})
	pod2 := createTestPodWithContainers("test-pod-2", "kube-system", []corev1.Container{*container2})

	// Create fake client with test pods
	client := fake.NewSimpleClientset(pod1, pod2)
	handler := NewContainerHandler(client, nil, []string{})
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

	// Test collecting all containers
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

	// Type assert to ContainerData for assertions
	entry, ok := entries[0].(types.ContainerData)
	if !ok {
		t.Fatalf("Expected ContainerData type, got %T", entries[0])
	}

	if entry.PodName != "test-pod-1" {
		t.Errorf("Expected pod name 'test-pod-1', got '%s'", entry.PodName)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestContainerHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewContainerHandler(client, nil, []string{})
	container := createTestContainer("app", "nginx:latest", true)
	pod := createTestPodWithContainers("test-pod", "default", []corev1.Container{*container})
	entry := handler.createLogEntry(pod, &pod.Status.ContainerStatuses[0], false)

	if entry.Name != "app" {
		t.Errorf("Expected name 'app', got '%s'", entry.Name)
	}

	if entry.Image != "nginx:latest" {
		t.Errorf("Expected image 'nginx:latest', got '%s'", entry.Image)
	}

	if entry.PodName != "test-pod" {
		t.Errorf("Expected pod name 'test-pod', got '%s'", entry.PodName)
	}

	// Verify container-specific fields
	if entry.Ready == nil || !*entry.Ready {
		t.Error("Expected container to be ready")
	}

	if entry.RestartCount != 0 {
		t.Errorf("Expected restart count 0, got %d", entry.RestartCount)
	}

	if entry.State != "running" {
		t.Errorf("Expected state 'running', got '%s'", entry.State)
	}

	if entry.StateRunning == nil || !*entry.StateRunning {
		t.Error("Expected StateRunning to be true")
	}

	if entry.StateWaiting != nil && *entry.StateWaiting {
		t.Error("Expected StateWaiting to be false")
	}

	if entry.StateTerminated != nil && *entry.StateTerminated {
		t.Error("Expected StateTerminated to be false")
	}

	// Verify resource requests - CPU and memory should NOT be in ResourceRequests anymore
	if _, hasCPU := entry.ResourceRequests["cpu"]; hasCPU {
		t.Error("Expected CPU request to be excluded from ResourceRequests map")
	}

	if _, hasMemory := entry.ResourceRequests["memory"]; hasMemory {
		t.Error("Expected memory request to be excluded from ResourceRequests map")
	}

	// Verify resource limits - CPU and memory should NOT be in ResourceLimits anymore
	if _, hasCPU := entry.ResourceLimits["cpu"]; hasCPU {
		t.Error("Expected CPU limit to be excluded from ResourceLimits map")
	}

	if _, hasMemory := entry.ResourceLimits["memory"]; hasMemory {
		t.Error("Expected memory limit to be excluded from ResourceLimits map")
	}

	// Verify specific CPU and memory fields
	if entry.RequestsCPUMillicore == nil || *entry.RequestsCPUMillicore != 100 {
		t.Errorf("Expected CPU request 100 millicores, got %v", entry.RequestsCPUMillicore)
	}

	if entry.RequestsMemoryBytes == nil || *entry.RequestsMemoryBytes != 134217728 { // 128Mi in bytes
		t.Errorf("Expected memory request 134217728 bytes (128Mi), got %v", entry.RequestsMemoryBytes)
	}

	if entry.LimitsCPUMillicore == nil || *entry.LimitsCPUMillicore != 200 {
		t.Errorf("Expected CPU limit 200 millicores, got %v", entry.LimitsCPUMillicore)
	}

	if entry.LimitsMemoryBytes == nil || *entry.LimitsMemoryBytes != 268435456 { // 256Mi in bytes
		t.Errorf("Expected memory limit 268435456 bytes (256Mi), got %v", entry.LimitsMemoryBytes)
	}

	// Verify ContainerID is extracted from the container status
	if entry.ContainerID != "docker://sha256:test" {
		t.Errorf("Expected ContainerID 'docker://sha256:test', got '%s'", entry.ContainerID)
	}
}

func TestContainerHandler_createLogEntry_Waiting(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewContainerHandler(client, nil, []string{})
	container := createTestContainer("app", "nginx:latest", false)
	pod := createTestPodWithContainers("test-pod", "default", []corev1.Container{*container})

	// Set container status to waiting
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Waiting: &corev1.ContainerStateWaiting{
			Reason:  "ImagePullBackOff",
			Message: "Back-off pulling image",
		},
	}
	pod.Status.ContainerStatuses[0].Ready = false

	entry := handler.createLogEntry(pod, &pod.Status.ContainerStatuses[0], false)

	if entry.State != "waiting" {
		t.Errorf("Expected state 'waiting', got '%s'", entry.State)
	}

	if entry.StateWaiting == nil || !*entry.StateWaiting {
		t.Error("Expected StateWaiting to be true")
	}

	if entry.WaitingReason != "ImagePullBackOff" {
		t.Errorf("Expected waiting reason 'ImagePullBackOff', got '%s'", entry.WaitingReason)
	}

	if entry.WaitingMessage != "Back-off pulling image" {
		t.Errorf("Expected waiting message 'Back-off pulling image', got '%s'", entry.WaitingMessage)
	}

	if entry.Ready != nil && *entry.Ready {
		t.Error("Expected container to not be ready")
	}
}

func TestContainerHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test containers
	container1 := createTestContainer("app", "nginx:latest", true)
	container2 := createTestContainer("sidecar", "busybox:latest", true)

	// Create test pods with containers
	pod1 := createTestPodWithContainers("test-pod-1", "default", []corev1.Container{*container1})
	pod2 := createTestPodWithContainers("test-pod-2", "kube-system", []corev1.Container{*container2})

	// Create fake client with test pods
	client := fake.NewSimpleClientset(pod1, pod2)
	handler := NewContainerHandler(client, nil, []string{})
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

	// Test collecting from specific namespace
	ctx := context.Background()
	entries, err := handler.Collect(ctx, []string{"default"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for default namespace, got %d", len(entries))
	}

	// Type assert to ContainerData for assertions
	entry, ok := entries[0].(types.ContainerData)
	if !ok {
		t.Fatalf("Expected ContainerData type, got %T", entries[0])
	}

	if entry.PodName != "test-pod-1" {
		t.Errorf("Expected pod name 'test-pod-1', got '%s'", entry.PodName)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestContainerHandler_InitContainerResources(t *testing.T) {
	// Create test init container
	initContainer := createTestContainer("init", "busybox:latest", true)
	regularContainer := createTestContainer("app", "nginx:latest", true)

	// Create test pod with both init and regular containers
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{*initContainer},
			Containers:     []corev1.Container{*regularContainer},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "init",
					Image:        "busybox:latest",
					ImageID:      "docker://sha256:init",
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Image:        "nginx:latest",
					ImageID:      "docker://sha256:app",
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset()
	handler := NewContainerHandler(client, nil, []string{})

	// Test init container resource extraction
	initEntry := handler.createLogEntry(pod, &pod.Status.InitContainerStatuses[0], true)
	if initEntry.Name != "init" {
		t.Errorf("Expected init container name 'init', got '%s'", initEntry.Name)
	}
	// ResourceRequests and ResourceLimits should be empty since only CPU and memory are set
	// and they are now in specific fields
	if initEntry.ResourceRequests != nil && len(initEntry.ResourceRequests) > 0 {
		t.Error("Expected init container ResourceRequests to be empty (CPU and memory moved to specific fields)")
	}
	if initEntry.ResourceLimits != nil && len(initEntry.ResourceLimits) > 0 {
		t.Error("Expected init container ResourceLimits to be empty (CPU and memory moved to specific fields)")
	}
	// Check specific CPU and memory fields
	if initEntry.RequestsCPUMillicore == nil || *initEntry.RequestsCPUMillicore != 100 {
		t.Errorf("Expected init container CPU request 100 millicores, got %v", initEntry.RequestsCPUMillicore)
	}
	if initEntry.RequestsMemoryBytes == nil || *initEntry.RequestsMemoryBytes != 134217728 {
		t.Errorf("Expected init container memory request 134217728 bytes, got %v", initEntry.RequestsMemoryBytes)
	}

	// Test regular container resource extraction
	regularEntry := handler.createLogEntry(pod, &pod.Status.ContainerStatuses[0], false)
	if regularEntry.Name != "app" {
		t.Errorf("Expected regular container name 'app', got '%s'", regularEntry.Name)
	}
	if regularEntry.ResourceRequests != nil && len(regularEntry.ResourceRequests) > 0 {
		t.Error("Expected regular container ResourceRequests to be empty (CPU and memory moved to specific fields)")
	}
	if regularEntry.ResourceLimits != nil && len(regularEntry.ResourceLimits) > 0 {
		t.Error("Expected regular container ResourceLimits to be empty (CPU and memory moved to specific fields)")
	}
	// Check specific CPU and memory fields
	if regularEntry.RequestsCPUMillicore == nil || *regularEntry.RequestsCPUMillicore != 100 {
		t.Errorf("Expected regular container CPU request 100 millicores, got %v", regularEntry.RequestsCPUMillicore)
	}
	if regularEntry.RequestsMemoryBytes == nil || *regularEntry.RequestsMemoryBytes != 134217728 {
		t.Errorf("Expected regular container memory request 134217728 bytes, got %v", regularEntry.RequestsMemoryBytes)
	}
}

func TestContainerHandler_ExitDetection(t *testing.T) {
	// Create a pod with a running container
	runningContainer := createTestContainer("app", "nginx:latest", true)
	pod := createTestPodWithContainers("test-pod", "default", []corev1.Container{*runningContainer})

	client := fake.NewSimpleClientset(pod)
	handler := NewContainerHandler(client, nil, []string{})

	// First collection - should log running container
	entries, err := handler.processPods(context.Background(), []any{pod}, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for running container, got %d", len(entries))
	}

	// Verify it's a running container
	entry, ok := entries[0].(types.ContainerData)
	if !ok {
		t.Fatalf("Expected ContainerData type, got %T", entries[0])
	}

	if entry.State != "running" {
		t.Errorf("Expected state 'running', got '%s'", entry.State)
	}

	// Now simulate container termination
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{
			ExitCode: 0,
			Reason:   "Completed",
			FinishedAt: metav1.Time{
				Time: time.Now(),
			},
		},
	}

	// Second collection - should log terminated container
	entries, err = handler.processPods(context.Background(), []any{pod}, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for terminated container, got %d", len(entries))
	}

	// Verify it's a terminated container
	entry, ok = entries[0].(types.ContainerData)
	if !ok {
		t.Fatalf("Expected ContainerData type, got %T", entries[0])
	}

	if entry.State != "terminated" {
		t.Errorf("Expected state 'terminated', got '%s'", entry.State)
	}

	if entry.ExitCode != 0 {
		t.Errorf("Expected exit code 0, got %d", entry.ExitCode)
	}

	if entry.Reason != "Completed" {
		t.Errorf("Expected reason 'Completed', got '%s'", entry.Reason)
	}
}

func TestContainerHandler_FirstTimeTerminatedContainer(t *testing.T) {
	// Create a pod with a terminated container (not previously seen as running)
	terminatedContainer := createTestContainer("app", "nginx:latest", false)
	pod := createTestPodWithContainers("test-pod", "default", []corev1.Container{*terminatedContainer})

	// Set container to terminated state
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{
			ExitCode: 1,
			Reason:   "Error",
			FinishedAt: metav1.Time{
				Time: time.Now(),
			},
		},
	}

	client := fake.NewSimpleClientset(pod)
	handler := NewContainerHandler(client, nil, []string{})

	// Collection - should log terminated container since we haven't seen it before
	entries, err := handler.processPods(context.Background(), []any{pod}, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for first-time terminated container, got %d", len(entries))
	}

	// Verify it's a terminated container
	entry, ok := entries[0].(types.ContainerData)
	if !ok {
		t.Fatalf("Expected ContainerData type, got %T", entries[0])
	}

	if entry.State != "terminated" {
		t.Errorf("Expected state 'terminated', got '%s'", entry.State)
	}

	if entry.ExitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", entry.ExitCode)
	}

	if entry.Reason != "Error" {
		t.Errorf("Expected reason 'Error', got '%s'", entry.Reason)
	}
}

func TestContainerHandler_NoDuplicateExitLogs(t *testing.T) {
	// Create a pod with a terminated container
	terminatedContainer := createTestContainer("app", "nginx:latest", false)
	pod := createTestPodWithContainers("test-pod", "default", []corev1.Container{*terminatedContainer})

	// Set container to terminated state
	pod.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{
			ExitCode: 1,
			Reason:   "Error",
			FinishedAt: metav1.Time{
				Time: time.Now(),
			},
		},
	}

	client := fake.NewSimpleClientset(pod)
	handler := NewContainerHandler(client, nil, []string{})

	// First collection - should log terminated container
	entries, err := handler.processPods(context.Background(), []any{pod}, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for first-time terminated container, got %d", len(entries))
	}

	// Second collection - should NOT log the same terminated container again
	entries, err = handler.processPods(context.Background(), []any{pod}, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("Expected 0 entries for already-logged terminated container, got %d", len(entries))
	}
}

func TestContainerHandler_InitContainerExitDetection(t *testing.T) {
	// Create a pod with a running init container
	initContainer := createTestContainer("init", "busybox:latest", true)
	regularContainer := createTestContainer("app", "nginx:latest", true)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{*initContainer},
			Containers:     []corev1.Container{*regularContainer},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "init",
					Image:        "busybox:latest",
					ImageID:      "docker://sha256:init",
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Image:        "nginx:latest",
					ImageID:      "docker://sha256:app",
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
			},
		},
	}

	client := fake.NewSimpleClientset(pod)
	handler := NewContainerHandler(client, nil, []string{})

	// First collection - should log running containers
	entries, err := handler.processPods(context.Background(), []any{pod}, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries for running containers, got %d", len(entries))
	}

	// Now simulate init container termination
	pod.Status.InitContainerStatuses[0].State = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{
			ExitCode: 0,
			Reason:   "Completed",
			FinishedAt: metav1.Time{
				Time: time.Now(),
			},
		},
	}

	// Second collection - should log running regular container + terminated init container
	entries, err = handler.processPods(context.Background(), []any{pod}, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries (running regular + terminated init), got %d", len(entries))
	}

	// Find the terminated init container entry
	var terminatedEntry types.ContainerData
	for _, e := range entries {
		if entry, ok := e.(types.ContainerData); ok {
			if entry.ResourceType == "init_container" && entry.State == "terminated" {
				terminatedEntry = entry
				break
			}
		}
	}

	if terminatedEntry.Name == "" {
		t.Fatal("Expected to find terminated init container entry")
	}

	if terminatedEntry.ResourceType != "init_container" {
		t.Errorf("Expected resource type 'init_container', got '%s'", terminatedEntry.ResourceType)
	}

	if terminatedEntry.State != "terminated" {
		t.Errorf("Expected state 'terminated', got '%s'", terminatedEntry.State)
	}
}

func TestContainerHandler_createLogEntry_NilContainer(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "nginx:latest",
				},
			},
		},
	}

	handler := NewContainerHandler(nil, nil, []string{})

	// Test regular container
	data := handler.createLogEntry(pod, nil, false)

	// Verify all required fields are set
	if data.ResourceType != "container" {
		t.Errorf("Expected ResourceType 'container', got '%s'", data.ResourceType)
	}

	if data.Timestamp.IsZero() {
		t.Error("Expected Timestamp to be set")
	}

	if data.PodName != "test-pod" {
		t.Errorf("Expected PodName 'test-pod', got '%s'", data.PodName)
	}

	if data.Namespace != "default" {
		t.Errorf("Expected Namespace 'default', got '%s'", data.Namespace)
	}

	if data.State != "unknown" {
		t.Errorf("Expected State 'unknown', got '%s'", data.State)
	}

	// Test init container
	data = handler.createLogEntry(pod, nil, true)

	if data.ResourceType != "init_container" {
		t.Errorf("Expected ResourceType 'init_container', got '%s'", data.ResourceType)
	}
}

func TestContainerHandler_TerminatedContainerTimeFiltering(t *testing.T) {
	// Create a pod with a terminated container (old finish time)
	terminatedContainer := createTestContainer("app", "nginx:latest", false)
	pod1 := createTestPodWithContainers("test-pod-old", "default", []corev1.Container{*terminatedContainer})

	// Set container to terminated state with old finish time (2 hours ago)
	oldFinishTime := time.Now().Add(-2 * time.Hour)
	pod1.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{
			ExitCode: 1,
			Reason:   "Error",
			FinishedAt: metav1.Time{
				Time: oldFinishTime,
			},
		},
	}

	// Create another pod with a terminated container (recent finish time)
	pod2 := createTestPodWithContainers("test-pod-recent", "default", []corev1.Container{*terminatedContainer})

	// Set container to terminated state with recent finish time (30 minutes ago)
	recentFinishTime := time.Now().Add(-30 * time.Minute)
	pod2.Status.ContainerStatuses[0].State = corev1.ContainerState{
		Terminated: &corev1.ContainerStateTerminated{
			ExitCode: 1,
			Reason:   "Error",
			FinishedAt: metav1.Time{
				Time: recentFinishTime,
			},
		},
	}

	client := fake.NewSimpleClientset(pod1, pod2)
	handler := NewContainerHandler(client, nil, []string{})

	// Collection - should only log the recent terminated container
	entries, err := handler.processPods(context.Background(), []any{pod1, pod2}, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for recent terminated container, got %d", len(entries))
	}

	// Verify it's a terminated container
	entry, ok := entries[0].(types.ContainerData)
	if !ok {
		t.Fatalf("Expected ContainerData type, got %T", entries[0])
	}

	if entry.State != "terminated" {
		t.Errorf("Expected state 'terminated', got '%s'", entry.State)
	}

	if entry.ExitCode != 1 {
		t.Errorf("Expected exit code 1, got %d", entry.ExitCode)
	}

	if entry.Reason != "Error" {
		t.Errorf("Expected reason 'Error', got '%s'", entry.Reason)
	}

	if entry.PodName != "test-pod-recent" {
		t.Errorf("Expected pod name 'test-pod-recent', got '%s'", entry.PodName)
	}
}

func TestContainerHandler_ResourceSeparation(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewContainerHandler(client, nil, []string{})

	// Create a container with CPU, memory, and other resources
	containerWithMixedResources := &corev1.Container{
		Name:  "test",
		Image: "test:latest",
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("500m"),
				corev1.ResourceMemory:           resource.MustParse("512Mi"),
				corev1.ResourceStorage:          resource.MustParse("1Gi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("1000m"),
				corev1.ResourceMemory:           resource.MustParse("1Gi"),
				corev1.ResourceStorage:          resource.MustParse("2Gi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("4Gi"),
			},
		},
	}

	pod := createTestPodWithContainers("test-pod", "default", []corev1.Container{*containerWithMixedResources})
	pod.Status.ContainerStatuses[0] = corev1.ContainerStatus{
		Name:         "test",
		Image:        "test:latest",
		ImageID:      "docker://sha256:test",
		Ready:        true,
		RestartCount: 0,
		State: corev1.ContainerState{
			Running: &corev1.ContainerStateRunning{
				StartedAt: metav1.Now(),
			},
		},
	}

	entry := handler.createLogEntry(pod, &pod.Status.ContainerStatuses[0], false)

	// Verify that CPU and memory are in specific fields
	if entry.RequestsCPUMillicore == nil || *entry.RequestsCPUMillicore != 500 {
		t.Errorf("Expected CPU request 500 millicores, got %v", entry.RequestsCPUMillicore)
	}
	if entry.RequestsMemoryBytes == nil || *entry.RequestsMemoryBytes != 536870912 { // 512Mi in bytes
		t.Errorf("Expected memory request 536870912 bytes (512Mi), got %v", entry.RequestsMemoryBytes)
	}
	if entry.LimitsCPUMillicore == nil || *entry.LimitsCPUMillicore != 1000 {
		t.Errorf("Expected CPU limit 1000 millicores, got %v", entry.LimitsCPUMillicore)
	}
	if entry.LimitsMemoryBytes == nil || *entry.LimitsMemoryBytes != 1073741824 { // 1Gi in bytes
		t.Errorf("Expected memory limit 1073741824 bytes (1Gi), got %v", entry.LimitsMemoryBytes)
	}

	// Verify that CPU and memory are NOT in the ResourceRequests/ResourceLimits maps
	if _, hasCPU := entry.ResourceRequests["cpu"]; hasCPU {
		t.Error("Expected CPU to be excluded from ResourceRequests map")
	}
	if _, hasMemory := entry.ResourceRequests["memory"]; hasMemory {
		t.Error("Expected memory to be excluded from ResourceRequests map")
	}
	if _, hasCPU := entry.ResourceLimits["cpu"]; hasCPU {
		t.Error("Expected CPU to be excluded from ResourceLimits map")
	}
	if _, hasMemory := entry.ResourceLimits["memory"]; hasMemory {
		t.Error("Expected memory to be excluded from ResourceLimits map")
	}

	// Verify that other resources ARE in the ResourceRequests/ResourceLimits maps
	if entry.ResourceRequests["storage"] != "1Gi" {
		t.Errorf("Expected storage request '1Gi', got '%s'", entry.ResourceRequests["storage"])
	}
	if entry.ResourceRequests["ephemeral-storage"] != "2Gi" {
		t.Errorf("Expected ephemeral-storage request '2Gi', got '%s'", entry.ResourceRequests["ephemeral-storage"])
	}
	if entry.ResourceLimits["storage"] != "2Gi" {
		t.Errorf("Expected storage limit '2Gi', got '%s'", entry.ResourceLimits["storage"])
	}
	if entry.ResourceLimits["ephemeral-storage"] != "4Gi" {
		t.Errorf("Expected ephemeral-storage limit '4Gi', got '%s'", entry.ResourceLimits["ephemeral-storage"])
	}
}

func TestContainerHandler_ContainerID(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewContainerHandler(client, nil, []string{})

	// Test with different container ID formats
	testCases := []struct {
		name        string
		containerID string
		expected    string
	}{
		{
			name:        "Docker container ID",
			containerID: "docker://1234567890abcdef",
			expected:    "docker://1234567890abcdef",
		},
		{
			name:        "Containerd container ID",
			containerID: "containerd://abcdef1234567890",
			expected:    "containerd://abcdef1234567890",
		},
		{
			name:        "Empty container ID",
			containerID: "",
			expected:    "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			container := createTestContainer("app", "nginx:latest", true)
			pod := createTestPodWithContainers("test-pod", "default", []corev1.Container{*container})

			// Set the container ID in the status
			pod.Status.ContainerStatuses[0].ContainerID = tc.containerID

			entry := handler.createLogEntry(pod, &pod.Status.ContainerStatuses[0], false)

			if entry.ContainerID != tc.expected {
				t.Errorf("Expected ContainerID '%s', got '%s'", tc.expected, entry.ContainerID)
			}
		})
	}
}

func TestContainerHandler_ContainerID_InitContainer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewContainerHandler(client, nil, []string{})

	// Create test init and regular containers
	initContainer := createTestContainer("init", "busybox:latest", true)
	regularContainer := createTestContainer("app", "nginx:latest", true)

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{*initContainer},
			Containers:     []corev1.Container{*regularContainer},
		},
		Status: corev1.PodStatus{
			InitContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "init",
					Image:        "busybox:latest",
					ImageID:      "docker://sha256:init",
					ContainerID:  "docker://init123456",
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
			},
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "app",
					Image:        "nginx:latest",
					ImageID:      "docker://sha256:app",
					ContainerID:  "docker://app789012",
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
			},
		},
	}

	// Test init container ContainerID
	initEntry := handler.createLogEntry(pod, &pod.Status.InitContainerStatuses[0], true)
	if initEntry.ContainerID != "docker://init123456" {
		t.Errorf("Expected init container ID 'docker://init123456', got '%s'", initEntry.ContainerID)
	}

	// Test regular container ContainerID
	regularEntry := handler.createLogEntry(pod, &pod.Status.ContainerStatuses[0], false)
	if regularEntry.ContainerID != "docker://app789012" {
		t.Errorf("Expected regular container ID 'docker://app789012', got '%s'", regularEntry.ContainerID)
	}
}

func TestContainerHandler_MultipleContainersInPod(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewContainerHandler(client, nil, []string{})

	// Create test containers with different configurations
	container1 := createTestContainer("nginx", "nginx:latest", true)
	container2 := createTestContainer("sidecar", "busybox:latest", true)
	container3 := createTestContainer("logger", "fluent/fluent-bit:latest", true)

	// Create a pod with multiple containers
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "multi-container-pod",
			Namespace: "default",
			Labels: map[string]string{
				"app": "multi-container-app",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{*container1, *container2, *container3},
		},
		Status: corev1.PodStatus{
			ContainerStatuses: []corev1.ContainerStatus{
				{
					Name:         "nginx",
					Image:        "nginx:latest",
					ImageID:      "docker://sha256:nginx123",
					ContainerID:  "docker://nginx-container-id-123",
					Ready:        true,
					RestartCount: 0,
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
				{
					Name:         "sidecar",
					Image:        "busybox:latest",
					ImageID:      "docker://sha256:busybox456",
					ContainerID:  "docker://sidecar-container-id-456",
					Ready:        true,
					RestartCount: 1, // This container has restarted once
					State: corev1.ContainerState{
						Running: &corev1.ContainerStateRunning{
							StartedAt: metav1.Now(),
						},
					},
				},
				{
					Name:         "logger",
					Image:        "fluent/fluent-bit:latest",
					ImageID:      "docker://sha256:fluentbit789",
					ContainerID:  "docker://logger-container-id-789",
					Ready:        false, // This container is not ready
					RestartCount: 0,
					State: corev1.ContainerState{
						Waiting: &corev1.ContainerStateWaiting{
							Reason:  "ImagePullBackOff",
							Message: "Failed to pull image",
						},
					},
				},
			},
		},
	}

	// Test processing all containers in the pod
	entries, err := handler.processPods(context.Background(), []any{pod}, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Should have 2 container entries (only running containers are logged, waiting containers are filtered out)
	if len(entries) != 2 {
		t.Fatalf("Expected 2 container entries (running containers only), got %d", len(entries))
	}

	// Convert to ContainerData for easier testing
	containerEntries := make([]types.ContainerData, 2)
	for i, entry := range entries {
		containerData, ok := entry.(types.ContainerData)
		if !ok {
			t.Fatalf("Expected ContainerData type, got %T", entry)
		}
		containerEntries[i] = containerData
	}

	// Sort by container name for consistent testing
	for i := 0; i < len(containerEntries); i++ {
		for j := i + 1; j < len(containerEntries); j++ {
			if containerEntries[i].Name > containerEntries[j].Name {
				containerEntries[i], containerEntries[j] = containerEntries[j], containerEntries[i]
			}
		}
	}

	// Test nginx container (first alphabetically among running containers)
	nginxEntry := containerEntries[0]
	if nginxEntry.Name != "nginx" {
		t.Errorf("Expected first container name 'nginx', got '%s'", nginxEntry.Name)
	}
	if nginxEntry.ContainerID != "docker://nginx-container-id-123" {
		t.Errorf("Expected nginx ContainerID 'docker://nginx-container-id-123', got '%s'", nginxEntry.ContainerID)
	}
	if nginxEntry.Image != "nginx:latest" {
		t.Errorf("Expected nginx image 'nginx:latest', got '%s'", nginxEntry.Image)
	}
	if nginxEntry.State != "running" {
		t.Errorf("Expected nginx state 'running', got '%s'", nginxEntry.State)
	}
	if nginxEntry.Ready == nil || !*nginxEntry.Ready {
		t.Error("Expected nginx container to be ready")
	}
	if nginxEntry.RestartCount != 0 {
		t.Errorf("Expected nginx restart count 0, got %d", nginxEntry.RestartCount)
	}

	// Test sidecar container (second alphabetically among running containers)
	sidecarEntry := containerEntries[1]
	if sidecarEntry.Name != "sidecar" {
		t.Errorf("Expected second container name 'sidecar', got '%s'", sidecarEntry.Name)
	}
	if sidecarEntry.ContainerID != "docker://sidecar-container-id-456" {
		t.Errorf("Expected sidecar ContainerID 'docker://sidecar-container-id-456', got '%s'", sidecarEntry.ContainerID)
	}
	if sidecarEntry.Image != "busybox:latest" {
		t.Errorf("Expected sidecar image 'busybox:latest', got '%s'", sidecarEntry.Image)
	}
	if sidecarEntry.State != "running" {
		t.Errorf("Expected sidecar state 'running', got '%s'", sidecarEntry.State)
	}
	if sidecarEntry.Ready == nil || !*sidecarEntry.Ready {
		t.Error("Expected sidecar container to be ready")
	}
	if sidecarEntry.RestartCount != 1 {
		t.Errorf("Expected sidecar restart count 1, got %d", sidecarEntry.RestartCount)
	}

	// Verify all containers belong to the same pod
	for i, entry := range containerEntries {
		if entry.PodName != "multi-container-pod" {
			t.Errorf("Expected container %d pod name 'multi-container-pod', got '%s'", i, entry.PodName)
		}
		if entry.Namespace != "default" {
			t.Errorf("Expected container %d namespace 'default', got '%s'", i, entry.Namespace)
		}
		if entry.ResourceType != "container" {
			t.Errorf("Expected container %d resource type 'container', got '%s'", i, entry.ResourceType)
		}
	}

	// Test the createLogEntry function directly for the waiting container to ensure ContainerID is extracted correctly
	loggerStatus := &pod.Status.ContainerStatuses[2] // The waiting logger container
	loggerEntry := handler.createLogEntry(pod, loggerStatus, false)

	if loggerEntry.Name != "logger" {
		t.Errorf("Expected logger container name 'logger', got '%s'", loggerEntry.Name)
	}
	if loggerEntry.ContainerID != "docker://logger-container-id-789" {
		t.Errorf("Expected logger ContainerID 'docker://logger-container-id-789', got '%s'", loggerEntry.ContainerID)
	}
	if loggerEntry.Image != "fluent/fluent-bit:latest" {
		t.Errorf("Expected logger image 'fluent/fluent-bit:latest', got '%s'", loggerEntry.Image)
	}
	if loggerEntry.State != "waiting" {
		t.Errorf("Expected logger state 'waiting', got '%s'", loggerEntry.State)
	}
	if loggerEntry.WaitingReason != "ImagePullBackOff" {
		t.Errorf("Expected logger waiting reason 'ImagePullBackOff', got '%s'", loggerEntry.WaitingReason)
	}
	if loggerEntry.Ready != nil && *loggerEntry.Ready {
		t.Error("Expected logger container to not be ready")
	}
}

// Test that when env var filter set, EnvironmentVariables populated only with requested vars
func TestContainerHandler_EnvironmentVariablesCapture(t *testing.T) {
	handler := NewContainerHandler(nil, nil, []string{"GOMAXPROCS", "IGNORED"})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "p1"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "c1",
			Image: "busybox",
			Env:   []corev1.EnvVar{{Name: "GOMAXPROCS", Value: "4"}, {Name: "OTHER", Value: "x"}},
		}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "c1",
			Ready: true,
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}}},
	}

	// Manually invoke createLogEntry (no informer setup needed for this focused test)
	entry := handler.createLogEntry(pod, &pod.Status.ContainerStatuses[0], false)
	if entry.EnvironmentVariables == nil {
		t.Fatalf("expected EnvironmentVariables map, got nil")
	}
	if val, ok := entry.EnvironmentVariables["GOMAXPROCS"]; !ok || val != "4" {
		t.Errorf("expected GOMAXPROCS=4, got %v", entry.EnvironmentVariables)
	}
	if _, ok := entry.EnvironmentVariables["OTHER"]; ok {
		t.Errorf("did not expect OTHER to be captured: %v", entry.EnvironmentVariables)
	}
}

// Test that when filter empty, map not populated
func TestContainerHandler_EnvironmentVariablesDisabled(t *testing.T) {
	handler := NewContainerHandler(nil, nil, []string{})

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "p1"},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name:  "c1",
			Image: "busybox",
			Env:   []corev1.EnvVar{{Name: "GOMAXPROCS", Value: "4"}},
		}}},
		Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
			Name:  "c1",
			Ready: true,
			State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
		}}},
	}
	entry := handler.createLogEntry(pod, &pod.Status.ContainerStatuses[0], false)
	if entry.EnvironmentVariables != nil {
		t.Errorf("expected no EnvironmentVariables map, got %v", entry.EnvironmentVariables)
	}
}
