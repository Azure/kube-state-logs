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

func createTestValidatingAdmissionPolicy(name string) *admissionregistrationv1beta1.ValidatingAdmissionPolicy {
	failurePolicy := admissionregistrationv1beta1.Fail
	return &admissionregistrationv1beta1.ValidatingAdmissionPolicy{
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
		Spec: admissionregistrationv1beta1.ValidatingAdmissionPolicySpec{
			FailurePolicy: &failurePolicy,
			ParamKind: &admissionregistrationv1beta1.ParamKind{
				APIVersion: "v1",
				Kind:       "ConfigMap",
			},
			MatchConstraints: &admissionregistrationv1beta1.MatchResources{
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
			Validations: []admissionregistrationv1beta1.Validation{
				{
					Expression: "object.spec.replicas <= 5",
					Message:    "Replicas must be <= 5",
				},
			},
			AuditAnnotations: []admissionregistrationv1beta1.AuditAnnotation{
				{
					Key:             "replicas",
					ValueExpression: "object.spec.replicas",
				},
			},
			MatchConditions: []admissionregistrationv1beta1.MatchCondition{
				{
					Name:       "test-condition",
					Expression: "object.spec.replicas > 0",
				},
			},
			Variables: []admissionregistrationv1beta1.Variable{
				{
					Name:       "replicas",
					Expression: "object.spec.replicas",
				},
			},
		},
		Status: admissionregistrationv1beta1.ValidatingAdmissionPolicyStatus{
			ObservedGeneration: 1,
			TypeChecking: &admissionregistrationv1beta1.TypeChecking{
				ExpressionWarnings: []admissionregistrationv1beta1.ExpressionWarning{
					{
						FieldRef: "spec.validations[0].expression",
						Warning:  "Deprecated expression syntax",
					},
				},
			},
		},
	}
}

func TestNewValidatingAdmissionPolicyHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created")
	}
}

func TestValidatingAdmissionPolicyHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)
	logger := &testutils.MockLogger{}
	factory := cache.NewSharedIndexInformer(
		&cache.ListWatch{},
		&admissionregistrationv1beta1.ValidatingAdmissionPolicy{},
		0,
		cache.Indexers{},
	)
	handler.SetupBaseInformer(factory, logger)
	if handler.GetInformer() == nil {
		t.Fatal("Expected informer to be set up")
	}
}

func TestValidatingAdmissionPolicyHandler_SetupInformer_Proper(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)
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

func TestValidatingAdmissionPolicyHandler_Collect(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)
	logger := &testutils.MockLogger{}

	// Create test validatingadmissionpolicy
	vap := createTestValidatingAdmissionPolicy("test-vap")

	// Create informer and add test object
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{},
		&admissionregistrationv1beta1.ValidatingAdmissionPolicy{},
		0,
		cache.Indexers{},
	)
	handler.SetupBaseInformer(informer, logger)

	// Add test object to store
	store := informer.GetStore()
	store.Add(vap)

	// Collect entries
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	// Type assert to ValidatingAdmissionPolicyData for assertions
	entry, ok := entries[0].(types.ValidatingAdmissionPolicyData)
	if !ok {
		t.Fatalf("Expected ValidatingAdmissionPolicyData type, got %T", entries[0])
	}

	if entry.Name != "test-vap" {
		t.Errorf("Expected name 'test-vap', got %s", entry.Name)
	}

	// Verify data
	if entry.FailurePolicy != "Fail" {
		t.Errorf("Expected failure policy 'Fail', got %s", entry.FailurePolicy)
	}

	if entry.ParamKind != "ConfigMap" {
		t.Errorf("Expected param kind 'ConfigMap', got %s", entry.ParamKind)
	}

	if entry.ObservedGeneration != int64(1) {
		t.Errorf("Expected observed generation 1, got %v", entry.ObservedGeneration)
	}

	if entry.Labels["app"] != "test-app" {
		t.Errorf("Expected label 'app' to be 'test-app', got %s", entry.Labels["app"])
	}

	if entry.Annotations["test-annotation"] != "test-value" {
		t.Errorf("Expected annotation 'test-annotation' to be 'test-value', got %s", entry.Annotations["test-annotation"])
	}
}

func TestValidatingAdmissionPolicyHandler_Collect_Empty(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)
	logger := &testutils.MockLogger{}

	// Create empty informer
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{},
		&admissionregistrationv1beta1.ValidatingAdmissionPolicy{},
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

func TestValidatingAdmissionPolicyHandler_Collect_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)
	logger := &testutils.MockLogger{}

	// Create informer
	informer := cache.NewSharedIndexInformer(
		&cache.ListWatch{},
		&admissionregistrationv1beta1.ValidatingAdmissionPolicy{},
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

func TestValidatingAdmissionPolicyHandler_CreateLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)
	vap := createTestValidatingAdmissionPolicy("test-vap")
	entry := handler.createLogEntry(vap)

	if entry.Name != "test-vap" {
		t.Errorf("Expected name 'test-vap', got %s", entry.Name)
	}

	if entry.FailurePolicy != "Fail" {
		t.Errorf("Expected failure policy 'Fail', got %s", entry.FailurePolicy)
	}

	if entry.ParamKind != "ConfigMap" {
		t.Errorf("Expected param kind 'ConfigMap', got %s", entry.ParamKind)
	}

	if entry.ObservedGeneration != int64(1) {
		t.Errorf("Expected observed generation 1, got %v", entry.ObservedGeneration)
	}

	if entry.Labels["app"] != "test-app" {
		t.Errorf("Expected label 'app' to be 'test-app', got %s", entry.Labels["app"])
	}

	if entry.Annotations["test-annotation"] != "test-value" {
		t.Errorf("Expected annotation 'test-annotation' to be 'test-value', got %s", entry.Annotations["test-annotation"])
	}
}

func TestValidatingAdmissionPolicyHandler_CreateLogEntry_NoFailurePolicy(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)
	vap := createTestValidatingAdmissionPolicy("test-vap")
	vap.Spec.FailurePolicy = nil
	entry := handler.createLogEntry(vap)

	if entry.FailurePolicy != "" {
		t.Errorf("Expected empty failure policy, got %s", entry.FailurePolicy)
	}
}

func TestValidatingAdmissionPolicyHandler_CreateLogEntry_NoParamKind(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)
	vap := createTestValidatingAdmissionPolicy("test-vap")
	vap.Spec.ParamKind = nil
	entry := handler.createLogEntry(vap)

	if entry.ParamKind != "" {
		t.Errorf("Expected empty param kind, got %s", entry.ParamKind)
	}
}

func TestValidatingAdmissionPolicyHandler_CreateLogEntry_NoObservedGeneration(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewValidatingAdmissionPolicyHandler(client)
	vap := createTestValidatingAdmissionPolicy("test-vap")
	vap.Status.ObservedGeneration = 0
	entry := handler.createLogEntry(vap)

	if entry.ObservedGeneration != int64(0) {
		t.Errorf("Expected observed generation 0, got %v", entry.ObservedGeneration)
	}
}
