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

// LimitRangeHandler handles collection of limitrange metrics
type LimitRangeHandler struct {
	utils.BaseHandler
}

// NewLimitRangeHandler creates a new LimitRangeHandler
func NewLimitRangeHandler(client kubernetes.Interface) *LimitRangeHandler {
	return &LimitRangeHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the limitrange informer
func (h *LimitRangeHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create limitrange informer
	informer := factory.Core().V1().LimitRanges().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers limitrange metrics from the cluster (uses cache)
func (h *LimitRangeHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all limitranges from the cache
	limitranges := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range limitranges {
		limitrange, ok := obj.(*corev1.LimitRange)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, limitrange.Namespace) {
			continue
		}

		entry := h.createLogEntry(limitrange)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a LimitRangeData from a limitrange
func (h *LimitRangeHandler) createLogEntry(lr *corev1.LimitRange) types.LimitRangeData {
	// Convert limits
	var limits []types.LimitRangeItem
	for _, limitItem := range lr.Spec.Limits {
		item := types.LimitRangeItem{
			Type:                 string(limitItem.Type),
			ResourceType:         "",
			ResourceName:         "",
			Min:                  make(map[string]string),
			Max:                  make(map[string]string),
			Default:              make(map[string]string),
			DefaultRequest:       make(map[string]string),
			MaxLimitRequestRatio: make(map[string]string),
		}

		// Extract resource type and name
		for resourceName := range limitItem.Min {
			item.ResourceType = string(resourceName)
			item.ResourceName = string(resourceName)
			break
		}

		// Convert resource maps
		for key, value := range limitItem.Min {
			item.Min[string(key)] = value.String()
		}
		for key, value := range limitItem.Max {
			item.Max[string(key)] = value.String()
		}
		for key, value := range limitItem.Default {
			item.Default[string(key)] = value.String()
		}
		for key, value := range limitItem.DefaultRequest {
			item.DefaultRequest[string(key)] = value.String()
		}
		for key, value := range limitItem.MaxLimitRequestRatio {
			item.MaxLimitRequestRatio[string(key)] = value.String()
		}

		limits = append(limits, item)
	}

	data := types.LimitRangeData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "limitrange",
					Name:             utils.ExtractName(lr),
					CreatedTimestamp: utils.ExtractCreationTimestamp(lr),
				},
				Namespace: utils.ExtractNamespace(lr),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(lr),
				Annotations: utils.ExtractAnnotations(lr),
			},
		},
		Limits: func() []types.LimitRangeItem {
			if limits == nil {
				return []types.LimitRangeItem{}
			}
			return limits
		}(),
	}

	return data
}
