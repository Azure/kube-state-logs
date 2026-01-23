package resources

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	testutils "github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// createTestService creates a test Service with various configurations
func createTestService(name, namespace string, serviceType corev1.ServiceType) *corev1.Service {
	now := metav1.Now()
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test service",
			},
			CreationTimestamp: now,
			Generation:        1,
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					NodePort:   30080,
				},
			},
			Selector: map[string]string{
				"app": name,
			},
			ClusterIP: "10.0.0.1",
		},
	}
	return service
}

// createTestServiceEndpoints creates a test service with various configurations
func createTestServiceEndpoints(name, namespace string, serviceType corev1.ServiceType) *corev1.Service {
	now := metav1.Now()
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test service",
			},
			CreationTimestamp: now,
			Generation:        1,
		},
		Spec: corev1.ServiceSpec{
			Type: serviceType,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(8080),
					NodePort:   30080,
				},
			},
			Selector: map[string]string{
				"app": name,
			},
			ClusterIP: "10.0.0.1",
		},
	}
	return service
}

// createTestEndpointsForService creates test Endpoints for a service
func createTestEndpointsForService(name, namespace string, addresses int) *corev1.Endpoints {
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: make([]corev1.EndpointAddress, addresses),
				Ports: []corev1.EndpointPort{
					{
						Name: "http",
						Port: 8080,
					},
				},
			},
		},
	}
	return endpoints
}

func TestNewServiceHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewServiceHandler(client)
	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestServiceHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewServiceHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}
	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if handler.GetInformer() == nil {
		t.Error("Expected informer to be set up")
	}
	if handler.endpointsInformer == nil {
		t.Error("Expected endpoints informer to be set up")
	}
}

func TestServiceHandler_Collect(t *testing.T) {
	// Create test services
	service1 := createTestService("test-service-1", "default", corev1.ServiceTypeClusterIP)
	service2 := createTestService("test-service-2", "kube-system", corev1.ServiceTypeLoadBalancer)

	// Create fake client with test services
	client := fake.NewSimpleClientset(service1, service2)
	handler := NewServiceHandler(client)
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

	// Test collecting all services
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

	// Type assert to ServiceData for assertions
	entry, ok := entries[0].(types.ServiceData)
	if !ok {
		t.Fatalf("Expected ServiceData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestServiceHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewServiceHandler(client)

	// Test service with ClusterIP type
	service := createTestService("test-service", "default", corev1.ServiceTypeClusterIP)
	entry := handler.createLogEntry(service)

	// Verify basic fields
	if entry.ResourceType != "service" {
		t.Errorf("Expected resource type 'service', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-service" {
		t.Errorf("Expected name 'test-service', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify service-specific fields
	if entry.Type != "ClusterIP" {
		t.Errorf("Expected service type 'ClusterIP', got '%s'", entry.Type)
	}

	if entry.ClusterIP != "10.0.0.1" {
		t.Errorf("Expected cluster IP '10.0.0.1', got '%s'", entry.ClusterIP)
	}

	if len(entry.Ports) != 1 {
		t.Errorf("Expected 1 port, got %d", len(entry.Ports))
	}

	if entry.Ports[0].Port != 80 {
		t.Errorf("Expected port 80, got %d", entry.Ports[0].Port)
	}

	if entry.Ports[0].TargetPort != 8080 {
		t.Errorf("Expected target port 8080, got %d", entry.Ports[0].TargetPort)
	}

	if entry.Ports[0].Protocol != "TCP" {
		t.Errorf("Expected protocol 'TCP', got '%s'", entry.Ports[0].Protocol)
	}

	// Verify metadata
	if entry.Labels["app"] != "test-service" {
		t.Errorf("Expected label 'app' to be 'test-service', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test service" {
		t.Errorf("Expected annotation 'description' to be 'test service', got '%s'", entry.Annotations["description"])
	}
}

func TestServiceHandler_createLogEntry_LoadBalancer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewServiceHandler(client)

	// Test service with LoadBalancer type
	service := createTestService("test-service", "default", corev1.ServiceTypeLoadBalancer)
	service.Status.LoadBalancer.Ingress = []corev1.LoadBalancerIngress{
		{
			IP:       "192.168.1.100",
			Hostname: "test-lb.example.com",
		},
	}

	entry := handler.createLogEntry(service)

	if entry.Type != "LoadBalancer" {
		t.Errorf("Expected service type 'LoadBalancer', got '%s'", entry.Type)
	}

	if len(entry.LoadBalancerIngress) != 1 {
		t.Errorf("Expected 1 load balancer ingress, got %d", len(entry.LoadBalancerIngress))
	}

	if entry.LoadBalancerIngress[0].IP != "192.168.1.100" {
		t.Errorf("Expected load balancer IP '192.168.1.100', got '%s'", entry.LoadBalancerIngress[0].IP)
	}

	if entry.LoadBalancerIngress[0].Hostname != "test-lb.example.com" {
		t.Errorf("Expected load balancer hostname 'test-lb.example.com', got '%s'", entry.LoadBalancerIngress[0].Hostname)
	}
}

func TestServiceHandler_Collect_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewServiceHandler(client)
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

func TestServiceHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test services in different namespaces
	service1 := createTestService("test-service-1", "default", corev1.ServiceTypeClusterIP)
	service2 := createTestService("test-service-2", "kube-system", corev1.ServiceTypeLoadBalancer)
	service3 := createTestService("test-service-3", "monitoring", corev1.ServiceTypeNodePort)

	client := fake.NewSimpleClientset(service1, service2, service3)
	handler := NewServiceHandler(client)
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
		entryData, ok := entry.(types.ServiceData)
		if !ok {
			t.Fatalf("Expected ServiceData type, got %T", entry)
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

func TestServiceHandler_countEndpointsForService(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewServiceHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}
	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	// Create endpoints before starting informers
	endpoints := createTestEndpointsForService("test-service", "default", 3)
	_, err = client.CoreV1().Endpoints("default").Create(context.Background(), endpoints, metav1.CreateOptions{})
	if err != nil {
		t.Fatalf("Failed to create endpoints: %v", err)
	}
	factory.Start(nil)
	factory.WaitForCacheSync(nil)
	// Test with endpoints
	count := handler.countEndpointsForService("default", "test-service")
	if count != 3 {
		t.Errorf("Expected 3 endpoints, got %d", count)
	}
	// Test with no endpoints
	count = handler.countEndpointsForService("default", "nonexistent-service")
	if count != 0 {
		t.Errorf("Expected 0 endpoints, got %d", count)
	}
}
