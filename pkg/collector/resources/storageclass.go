package resources

import (
	"context"
	"time"

	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// StorageClassHandler handles collection of storageclass metrics
type StorageClassHandler struct {
	utils.BaseHandler
}

// NewStorageClassHandler creates a new StorageClassHandler
func NewStorageClassHandler(client kubernetes.Interface) *StorageClassHandler {
	return &StorageClassHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the storageclass informer
func (h *StorageClassHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create storageclass informer
	informer := factory.Storage().V1().StorageClasses().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers storageclass metrics from the cluster (uses cache)
func (h *StorageClassHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all storageclasses from the cache
	storageclasses := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range storageclasses {
		storageclass, ok := obj.(*storagev1.StorageClass)
		if !ok {
			continue
		}

		entry := h.createLogEntry(storageclass)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a StorageClassData from a storageclass
func (h *StorageClassHandler) createLogEntry(sc *storagev1.StorageClass) types.StorageClassData {
	// Get reclaim policy
	// Default is "Delete" when reclaimPolicy is nil
	// See: https://kubernetes.io/docs/concepts/storage/storage-classes/#reclaim-policy
	reclaimPolicy := ""
	if sc.ReclaimPolicy != nil {
		reclaimPolicy = string(*sc.ReclaimPolicy)
	}

	// Get volume binding mode
	// Default is "Immediate" when volumeBindingMode is nil
	// See: https://kubernetes.io/docs/concepts/storage/storage-classes/#volume-binding-mode
	volumeBindingMode := ""
	if sc.VolumeBindingMode != nil {
		volumeBindingMode = string(*sc.VolumeBindingMode)
	}

	// Get allow volume expansion
	// Default is false when allowVolumeExpansion is nil
	// See: https://kubernetes.io/docs/concepts/storage/storage-classes/#allow-volume-expansion
	allowVolumeExpansion := false
	if sc.AllowVolumeExpansion != nil {
		allowVolumeExpansion = *sc.AllowVolumeExpansion
	}

	// Get parameters
	parameters := make(map[string]string)
	if sc.Parameters != nil {
		for key, value := range sc.Parameters {
			parameters[key] = value
		}
	}

	// Get mount options
	mountOptions := sc.MountOptions

	// Get allowed topologies
	allowedTopologies := make(map[string]any)
	if sc.AllowedTopologies != nil {
		// Convert to map for JSON serialization
		allowedTopologies["allowedTopologies"] = sc.AllowedTopologies
	}

	// Check if this is the default storage class
	isDefaultClass := false
	annotations := utils.ExtractAnnotations(sc)
	if annotations != nil {
		if annotations["storageclass.kubernetes.io/is-default-class"] == "true" {
			isDefaultClass = true
		}
	}

	// Create data structure
	data := types.StorageClassData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "storageclass",
				Name:             utils.ExtractName(sc),
				CreatedTimestamp: utils.ExtractCreationTimestamp(sc),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(sc),
				Annotations: annotations,
			},
		},
		Provisioner:          sc.Provisioner,
		ReclaimPolicy:        reclaimPolicy,
		VolumeBindingMode:    volumeBindingMode,
		AllowVolumeExpansion: allowVolumeExpansion,
		Parameters:           parameters,
		MountOptions:         mountOptions,
		AllowedTopologies:    allowedTopologies,
		IsDefaultClass:       isDefaultClass,
	}

	return data
}
