package resources

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// NamespaceHandler handles collection of namespace metrics
type NamespaceHandler struct {
	utils.BaseHandler
}

// NewNamespaceHandler creates a new NamespaceHandler
func NewNamespaceHandler(client kubernetes.Interface) *NamespaceHandler {
	return &NamespaceHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the namespace informer
func (h *NamespaceHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create namespace informer
	informer := factory.Core().V1().Namespaces().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers namespace metrics from the cluster (uses cache)
func (h *NamespaceHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all namespaces from the cache
	namespaceList := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range namespaceList {
		namespace, ok := obj.(*corev1.Namespace)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, namespace.Name) {
			continue
		}

		entry := h.createLogEntry(namespace)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a NamespaceData from a namespace
func (h *NamespaceHandler) createLogEntry(ns *corev1.Namespace) types.NamespaceData {
	// Determine phase
	phase := string(ns.Status.Phase)

	// Check conditions in a single loop
	var conditionActive, conditionTerminating *bool
	conditions := make(map[string]*bool)

	for _, condition := range ns.Status.Conditions {
		val := utils.ConvertCoreConditionStatus(condition.Status)

		switch condition.Type {
		case "Active":
			conditionActive = val
		case "Terminating":
			conditionTerminating = val
		default:
			// Add unknown conditions to the map
			conditions[string(condition.Type)] = val
		}
	}

	var deletionTimestamp *v1.Time
	if t := utils.ExtractDeletionTimestamp(ns); t != nil {
		ts := v1.NewTime(*t)
		deletionTimestamp = &ts
	}

	data := types.NamespaceData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "namespace",
				Name:             utils.ExtractName(ns),
				CreatedTimestamp: utils.ExtractCreationTimestamp(ns),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(ns),
				Annotations: utils.ExtractAnnotations(ns),
			},
		},
		Phase:                phase,
		ConditionActive:      conditionActive,
		ConditionTerminating: conditionTerminating,
		Conditions:           conditions,
		DeletionTimestamp:    deletionTimestamp,
		Finalizers:           ns.ObjectMeta.Finalizers,
	}

	return data
}
