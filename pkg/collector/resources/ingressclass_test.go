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

func TestIngressClassHandler(t *testing.T) {
	ic1 := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "nginx",
			Labels:            map[string]string{"type": "load-balancer"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Spec: networkingv1.IngressClassSpec{
			Controller: "k8s.io/ingress-nginx",
		},
	}

	ic2 := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "default-nginx",
			Annotations: map[string]string{
				"ingressclass.kubernetes.io/is-default-class": "true",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: networkingv1.IngressClassSpec{
			Controller: "k8s.io/ingress-nginx",
		},
	}

	icEmptyController := &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "empty-controller",
			CreationTimestamp: metav1.Now(),
		},
		Spec: networkingv1.IngressClassSpec{
			Controller: "",
		},
	}

	tests := []struct {
		name           string
		ingressClasses []*networkingv1.IngressClass
		expectedCount  int
		expectedNames  []string
		expectedFields map[string]interface{}
	}{
		{
			name:           "collect all ingress classes",
			ingressClasses: []*networkingv1.IngressClass{ic1, ic2},
			expectedCount:  2,
			expectedNames:  []string{"nginx", "default-nginx"},
		},
		{
			name:           "collect default ingress class",
			ingressClasses: []*networkingv1.IngressClass{ic2},
			expectedCount:  1,
			expectedNames:  []string{"default-nginx"},
			expectedFields: map[string]interface{}{
				"is_default": true,
			},
		},
		{
			name:           "collect ingress class with nginx controller",
			ingressClasses: []*networkingv1.IngressClass{ic1},
			expectedCount:  1,
			expectedNames:  []string{"nginx"},
			expectedFields: map[string]interface{}{
				"controller": "k8s.io/ingress-nginx",
			},
		},
		{
			name:           "collect ingress class with empty controller",
			ingressClasses: []*networkingv1.IngressClass{icEmptyController},
			expectedCount:  1,
			expectedNames:  []string{"empty-controller"},
			expectedFields: map[string]interface{}{
				"controller": "",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.ingressClasses))
			for i, ic := range tt.ingressClasses {
				objects[i] = ic
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewIngressClassHandler(client)
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
			if len(entries) != tt.expectedCount {
				t.Errorf("Expected %d entries, got %d", tt.expectedCount, len(entries))
			}
			entryNames := make([]string, len(entries))
			for i, entry := range entries {
				ingressClassData, ok := entry.(types.IngressClassData)
				if !ok {
					t.Fatalf("Expected IngressClassData type, got %T", entry)
				}
				entryNames[i] = ingressClassData.Name
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
					t.Errorf("Expected to find ingress class with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				ingressClassData, ok := entries[0].(types.IngressClassData)
				if !ok {
					t.Fatalf("Expected IngressClassData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "controller":
						if ingressClassData.Controller != expectedValue.(string) {
							t.Errorf("Expected controller %s, got %v", expectedValue, ingressClassData.Controller)
						}
					case "is_default":
						if ingressClassData.IsDefault != expectedValue.(bool) {
							t.Errorf("Expected is_default %v, got %v", expectedValue, ingressClassData.IsDefault)
						}
					}
				}
			}
			for _, entry := range entries {
				ingressClassData, ok := entry.(types.IngressClassData)
				if !ok {
					t.Fatalf("Expected IngressClassData type, got %T", entry)
				}
				if ingressClassData.ResourceType != "ingressclass" {
					t.Errorf("Expected resource type 'ingressclass', got %s", ingressClassData.ResourceType)
				}
				if ingressClassData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if ingressClassData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
			}
		})
	}
}

func TestIngressClassHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewIngressClassHandler(client)
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

func TestIngressClassHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewIngressClassHandler(client)
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
