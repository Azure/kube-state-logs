package resources

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// ResourceQuotaHandler handles collection of resourcequota metrics
type ResourceQuotaHandler struct {
	utils.BaseHandler
}

// NewResourceQuotaHandler creates a new ResourceQuotaHandler
func NewResourceQuotaHandler(client kubernetes.Interface) *ResourceQuotaHandler {
	return &ResourceQuotaHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the resourcequota informer
func (h *ResourceQuotaHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create resourcequota informer
	informer := factory.Core().V1().ResourceQuotas().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers resourcequota metrics from the cluster (uses cache)
func (h *ResourceQuotaHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all resourcequotas from the cache
	resourcequotas := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range resourcequotas {
		resourcequota, ok := obj.(*corev1.ResourceQuota)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, resourcequota.Namespace) {
			continue
		}

		entry := h.createLogEntry(resourcequota)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a ResourceQuotaData from a resourcequota
func (h *ResourceQuotaHandler) createLogEntry(quota *corev1.ResourceQuota) types.ResourceQuotaData {
	hard := resourceListToInt64Map(quota.Spec.Hard)
	used := resourceListToInt64Map(quota.Status.Used)

	// Format scopes
	// See: https://kubernetes.io/docs/concepts/policy/resource-quotas/#quota-scopes
	scopes := make([]string, len(quota.Spec.Scopes))
	for i, scope := range quota.Spec.Scopes {
		scopes[i] = string(scope)
	}

	// Create data structure
	data := types.ResourceQuotaData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "resourcequota",
					Name:             utils.ExtractName(quota),
					CreatedTimestamp: utils.ExtractCreationTimestamp(quota),
				},
				Namespace: utils.ExtractNamespace(quota),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(quota),
				Annotations: utils.ExtractAnnotations(quota),
			},
		},
		Hard:   hard,
		Used:   used,
		Scopes: scopes,
	}

	return data
}

// resourceListToInt64Map converts corev1.ResourceList to map[string]int64
func resourceListToInt64Map(rl corev1.ResourceList) map[string]int64 {
	result := make(map[string]int64)
	for resourceName, quantity := range rl {
		result[string(resourceName)] = quantity.Value()
	}
	return result
}
