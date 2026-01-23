package resources

import (
	"context"
	"sort"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// SecretHandler handles collection of secret metrics
type SecretHandler struct {
	utils.BaseHandler
}

// NewSecretHandler creates a new SecretHandler
func NewSecretHandler(client kubernetes.Interface) *SecretHandler {
	return &SecretHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the secret informer
func (h *SecretHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create secret informer
	informer := factory.Core().V1().Secrets().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers secret metrics from the cluster (uses cache)
func (h *SecretHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all secrets from the cache
	secrets := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range secrets {
		secret, ok := obj.(*corev1.Secret)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, secret.Namespace) {
			continue
		}

		entry := h.createLogEntry(secret)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a SecretData from a secret
func (h *SecretHandler) createLogEntry(secret *corev1.Secret) types.SecretData {
	var dataKeys []string
	for key := range secret.Data {
		dataKeys = append(dataKeys, key)
	}
	for key := range secret.StringData {
		dataKeys = append(dataKeys, key)
	}

	sort.Strings(dataKeys)

	data := types.SecretData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "secret",
					Name:             utils.ExtractName(secret),
					CreatedTimestamp: utils.ExtractCreationTimestamp(secret),
				},
				Namespace: utils.ExtractNamespace(secret),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(secret),
				Annotations: utils.ExtractAnnotations(secret),
			},
		},
		Type:     string(secret.Type),
		DataKeys: dataKeys,
	}

	return data
}
