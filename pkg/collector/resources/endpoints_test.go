// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

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
	"github.com/azure/kube-state-logs/pkg/utils"
)

func TestEndpointsHandler(t *testing.T) {
	endpoints1 := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "service1",
			Namespace:         "default",
			Labels:            map[string]string{"app": "web"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP:       "10.0.0.1",
						Hostname: "pod1",
						NodeName: stringPtr("node1"),
						TargetRef: &corev1.ObjectReference{
							Kind: "Pod",
							Name: "pod1",
						},
					},
					{
						IP:       "10.0.0.2",
						Hostname: "pod2",
						NodeName: stringPtr("node2"),
						TargetRef: &corev1.ObjectReference{
							Kind: "Pod",
							Name: "pod2",
						},
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Name:     "http",
						Protocol: corev1.ProtocolTCP,
						Port:     80,
					},
					{
						Name:     "https",
						Protocol: corev1.ProtocolTCP,
						Port:     443,
					},
				},
			},
		},
	}

	endpoints2 := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "service2",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: []corev1.EndpointAddress{
					{
						IP:       "10.0.0.3",
						Hostname: "pod3",
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Protocol: corev1.ProtocolUDP,
						Port:     53,
					},
				},
			},
		},
	}

	endpointsEmpty := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "empty-service",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		},
		Subsets: []corev1.EndpointSubset{},
	}

	tests := []struct {
		name           string
		endpoints      []*corev1.Endpoints
		namespaces     []string
		expectedCount  int
		expectedNames  []string
		expectedFields map[string]interface{}
	}{
		{
			name:          "collect all endpoints",
			endpoints:     []*corev1.Endpoints{endpoints1, endpoints2},
			namespaces:    []string{},
			expectedCount: 2,
			expectedNames: []string{"service1", "service2"},
		},
		{
			name:          "collect endpoints from specific namespace",
			endpoints:     []*corev1.Endpoints{endpoints1, endpoints2},
			namespaces:    []string{"default"},
			expectedCount: 1,
			expectedNames: []string{"service1"},
		},
		{
			name:          "collect endpoints with multiple addresses and ports",
			endpoints:     []*corev1.Endpoints{endpoints1},
			namespaces:    []string{},
			expectedCount: 1,
			expectedNames: []string{"service1"},
			expectedFields: map[string]interface{}{
				"ready": true,
			},
		},
		{
			name:          "collect empty endpoints",
			endpoints:     []*corev1.Endpoints{endpointsEmpty},
			namespaces:    []string{},
			expectedCount: 1,
			expectedNames: []string{"empty-service"},
			expectedFields: map[string]interface{}{
				"ready": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.endpoints))
			for i, ep := range tt.endpoints {
				objects[i] = ep
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewEndpointsHandler(client)
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
				endpointsData, ok := entry.(types.EndpointsData)
				if !ok {
					t.Fatalf("Expected EndpointsData type, got %T", entry)
				}
				entryNames[i] = endpointsData.Name
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
					t.Errorf("Expected to find endpoints with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				endpointsData, ok := entries[0].(types.EndpointsData)
				if !ok {
					t.Fatalf("Expected EndpointsData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "ready":
						expectedReady := expectedValue.(bool)
						if endpointsData.Ready == nil || *endpointsData.Ready != expectedReady {
							t.Errorf("Expected ready %v, got %v", expectedReady, endpointsData.Ready)
						}
					}
				}
			}
			for _, entry := range entries {
				endpointsData, ok := entry.(types.EndpointsData)
				if !ok {
					t.Fatalf("Expected EndpointsData type, got %T", entry)
				}
				if endpointsData.ResourceType != "endpoints" {
					t.Errorf("Expected resource type 'endpoints', got %s", endpointsData.ResourceType)
				}
				if endpointsData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if endpointsData.Namespace == "" {
					t.Error("Entry namespace should not be empty")
				}
				if endpointsData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if endpointsData.Addresses == nil {
					t.Error("addresses should not be nil")
				}
				if endpointsData.Ports == nil {
					t.Error("ports should not be nil")
				}
			}
		})
	}
}

func TestEndpointsHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewEndpointsHandler(client)
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

func TestEndpointsHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewEndpointsHandler(client)
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

func stringPtr(s string) *string {
	return &s
}

// createTestEndpoints creates a test endpoints with various configurations
func createTestEndpoints(name, namespace string, addresses int) *corev1.Endpoints {
	endpoints := &corev1.Endpoints{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test endpoints",
			},
			CreationTimestamp: metav1.Now(),
		},
		Subsets: []corev1.EndpointSubset{
			{
				Addresses: make([]corev1.EndpointAddress, addresses),
				NotReadyAddresses: []corev1.EndpointAddress{
					{
						IP: "10.244.0.2",
						TargetRef: &corev1.ObjectReference{
							Kind:      "Pod",
							Name:      "test-pod-2",
							Namespace: namespace,
						},
					},
				},
				Ports: []corev1.EndpointPort{
					{
						Name:     "http",
						Port:     80,
						Protocol: corev1.ProtocolTCP,
					},
					{
						Name:     "https",
						Port:     443,
						Protocol: corev1.ProtocolTCP,
					},
				},
			},
		},
	}

	// Populate addresses
	for i := 0; i < addresses; i++ {
		endpoints.Subsets[0].Addresses[i] = corev1.EndpointAddress{
			IP:       "10.244.0.1",
			Hostname: "test-pod-1",
			NodeName: stringPtr("test-node"),
			TargetRef: &corev1.ObjectReference{
				Kind:      "Pod",
				Name:      "test-pod-1",
				Namespace: namespace,
			},
		}
	}

	return endpoints
}

func TestNewEndpointsHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewEndpointsHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestEndpointsHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewEndpointsHandler(client)
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

func TestEndpointsHandler_Collect(t *testing.T) {
	endpoints1 := createTestEndpoints("test-endpoints-1", "default", 2)
	endpoints2 := createTestEndpoints("test-endpoints-2", "kube-system", 1)

	client := fake.NewSimpleClientset(endpoints1, endpoints2)
	handler := NewEndpointsHandler(client)
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

	// Type assert to EndpointsData for assertions
	entry, ok := entries[0].(types.EndpointsData)
	if !ok {
		t.Fatalf("Expected EndpointsData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if entry.Namespace == "" {
		t.Error("Expected namespace to not be empty")
	}
}

func TestEndpointsHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewEndpointsHandler(client)
	endpoints := createTestEndpoints("test-endpoints", "default", 2)
	entry := handler.createLogEntry(endpoints)

	if entry.ResourceType != "endpoints" {
		t.Errorf("Expected resource type 'endpoints', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-endpoints" {
		t.Errorf("Expected name 'test-endpoints', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify endpoints-specific fields
	if len(entry.Addresses) != 2 {
		t.Errorf("Expected 2 addresses, got %d", len(entry.Addresses))
	}

	if len(entry.Ports) != 2 {
		t.Errorf("Expected 2 ports, got %d", len(entry.Ports))
	}

	if entry.Ready == nil || !*entry.Ready {
		t.Error("Expected Ready to be true")
	}

	// Verify metadata
	if entry.Labels["app"] != "test-endpoints" {
		t.Errorf("Expected label 'app' to be 'test-endpoints', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test endpoints" {
		t.Errorf("Expected annotation 'description' to be 'test endpoints', got '%s'", entry.Annotations["description"])
	}
}

func TestEndpointsHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test endpoints in different namespaces
	endpoints1 := createTestEndpoints("test-endpoints-1", "default", 1)
	endpoints2 := createTestEndpoints("test-endpoints-2", "kube-system", 1)
	endpoints3 := createTestEndpoints("test-endpoints-3", "monitoring", 1)

	client := fake.NewSimpleClientset(endpoints1, endpoints2, endpoints3)
	handler := NewEndpointsHandler(client)
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
		endpointsData, ok := entry.(types.EndpointsData)
		if !ok {
			t.Fatalf("Expected EndpointsData type, got %T", entry)
		}
		namespaces[endpointsData.Namespace] = true
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
