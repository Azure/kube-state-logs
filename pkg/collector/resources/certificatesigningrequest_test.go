package resources

import (
	"context"
	"testing"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	testutils "github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// createTestCertificateSigningRequest creates a test CSR with various configurations
func createTestCertificateSigningRequest(name string, signerName string, status certificatesv1.RequestConditionType) *certificatesv1.CertificateSigningRequest {
	csr := &certificatesv1.CertificateSigningRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test csr",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: certificatesv1.CertificateSigningRequestSpec{
			Request:    []byte("test-request-data"),
			SignerName: signerName,
			Usages: []certificatesv1.KeyUsage{
				certificatesv1.UsageDigitalSignature,
				certificatesv1.UsageKeyEncipherment,
				certificatesv1.UsageServerAuth,
			},
		},
		Status: certificatesv1.CertificateSigningRequestStatus{
			Conditions: []certificatesv1.CertificateSigningRequestCondition{
				{
					Type:               status,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "TestReason",
					Message:            "Test message",
				},
			},
		},
	}

	return csr
}

func TestNewCertificateSigningRequestHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCertificateSigningRequestHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestCertificateSigningRequestHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCertificateSigningRequestHandler(client)
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

func TestCertificateSigningRequestHandler_Collect(t *testing.T) {
	csr1 := createTestCertificateSigningRequest("test-csr-1", "kubernetes.io/kube-apiserver-client", certificatesv1.CertificateApproved)
	csr2 := createTestCertificateSigningRequest("test-csr-2", "kubernetes.io/kubelet-serving", certificatesv1.RequestConditionType("Pending"))

	client := fake.NewSimpleClientset(csr1, csr2)
	handler := NewCertificateSigningRequestHandler(client)
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

	// Type assert to CertificateSigningRequestData for assertions
	entry, ok := entries[0].(types.CertificateSigningRequestData)
	if !ok {
		t.Fatalf("Expected CertificateSigningRequestData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if entry.SignerName == "" {
		t.Error("Expected signer name to not be empty")
	}
}

func TestCertificateSigningRequestHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCertificateSigningRequestHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	factory.Start(context.Background().Done())
	factory.WaitForCacheSync(context.Background().Done())
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

func TestCertificateSigningRequestHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCertificateSigningRequestHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	invalidObj := &corev1.Pod{}
	handler.GetInformer().GetStore().Add(invalidObj)
	factory.Start(context.Background().Done())
	factory.WaitForCacheSync(context.Background().Done())
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries with invalid object, got %d", len(entries))
	}
}

func TestCertificateSigningRequestHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCertificateSigningRequestHandler(client)
	csr := createTestCertificateSigningRequest("test-csr", "kubernetes.io/kube-apiserver-client", certificatesv1.CertificateApproved)
	entry := handler.createLogEntry(csr)

	if entry.ResourceType != "certificatesigningrequest" {
		t.Errorf("Expected resource type 'certificatesigningrequest', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-csr" {
		t.Errorf("Expected name 'test-csr', got '%s'", entry.Name)
	}

	// Verify CSR-specific fields
	if entry.SignerName != "kubernetes.io/kube-apiserver-client" {
		t.Errorf("Expected signer name 'kubernetes.io/kube-apiserver-client', got '%s'", entry.SignerName)
	}

	if len(entry.Usages) != 3 {
		t.Errorf("Expected 3 usages, got %d", len(entry.Usages))
	}

	// Verify status
	if entry.Status != "Approved" {
		t.Errorf("Expected status 'Approved', got '%s'", entry.Status)
	}

	// Verify metadata
	if entry.Labels["app"] != "test-csr" {
		t.Errorf("Expected label 'app' to be 'test-csr', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test csr" {
		t.Errorf("Expected annotation 'description' to be 'test csr', got '%s'", entry.Annotations["description"])
	}
}

func TestCertificateSigningRequestHandler_createLogEntry_Denied(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCertificateSigningRequestHandler(client)
	csr := createTestCertificateSigningRequest("test-csr", "kubernetes.io/kube-apiserver-client", certificatesv1.CertificateDenied)
	entry := handler.createLogEntry(csr)

	if entry.Status != "Denied" {
		t.Errorf("Expected status 'Denied', got '%s'", entry.Status)
	}
}

func TestCertificateSigningRequestHandler_createLogEntry_Failed(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCertificateSigningRequestHandler(client)
	csr := createTestCertificateSigningRequest("test-csr", "kubernetes.io/kube-apiserver-client", certificatesv1.CertificateFailed)
	entry := handler.createLogEntry(csr)

	if entry.Status != "Failed" {
		t.Errorf("Expected status 'Failed', got '%s'", entry.Status)
	}
}
