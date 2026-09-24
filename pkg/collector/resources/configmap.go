// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"maps"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// ConfigMapHandler handles collection of configmap metrics
type ConfigMapHandler struct {
	utils.BaseHandler
	includeValues bool
}

// NewConfigMapHandler creates a new ConfigMapHandler
func NewConfigMapHandler(client kubernetes.Interface, includeValues bool) *ConfigMapHandler {
	return &ConfigMapHandler{
		BaseHandler:   utils.NewBaseHandler(client),
		includeValues: includeValues,
	}
}

// SetupInformer sets up the configmap informer
func (h *ConfigMapHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create configmap informer
	informer := factory.Core().V1().ConfigMaps().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers configmap metrics from the cluster (uses cache)
func (h *ConfigMapHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all configmaps from the cache
	configmaps := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range configmaps {
		configmap, ok := obj.(*corev1.ConfigMap)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, configmap.Namespace) {
			continue
		}

		if !h.MatchesSelectors(configmap) {
			continue
		}

		entry := h.createLogEntry(configmap)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a ConfigMapData from a configmap
func (h *ConfigMapHandler) createLogEntry(configmap *corev1.ConfigMap) types.ConfigMapData {
	dataKeys := slices.Collect(maps.Keys(configmap.Data))
	dataKeys = slices.AppendSeq(dataKeys, maps.Keys(configmap.BinaryData))

	var dataValues map[string]string
	if h.includeValues && len(configmap.Data) > 0 {
		dataValues = maps.Clone(configmap.Data)
	}

	data := types.ConfigMapData{
		Timestamp:        time.Now(),
		ResourceType:     "configmap",
		Name:             utils.ExtractName(configmap),
		CreatedTimestamp: utils.ExtractCreationTimestamp(configmap),
		Namespace:        utils.ExtractNamespace(configmap),
		Labels:           utils.ExtractLabels(configmap),
		Annotations:      utils.ExtractAnnotations(configmap),
		DataKeys:         dataKeys,
		Data:             dataValues,
	}

	return data
}
