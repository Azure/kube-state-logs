// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"slices"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
)

func TestStorageClassHandler(t *testing.T) {
	reclaimPolicyDelete := corev1.PersistentVolumeReclaimDelete
	reclaimPolicyRetain := corev1.PersistentVolumeReclaimRetain
	bindingModeImmediate := storagev1.VolumeBindingImmediate
	bindingModeWaitForFirstConsumer := storagev1.VolumeBindingWaitForFirstConsumer
	allowExpansion := true

	sc1 := &storagev1.StorageClass{
		Name:                 "fast-ssd",
		Labels:               map[string]string{"type": "ssd"},
		Annotations:          map[string]string{"purpose": "test"},
		CreationTimestamp:    metav1.Now(),
		Provisioner:          "kubernetes.io/aws-ebs",
		ReclaimPolicy:        &reclaimPolicyDelete,
		VolumeBindingMode:    &bindingModeImmediate,
		AllowVolumeExpansion: &allowExpansion,
		Parameters: map[string]string{
			"type": "gp3",
			"iops": "3000",
		},
		MountOptions: []string{"debug"},
	}

	sc2 := &storagev1.StorageClass{
		Name:                 "slow-hdd",
		CreationTimestamp:    metav1.Now(),
		Provisioner:          "kubernetes.io/azure-disk",
		ReclaimPolicy:        &reclaimPolicyRetain,
		VolumeBindingMode:    &bindingModeWaitForFirstConsumer,
		AllowVolumeExpansion: nil, // Should default to false
		Parameters: map[string]string{
			"storageaccounttype": "Standard_LRS",
		},
	}

	scDefault := &storagev1.StorageClass{
		Name: "default-sc",
		Annotations: map[string]string{
			"storageclass.kubernetes.io/is-default-class": "true",
		},
		CreationTimestamp:    metav1.Now(),
		Provisioner:          "kubernetes.io/aws-ebs",
		ReclaimPolicy:        &reclaimPolicyDelete,
		VolumeBindingMode:    &bindingModeImmediate,
		AllowVolumeExpansion: &allowExpansion,
	}

	scWithTopologies := &storagev1.StorageClass{
		Name:              "topology-sc",
		CreationTimestamp: metav1.Now(),
		Provisioner:       "kubernetes.io/aws-ebs",
		ReclaimPolicy:     &reclaimPolicyDelete,
		VolumeBindingMode: &bindingModeImmediate,
		AllowedTopologies: []corev1.TopologySelectorTerm{
			{
				MatchLabelExpressions: []corev1.TopologySelectorLabelRequirement{
					{
						Key:    "topology.kubernetes.io/zone",
						Values: []string{"us-west-2a", "us-west-2b"},
					},
				},
			},
		},
	}

	tests := []struct {
		name           string
		storageClasses []*storagev1.StorageClass
		expectedCount  int
		expectedNames  []string
		expectedFields map[string]any
	}{
		{
			name:           "collect all storage classes",
			storageClasses: []*storagev1.StorageClass{sc1, sc2},
			expectedCount:  2,
			expectedNames:  []string{"fast-ssd", "slow-hdd"},
		},
		{
			name:           "collect storage class with parameters and mount options",
			storageClasses: []*storagev1.StorageClass{sc1},
			expectedCount:  1,
			expectedNames:  []string{"fast-ssd"},
			expectedFields: map[string]any{
				"provisioner":            "kubernetes.io/aws-ebs",
				"reclaim_policy":         "Delete",
				"volume_binding_mode":    "Immediate",
				"allow_volume_expansion": true,
			},
		},
		{
			name:           "collect default storage class",
			storageClasses: []*storagev1.StorageClass{scDefault},
			expectedCount:  1,
			expectedNames:  []string{"default-sc"},
			expectedFields: map[string]any{
				"is_default_class": true,
			},
		},
		{
			name:           "collect storage class with allowed topologies",
			storageClasses: []*storagev1.StorageClass{scWithTopologies},
			expectedCount:  1,
			expectedNames:  []string{"topology-sc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.storageClasses))
			for i, sc := range tt.storageClasses {
				objects[i] = sc
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewStorageClassHandler(client)
			factory := informers.NewSharedInformerFactory(client, time.Hour)
			err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
			if err != nil {
				t.Fatalf("Failed to setup informer: %v", err)
			}
			factory.Start(t.Context().Done())
			if !cache.WaitForCacheSync(t.Context().Done(), handler.GetInformer().HasSynced) {
				t.Fatal("Failed to sync cache")
			}
			entries, err := handler.Collect(t.Context(), []string{})
			if err != nil {
				t.Fatalf("Failed to collect metrics: %v", err)
			}
			if len(entries) != tt.expectedCount {
				t.Errorf("Expected %d entries, got %d", tt.expectedCount, len(entries))
			}
			entryNames := make([]string, len(entries))
			for i, entry := range entries {
				storageClassData, ok := entry.(types.StorageClassData)
				if !ok {
					t.Fatalf("Expected StorageClassData type, got %T", entry)
				}
				entryNames[i] = storageClassData.Name
			}
			for _, expectedName := range tt.expectedNames {
				found := slices.Contains(entryNames, expectedName)
				if !found {
					t.Errorf("Expected to find storage class with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				storageClassData, ok := entries[0].(types.StorageClassData)
				if !ok {
					t.Fatalf("Expected StorageClassData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "provisioner":
						if storageClassData.Provisioner != expectedValue {
							t.Errorf("Expected provisioner to be %v, got %v", expectedValue, storageClassData.Provisioner)
						}
					case "reclaim_policy":
						if storageClassData.ReclaimPolicy != expectedValue {
							t.Errorf("Expected reclaim_policy to be %v, got %v", expectedValue, storageClassData.ReclaimPolicy)
						}
					case "volume_binding_mode":
						if storageClassData.VolumeBindingMode != expectedValue {
							t.Errorf("Expected volume_binding_mode to be %v, got %v", expectedValue, storageClassData.VolumeBindingMode)
						}
					case "allow_volume_expansion":
						if storageClassData.AllowVolumeExpansion != expectedValue {
							t.Errorf("Expected allow_volume_expansion to be %v, got %v", expectedValue, storageClassData.AllowVolumeExpansion)
						}
					case "is_default_class":
						if storageClassData.IsDefaultClass != expectedValue {
							t.Errorf("Expected is_default_class to be %v, got %v", expectedValue, storageClassData.IsDefaultClass)
						}
					}
				}
			}
		})
	}
}

func TestStorageClassHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewStorageClassHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	factory.Start(t.Context().Done())
	if !cache.WaitForCacheSync(t.Context().Done(), handler.GetInformer().HasSynced) {
		t.Fatal("Failed to sync cache")
	}
	entries, err := handler.Collect(t.Context(), []string{})
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

func TestStorageClassHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewStorageClassHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	factory.Start(t.Context().Done())
	if !cache.WaitForCacheSync(t.Context().Done(), handler.GetInformer().HasSynced) {
		t.Fatal("Failed to sync cache")
	}
	entries, err := handler.Collect(t.Context(), []string{})
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

func createTestStorageClass(name string) *storagev1.StorageClass {
	return &storagev1.StorageClass{
		Name:              name,
		CreationTimestamp: metav1.Now(),
		Provisioner:       "kubernetes.io/aws-ebs",
	}
}

func TestStorageClassHandler_Collect(t *testing.T) {
	// Create test storage classes
	sc1 := createTestStorageClass("test-sc-1")
	sc2 := createTestStorageClass("test-sc-2")

	// Create fake client with test storage classes
	client := fake.NewSimpleClientset(sc1, sc2)
	handler := NewStorageClassHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	// Setup informer
	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}

	// Start the factory to populate the cache
	factory.Start(t.Context().Done())
	factory.WaitForCacheSync(t.Context().Done())

	// Test collecting all storage classes
	ctx := t.Context()
	entries, err := handler.Collect(ctx, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Verify entries are of correct type
	for _, entry := range entries {
		_, ok := entry.(types.StorageClassData)
		if !ok {
			t.Fatalf("Expected StorageClassData type, got %T", entry)
		}
	}
}
