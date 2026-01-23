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

// createTestNode creates a test Node with various configurations
func createTestNode(name string, status corev1.ConditionStatus) *corev1.Node {
	now := metav1.Now()
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"kubernetes.io/hostname":         name,
				"node-role.kubernetes.io/worker": "",
			},
			Annotations: map[string]string{
				"description": "test node",
			},
			CreationTimestamp: now,
			Generation:        1,
		},
		Spec: corev1.NodeSpec{
			Unschedulable: false,
			Taints: []corev1.Taint{
				{
					Key:    "node.kubernetes.io/not-ready",
					Value:  "true",
					Effect: corev1.TaintEffectNoSchedule,
				},
			},
		},
		Status: corev1.NodeStatus{
			Phase: corev1.NodeRunning,
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "192.168.1.100",
				},
				{
					Type:    corev1.NodeExternalIP,
					Address: "203.0.113.1",
				},
				{
					Type:    corev1.NodeHostName,
					Address: name,
				},
			},
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
				corev1.ResourcePods:   resource.MustParse("110"),
			},
			Conditions: []corev1.NodeCondition{
				{
					Type:               corev1.NodeReady,
					Status:             status,
					LastHeartbeatTime:  metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "KubeletReady",
					Message:            "kubelet is posting ready status",
				},
				{
					Type:               corev1.NodeMemoryPressure,
					Status:             corev1.ConditionFalse,
					LastHeartbeatTime:  metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "KubeletHasSufficientMemory",
					Message:            "kubelet has sufficient memory available",
				},
				{
					Type:               corev1.NodeDiskPressure,
					Status:             corev1.ConditionFalse,
					LastHeartbeatTime:  metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "KubeletHasNoDiskPressure",
					Message:            "kubelet has no disk pressure",
				},
				{
					Type:               corev1.NodePIDPressure,
					Status:             corev1.ConditionFalse,
					LastHeartbeatTime:  metav1.Now(),
					LastTransitionTime: metav1.Now(),
					Reason:             "KubeletHasSufficientPID",
					Message:            "kubelet has sufficient PID available",
				},
			},
			NodeInfo: corev1.NodeSystemInfo{
				Architecture:            "amd64",
				OperatingSystem:         "linux",
				KernelVersion:           "5.4.0-42-generic",
				KubeletVersion:          "v1.24.0",
				KubeProxyVersion:        "v1.24.0",
				ContainerRuntimeVersion: "containerd://1.6.0",
			},
		},
	}
	return node
}

func TestNewNodeHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewNodeHandler(client, nil)
	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestNodeHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewNodeHandler(client, nil)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}
	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if handler.GetInformer() == nil {
		t.Error("Expected informer to be set up")
	}
}

func TestNodeHandler_Collect(t *testing.T) {
	node1 := createTestNode("test-node-1", corev1.ConditionTrue)
	node2 := createTestNode("test-node-2", corev1.ConditionFalse)
	client := fake.NewSimpleClientset(node1, node2)
	handler := NewNodeHandler(client, nil)
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

	for _, entry := range entries {
		_, ok := entry.(types.NodeData)
		if !ok {
			t.Fatalf("Expected NodeData type, got %T", entry)
		}
	}
}

func TestNodeHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewNodeHandler(client, nil)
	node := createTestNode("test-node", corev1.ConditionTrue)
	entry := handler.createLogEntry(node)

	if entry.ResourceType != "node" {
		t.Errorf("Expected resource type 'node', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-node" {
		t.Errorf("Expected name 'test-node', got '%s'", entry.Name)
	}

	// Verify node-specific fields
	if entry.Ready == nil || !*entry.Ready {
		t.Error("Expected Ready condition to be true")
	}

	if entry.InternalIP != "192.168.1.100" {
		t.Errorf("Expected internal IP '192.168.1.100', got '%s'", entry.InternalIP)
	}

	if entry.ExternalIP != "203.0.113.1" {
		t.Errorf("Expected external IP '203.0.113.1', got '%s'", entry.ExternalIP)
	}

	if entry.KernelVersion != "5.4.0-42-generic" {
		t.Errorf("Expected kernel version '5.4.0-42-generic', got '%s'", entry.KernelVersion)
	}

	if entry.OperatingSystem != "linux" {
		t.Errorf("Expected OS 'linux', got '%s'", entry.OperatingSystem)
	}

	if entry.Architecture != "amd64" {
		t.Errorf("Expected architecture 'amd64', got '%s'", entry.Architecture)
	}

	// Verify metadata
	if entry.Labels["kubernetes.io/hostname"] != "test-node" {
		t.Errorf("Expected label 'kubernetes.io/hostname' to be 'test-node', got '%s'", entry.Labels["kubernetes.io/hostname"])
	}

	if entry.Annotations["description"] != "test node" {
		t.Errorf("Expected annotation 'description' to be 'test node', got '%s'", entry.Annotations["description"])
	}
}

func TestNodeHandler_Collect_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewNodeHandler(client, nil)
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
		t.Fatalf("Expected 0 entries for empty cache, got %d", len(entries))
	}
}

func TestNodeHandler_createLogEntry_WithTaints(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewNodeHandler(client, nil)
	node := createTestNode("test-node", corev1.ConditionTrue)

	// Add taints
	node.Spec.Taints = []corev1.Taint{
		{
			Key:    "node-role.kubernetes.io/master",
			Value:  "",
			Effect: corev1.TaintEffectNoSchedule,
		},
		{
			Key:    "dedicated",
			Value:  "gpu",
			Effect: corev1.TaintEffectPreferNoSchedule,
		},
	}

	entry := handler.createLogEntry(node)

	if len(entry.Taints) != 2 {
		t.Fatalf("Expected 2 taints, got %d", len(entry.Taints))
	}

	// Check first taint
	if entry.Taints[0].Key != "node-role.kubernetes.io/master" {
		t.Errorf("Expected first taint key 'node-role.kubernetes.io/master', got '%s'", entry.Taints[0].Key)
	}
	if entry.Taints[0].Effect != "NoSchedule" {
		t.Errorf("Expected first taint effect 'NoSchedule', got '%s'", entry.Taints[0].Effect)
	}

	// Check second taint
	if entry.Taints[1].Key != "dedicated" {
		t.Errorf("Expected second taint key 'dedicated', got '%s'", entry.Taints[1].Key)
	}
	if entry.Taints[1].Value != "gpu" {
		t.Errorf("Expected second taint value 'gpu', got '%s'", entry.Taints[1].Value)
	}
	if entry.Taints[1].Effect != "PreferNoSchedule" {
		t.Errorf("Expected second taint effect 'PreferNoSchedule', got '%s'", entry.Taints[1].Effect)
	}
}
