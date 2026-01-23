package resources

import (
	"context"
	"testing"
	"time"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
)

func TestIngressHandler(t *testing.T) {
	pathTypePrefix := networkingv1.PathTypePrefix
	pathTypeExact := networkingv1.PathTypeExact
	ingressClassName := "nginx"

	ingress1 := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "web-ingress",
			Namespace:         "default",
			Labels:            map[string]string{"app": "web"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "web-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
								{
									Path:     "/api",
									PathType: &pathTypeExact,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "api-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 8080,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{"example.com"},
					SecretName: "tls-secret",
				},
			},
		},
		Status: networkingv1.IngressStatus{
			LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{
					{
						IP:       "192.168.1.100",
						Hostname: "example.com",
					},
				},
			},
		},
	}

	ingress2 := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "api-ingress",
			Namespace:         "kube-system",
			CreationTimestamp: metav1.Now(),
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "api.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "api-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 8080,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	ingressNoLoadBalancer := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "no-lb-ingress",
			Namespace:         "default",
			CreationTimestamp: metav1.Now(),
		},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{
					Host: "nolb.example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "nolb-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name           string
		ingresses      []*networkingv1.Ingress
		namespaces     []string
		expectedCount  int
		expectedNames  []string
		expectedFields map[string]interface{}
	}{
		{
			name:          "collect all ingresses",
			ingresses:     []*networkingv1.Ingress{ingress1, ingress2},
			namespaces:    []string{},
			expectedCount: 2,
			expectedNames: []string{"web-ingress", "api-ingress"},
		},
		{
			name:          "collect ingresses from specific namespace",
			ingresses:     []*networkingv1.Ingress{ingress1, ingress2},
			namespaces:    []string{"default"},
			expectedCount: 1,
			expectedNames: []string{"web-ingress"},
		},
		{
			name:          "collect ingress with load balancer",
			ingresses:     []*networkingv1.Ingress{ingress1},
			namespaces:    []string{},
			expectedCount: 1,
			expectedNames: []string{"web-ingress"},
			expectedFields: map[string]interface{}{
				"condition_load_balancer_ready": true,
			},
		},
		{
			name:          "collect ingress without load balancer",
			ingresses:     []*networkingv1.Ingress{ingressNoLoadBalancer},
			namespaces:    []string{},
			expectedCount: 1,
			expectedNames: []string{"no-lb-ingress"},
			expectedFields: map[string]interface{}{
				"condition_load_balancer_ready": false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.ingresses))
			for i, ing := range tt.ingresses {
				objects[i] = ing
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewIngressHandler(client)
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
				ingressData, ok := entry.(types.IngressData)
				if !ok {
					t.Fatalf("Expected IngressData type, got %T", entry)
				}
				entryNames[i] = ingressData.Name
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
					t.Errorf("Expected to find ingress with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				ingressData, ok := entries[0].(types.IngressData)
				if !ok {
					t.Fatalf("Expected IngressData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "condition_load_balancer_ready":
						expected := expectedValue.(bool)
						if ingressData.ConditionLoadBalancerReady == nil {
							if expected {
								t.Errorf("Expected condition_load_balancer_ready %v, got nil", expectedValue)
							}
						} else if *ingressData.ConditionLoadBalancerReady != expected {
							t.Errorf("Expected condition_load_balancer_ready %v, got %v", expectedValue, *ingressData.ConditionLoadBalancerReady)
						}
					}
				}
			}
			for _, entry := range entries {
				ingressData, ok := entry.(types.IngressData)
				if !ok {
					t.Fatalf("Expected IngressData type, got %T", entry)
				}
				if ingressData.ResourceType != "ingress" {
					t.Errorf("Expected resource type 'ingress', got %s", ingressData.ResourceType)
				}
				if ingressData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if ingressData.Namespace == "" {
					t.Error("Entry namespace should not be empty")
				}
				if ingressData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if ingressData.Rules == nil {
					t.Error("rules should not be nil")
				}
				if ingressData.TLS == nil {
					t.Error("tls should not be nil")
				}
				if ingressData.LoadBalancerIngress == nil {
					t.Error("loadBalancerIngress should not be nil")
				}
			}
		})
	}
}

func TestIngressHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewIngressHandler(client)
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

func TestIngressHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewIngressHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	invalidObj := &networkingv1.NetworkPolicy{}
	handler.GetInformer().GetStore().Add(invalidObj)
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries with invalid object, got %d", len(entries))
	}
}

// createTestIngress creates a test ingress with various configurations
func createTestIngress(name, namespace string) *networkingv1.Ingress {
	pathTypePrefix := networkingv1.PathTypePrefix
	ingressClassName := "nginx"
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description":                 "test ingress",
				"kubernetes.io/ingress.class": "nginx",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: &ingressClassName,
			Rules: []networkingv1.IngressRule{
				{
					Host: "example.com",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathTypePrefix,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: "test-service",
											Port: networkingv1.ServiceBackendPort{
												Number: 80,
											},
										},
									},
								},
							},
						},
					},
				},
			},
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{"example.com"},
					SecretName: "tls-secret",
				},
			},
		},
		Status: networkingv1.IngressStatus{
			LoadBalancer: networkingv1.IngressLoadBalancerStatus{
				Ingress: []networkingv1.IngressLoadBalancerIngress{
					{
						IP:       "192.168.1.100",
						Hostname: "example.com",
					},
				},
			},
		},
	}

	return ingress
}

func TestIngressHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewIngressHandler(client)
	ingress := createTestIngress("test-ingress", "default")
	entry := handler.createLogEntry(ingress)

	if entry.ResourceType != "ingress" {
		t.Errorf("Expected resource type 'ingress', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-ingress" {
		t.Errorf("Expected name 'test-ingress', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify ingress-specific fields
	if entry.IngressClassName == nil || *entry.IngressClassName != "nginx" {
		t.Errorf("Expected ingress class name 'nginx', got %v", entry.IngressClassName)
	}

	if len(entry.Rules) != 1 {
		t.Errorf("Expected 1 rule, got %d", len(entry.Rules))
	}

	if len(entry.TLS) != 1 {
		t.Errorf("Expected 1 TLS config, got %d", len(entry.TLS))
	}

	if len(entry.LoadBalancerIngress) != 1 {
		t.Errorf("Expected 1 load balancer ingress, got %d", len(entry.LoadBalancerIngress))
	}

	if entry.ConditionLoadBalancerReady == nil || !*entry.ConditionLoadBalancerReady {
		t.Error("Expected ConditionLoadBalancerReady to be true")
	}

	// Verify metadata
	if entry.Labels["app"] != "test-ingress" {
		t.Errorf("Expected label 'app' to be 'test-ingress', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test ingress" {
		t.Errorf("Expected annotation 'description' to be 'test ingress', got '%s'", entry.Annotations["description"])
	}
}

func TestIngressHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test ingresses in different namespaces
	ingress1 := createTestIngress("test-ingress-1", "default")
	ingress2 := createTestIngress("test-ingress-2", "kube-system")
	ingress3 := createTestIngress("test-ingress-3", "monitoring")

	client := fake.NewSimpleClientset(ingress1, ingress2, ingress3)
	handler := NewIngressHandler(client)
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
		ingressData, ok := entry.(types.IngressData)
		if !ok {
			t.Fatalf("Expected IngressData type, got %T", entry)
		}
		namespaces[ingressData.Namespace] = true
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

func TestNewIngressHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewIngressHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}
}

func TestIngressHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewIngressHandler(client)
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

func TestIngressHandler_Collect(t *testing.T) {
	ingress1 := createTestIngress("test-ingress-1", "default")
	ingress2 := createTestIngress("test-ingress-2", "kube-system")

	client := fake.NewSimpleClientset(ingress1, ingress2)
	handler := NewIngressHandler(client)
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

	// Type assert to IngressData for assertions
	entry, ok := entries[0].(types.IngressData)
	if !ok {
		t.Fatalf("Expected IngressData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if entry.Namespace == "" {
		t.Error("Expected namespace to not be empty")
	}
}
