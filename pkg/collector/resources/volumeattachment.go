// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"maps"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// VolumeAttachmentHandler handles collection of volumeattachment metrics
type VolumeAttachmentHandler struct {
	utils.BaseHandler
}

// NewVolumeAttachmentHandler creates a new VolumeAttachmentHandler
func NewVolumeAttachmentHandler(client kubernetes.Interface) *VolumeAttachmentHandler {
	return &VolumeAttachmentHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the volumeattachment informer
func (h *VolumeAttachmentHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create volumeattachment informer
	informer := factory.Storage().V1().VolumeAttachments().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers volumeattachment metrics from the cluster (uses cache)
func (h *VolumeAttachmentHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all volumeattachments from the cache
	volumeattachments := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range volumeattachments {
		volumeattachment, ok := obj.(*storagev1.VolumeAttachment)
		if !ok {
			continue
		}

		if !h.MatchesSelectors(volumeattachment) {
			continue
		}

		entry := h.createLogEntry(volumeattachment)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a VolumeAttachmentData from a volumeattachment
func (h *VolumeAttachmentHandler) createLogEntry(va *storagev1.VolumeAttachment) types.VolumeAttachmentData {
	// Get attachment metadata
	attachmentMetadata := make(map[string]string)
	if va.Status.AttachmentMetadata != nil {
		maps.Copy(attachmentMetadata, va.Status.AttachmentMetadata)
	}

	// Get volume name
	volumeName := ""
	if va.Spec.Source.PersistentVolumeName != nil {
		volumeName = *va.Spec.Source.PersistentVolumeName
	}

	// Create data structure
	data := types.VolumeAttachmentData{
		Timestamp:        time.Now(),
		ResourceType:     "volumeattachment",
		Name:             utils.ExtractName(va),
		CreatedTimestamp: utils.ExtractCreationTimestamp(va),
		Labels:           utils.ExtractLabels(va),
		Annotations:      utils.ExtractAnnotations(va),
		Attacher:         va.Spec.Attacher,
		VolumeName:       volumeName,
		NodeName:         va.Spec.NodeName,
		Attached:         va.Status.Attached,
	}

	return data
}
