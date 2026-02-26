// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"sync/atomic"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// PersistentVolumeHandler handles collection of persistentvolume metrics
type PersistentVolumeHandler struct {
	utils.BaseHandler
}

// NewPersistentVolumeHandler creates a new PersistentVolumeHandler
func NewPersistentVolumeHandler(client kubernetes.Interface) *PersistentVolumeHandler {
	return &PersistentVolumeHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the persistentvolume informer
func (h *PersistentVolumeHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create persistentvolume informer
	informer := factory.Core().V1().PersistentVolumes().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers persistentvolume metrics from the cluster (uses cache)
func (h *PersistentVolumeHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all persistentvolumes from the cache
	pvs := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range pvs {
		pv, ok := obj.(*corev1.PersistentVolume)
		if !ok {
			continue
		}

		entry := h.createLogEntry(pv)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a PersistentVolumeData from a persistentvolume
func (h *PersistentVolumeHandler) createLogEntry(pv *corev1.PersistentVolume) types.PersistentVolumeData {
	// Extract access modes
	var accessModes []string
	for _, mode := range pv.Spec.AccessModes {
		accessModes = append(accessModes, string(mode))
	}

	// Extract capacity
	capacityBytes := int64(0)
	if pv.Spec.Capacity != nil {
		if storage, exists := pv.Spec.Capacity[corev1.ResourceStorage]; exists {
			capacityBytes = storage.Value()
		}
	}

	// Extract reclaim policy
	reclaimPolicy := string(pv.Spec.PersistentVolumeReclaimPolicy)

	// Extract status
	status := string(pv.Status.Phase)

	// Extract storage class name
	storageClassName := ""
	if pv.Spec.StorageClassName != "" {
		storageClassName = pv.Spec.StorageClassName
	}

	// Extract volume mode
	volumeMode := string(*pv.Spec.VolumeMode)

	// Extract volume plugin name
	volumePluginName := ""
	if pv.Spec.PersistentVolumeSource.AWSElasticBlockStore != nil {
		volumePluginName = "awsElasticBlockStore"
	} else if pv.Spec.PersistentVolumeSource.AzureDisk != nil {
		volumePluginName = "azureDisk"
	} else if pv.Spec.PersistentVolumeSource.AzureFile != nil {
		volumePluginName = "azureFile"
	} else if pv.Spec.PersistentVolumeSource.CephFS != nil {
		volumePluginName = "cephFS"
	} else if pv.Spec.PersistentVolumeSource.Cinder != nil {
		volumePluginName = "cinder"
	} else if pv.Spec.PersistentVolumeSource.FC != nil {
		volumePluginName = "fc"
	} else if pv.Spec.PersistentVolumeSource.FlexVolume != nil {
		volumePluginName = "flexVolume"
	} else if pv.Spec.PersistentVolumeSource.Flocker != nil {
		volumePluginName = "flocker"
	} else if pv.Spec.PersistentVolumeSource.GCEPersistentDisk != nil {
		volumePluginName = "gcePersistentDisk"
	} else if pv.Spec.PersistentVolumeSource.Glusterfs != nil {
		volumePluginName = "glusterfs"
	} else if pv.Spec.PersistentVolumeSource.HostPath != nil {
		volumePluginName = "hostPath"
	} else if pv.Spec.PersistentVolumeSource.ISCSI != nil {
		volumePluginName = "iscsi"
	} else if pv.Spec.PersistentVolumeSource.Local != nil {
		volumePluginName = "local"
	} else if pv.Spec.PersistentVolumeSource.NFS != nil {
		volumePluginName = "nfs"
	} else if pv.Spec.PersistentVolumeSource.PhotonPersistentDisk != nil {
		volumePluginName = "photonPersistentDisk"
	} else if pv.Spec.PersistentVolumeSource.PortworxVolume != nil {
		volumePluginName = "portworxVolume"
	} else if pv.Spec.PersistentVolumeSource.Quobyte != nil {
		volumePluginName = "quobyte"
	} else if pv.Spec.PersistentVolumeSource.RBD != nil {
		volumePluginName = "rbd"
	} else if pv.Spec.PersistentVolumeSource.ScaleIO != nil {
		volumePluginName = "scaleIO"
	} else if pv.Spec.PersistentVolumeSource.StorageOS != nil {
		volumePluginName = "storageOS"
	} else if pv.Spec.PersistentVolumeSource.VsphereVolume != nil {
		volumePluginName = "vsphereVolume"
	} else {
		volumePluginName = "unknown"
	}

	// Extract persistent volume source
	persistentVolumeSource := volumePluginName

	// Create data structure
	data := types.PersistentVolumeData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:         time.Now(),
				ResourceType:      "persistentvolume",
				Name:              utils.ExtractName(pv),
				CreatedTimestamp:  utils.ExtractCreationTimestamp(pv),
				EventType:         "snapshot",
				DeletionTimestamp: utils.ExtractDeletionTimestamp(pv),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(pv),
				Annotations: utils.ExtractAnnotations(pv),
			},
		},
		CapacityBytes:          capacityBytes,
		AccessModes:            accessModes[0],
		ReclaimPolicy:          reclaimPolicy,
		Status:                 status,
		StorageClassName:       storageClassName,
		VolumeMode:             volumeMode,
		VolumePluginName:       volumePluginName,
		PersistentVolumeSource: persistentVolumeSource,
		IsDefaultClass:         false,
	}

	return data
}

// SetupEventHandlers registers informer event handlers for immediate
// logging on resource creation and deletion.
func (h *PersistentVolumeHandler) SetupEventHandlers(logger interfaces.Logger, namespaces []string, hasSynced *atomic.Bool) {
	utils.SetupEventHandlers(h.GetInformer(), func(obj *corev1.PersistentVolume, eventType string) any {
		entry := h.createLogEntry(obj)
		entry.EventType = eventType
		return entry
	}, func(obj *corev1.PersistentVolume) bool {
		return true
	}, logger, hasSynced)
}
