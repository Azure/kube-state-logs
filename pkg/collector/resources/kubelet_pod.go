// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/kubelet"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// KubeletPodHandler handles collection of pod data from a shared kubelet snapshot.
type KubeletPodHandler struct {
	source            kubelet.SnapshotSource
	labelSelector     labels.Selector
	fieldSelector     fields.Selector
	nodeLabelPromoter nodeLabelPromoter
}

// NewKubeletPodHandler creates a new KubeletPodHandler.
func NewKubeletPodHandler(source kubelet.SnapshotSource, client kubernetes.Interface, nodeName string, promotedNodeLabels ...string) *KubeletPodHandler {
	promoter := newNodeLabelPromoter(promotedNodeLabels)
	promoter.useDirectLookup(client, nodeName)
	return &KubeletPodHandler{
		source:            source,
		labelSelector:     labels.Everything(),
		fieldSelector:     fields.Everything(),
		nodeLabelPromoter: promoter,
	}
}

// SetSelectors configures pod label and field filtering.
func (h *KubeletPodHandler) SetSelectors(labelSelector labels.Selector, fieldSelector fields.Selector) {
	if labelSelector == nil {
		labelSelector = labels.Everything()
	}
	if fieldSelector == nil {
		fieldSelector = fields.Everything()
	}
	h.labelSelector = labelSelector
	h.fieldSelector = fieldSelector
}

// Collect gathers pod data from the kubelet API.
func (h *KubeletPodHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	snapshot, err := h.source.GetSnapshot(ctx, false)
	if err != nil {
		return nil, err
	}

	entries := make([]any, 0, len(snapshot.Pods))
	listTime := time.Now()
	for i := range snapshot.Pods {
		pod := &snapshot.Pods[i]
		if !utils.ShouldIncludeNamespace(namespaces, pod.Namespace) {
			continue
		}
		if !matchesPodSelectors(pod, h.labelSelector, h.fieldSelector) {
			continue
		}

		entry := CreatePodLogEntry(pod, h.nodeLabelPromoter.labelsForNode(pod.Spec.NodeName))
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}
