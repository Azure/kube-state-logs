package resources

import (
	"context"
	"time"

	certificatesv1 "k8s.io/api/certificates/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// CertificateSigningRequestHandler handles collection of certificatesigningrequest metrics
type CertificateSigningRequestHandler struct {
	utils.BaseHandler
}

// NewCertificateSigningRequestHandler creates a new CertificateSigningRequestHandler
func NewCertificateSigningRequestHandler(client kubernetes.Interface) *CertificateSigningRequestHandler {
	return &CertificateSigningRequestHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the certificatesigningrequest informer
func (h *CertificateSigningRequestHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create certificatesigningrequest informer
	informer := factory.Certificates().V1().CertificateSigningRequests().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers certificatesigningrequest metrics from the cluster (uses cache)
func (h *CertificateSigningRequestHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all certificatesigningrequests from the cache
	csrs := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range csrs {
		csr, ok := obj.(*certificatesv1.CertificateSigningRequest)
		if !ok {
			continue
		}

		entry := h.createLogEntry(csr)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a CertificateSigningRequestData from a certificatesigningrequest
func (h *CertificateSigningRequestHandler) createLogEntry(csr *certificatesv1.CertificateSigningRequest) types.CertificateSigningRequestData {
	// Convert usages to strings
	var usages []string
	for _, usage := range csr.Spec.Usages {
		usages = append(usages, string(usage))
	}

	// Get status
	status := ""
	if len(csr.Status.Conditions) > 0 {
		status = string(csr.Status.Conditions[0].Type)
	}

	// Create data structure
	data := types.CertificateSigningRequestData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "certificatesigningrequest",
				Name:             utils.ExtractName(csr),
				CreatedTimestamp: utils.ExtractCreationTimestamp(csr),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(csr),
				Annotations: utils.ExtractAnnotations(csr),
			},
		},
		Status:            status,
		SignerName:        csr.Spec.SignerName,
		ExpirationSeconds: csr.Spec.ExpirationSeconds,
		Usages:            usages,
	}

	return data
}
