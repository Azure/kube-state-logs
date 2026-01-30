// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"k8s.io/client-go/informers"
)

func createTestValidatingAdmissionPolicyBinding(name string) *admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding {
	return &admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app": "test-app",
			},
			Annotations: map[string]string{
				"test-annotation": "test-value",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName: "test-policy",
			ParamRef: &admissionregistrationv1beta1.ParamRef{
				Name: "test-param",
			},
			MatchResources: &admissionregistrationv1beta1.MatchResources{
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"namespace-label": "namespace-value",
					},
				},
				ObjectSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"object-label": "object-value",
					},
				},
			},
			ValidationActions: []admissionregistrationv1beta1.ValidationAction{
				admissionregistrationv1beta1.Deny,
				admissionregistrationv1beta1.Warn,
			},
		},
	}
}

func TestNewValidatingAdmissionPolicyBindingHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyBindingHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created")
	}
}

func TestValidatingAdmissionPolicyBindingHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyBindingHandler(client)
	logger := &testutils.MockLogger{}
	factory := cache.NewSharedIndexInformer(
		&cache.ListWatch{},
		&admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{},
		0,
		cache.Indexers{},
	)
	handler.SetupBaseInformer(factory, logger)
	if handler.GetInformer() == nil {
		t.Fatal("Expected informer to be set up")
	}
}

func TestValidatingAdmissionPolicyBindingHandler_SetupInformer_Proper(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyBindingHandler(client)
	logger := &testutils.MockLogger{}

	// Create a proper informer factory
	factory := informers.NewSharedInformerFactory(client, 0)

	err := handler.SetupInformer(factory, logger, 0)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if handler.GetInformer() == nil {
		t.Fatal("Expected informer to be set up")
	}
}

func TestValidatingAdmissionPolicyBindingHandler_Collect(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyBindingHandler(client)
	logger := &testutils.MockLogger{}

	// Create test validatingadmissionpolicybinding
	vapb := createTestValidatingAdmissionPolicyBinding("test-vapb")

	// Create informer and add test object
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{},
		&admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{},
		0,
		cache.Indexers{},
	)
	handler.SetupBaseInformer(informer, logger)

	// Add test object to store
	store := informer.GetStore()
	store.Add(vapb)

	// Collect entries
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	// Type assert to ValidatingAdmissionPolicyBindingData for assertions
	entry, ok := entries[0].(types.ValidatingAdmissionPolicyBindingData)
	if !ok {
		t.Fatalf("Expected ValidatingAdmissionPolicyBindingData type, got %T", entries[0])
	}

	if entry.Name != "test-vapb" {
		t.Errorf("Expected name 'test-vapb', got %s", entry.Name)
	}

	// Verify data
	if entry.PolicyName != "test-policy" {
		t.Errorf("Expected policy name 'test-policy', got %s", entry.PolicyName)
	}

	if entry.ParamRef != "test-param" {
		t.Errorf("Expected param ref 'test-param', got %s", entry.ParamRef)
	}

	if entry.Labels["app"] != "test-app" {
		t.Errorf("Expected label 'app' to be 'test-app', got %s", entry.Labels["app"])
	}

	if entry.Annotations["test-annotation"] != "test-value" {
		t.Errorf("Expected annotation 'test-annotation' to be 'test-value', got %s", entry.Annotations["test-annotation"])
	}
}

func TestValidatingAdmissionPolicyBindingHandler_Collect_Empty(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyBindingHandler(client)
	logger := &testutils.MockLogger{}

	// Create empty informer
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{},
		&admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{},
		0,
		cache.Indexers{},
	)
	handler.SetupBaseInformer(informer, logger)

	// Collect entries
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("Expected 0 entries, got %d", len(entries))
	}
}

func TestValidatingAdmissionPolicyBindingHandler_Collect_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyBindingHandler(client)
	logger := &testutils.MockLogger{}

	// Create informer
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{},
		&admissionregistrationv1beta1.ValidatingAdmissionPolicyBinding{},
		0,
		cache.Indexers{},
	)
	handler.SetupBaseInformer(informer, logger)

	// Add invalid object to store
	store := informer.GetStore()
	store.Add(&corev1.Pod{})

	// Collect entries
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 0 {
		t.Fatalf("Expected 0 entries with invalid object, got %d", len(entries))
	}
}

func TestValidatingAdmissionPolicyBindingHandler_CreateLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyBindingHandler(client)
	vapb := createTestValidatingAdmissionPolicyBinding("test-vapb")
	entry := handler.createLogEntry(vapb)

	if entry.Name != "test-vapb" {
		t.Errorf("Expected name 'test-vapb', got %s", entry.Name)
	}

	if entry.PolicyName != "test-policy" {
		t.Errorf("Expected policy name 'test-policy', got %s", entry.PolicyName)
	}

	if entry.ParamRef != "test-param" {
		t.Errorf("Expected param ref 'test-param', got %s", entry.ParamRef)
	}

	if entry.Labels["app"] != "test-app" {
		t.Errorf("Expected label 'app' to be 'test-app', got %s", entry.Labels["app"])
	}

	if entry.Annotations["test-annotation"] != "test-value" {
		t.Errorf("Expected annotation 'test-annotation' to be 'test-value', got %s", entry.Annotations["test-annotation"])
	}
}

func TestValidatingAdmissionPolicyBindingHandler_CreateLogEntry_NoParamRef(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyBindingHandler(client)
	vapb := createTestValidatingAdmissionPolicyBinding("test-vapb")
	vapb.Spec.ParamRef = nil
	entry := handler.createLogEntry(vapb)

	if entry.ParamRef != "" {
		t.Errorf("Expected empty param ref, got %s", entry.ParamRef)
	}
}

func TestValidatingAdmissionPolicyBindingHandler_CreateLogEntry_NoPolicyName(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyBindingHandler(client)
	vapb := createTestValidatingAdmissionPolicyBinding("test-vapb")
	vapb.Spec.PolicyName = ""
	entry := handler.createLogEntry(vapb)

	if entry.PolicyName != "" {
		t.Errorf("Expected empty policy name, got %s", entry.PolicyName)
	}
}
