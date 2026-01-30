// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	resource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"

	"github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
)

func TestPersistentVolumeHandler(t *testing.T) {
	volumeMode := corev1.PersistentVolumeFilesystem
	pv1 := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pv1",
			Labels:            map[string]string{"env": "prod"},
			Annotations:       map[string]string{"purpose": "test"},
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("10Gi"),
			},
			AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			StorageClassName:              "fast",
			VolumeMode:                    &volumeMode,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/data",
				},
			},
		},
		Status: corev1.PersistentVolumeStatus{
			Phase: corev1.VolumeAvailable,
		},
	}

	pv2 := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "pv2",
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("20Gi"),
			},
			AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadOnlyMany},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimDelete,
			VolumeMode:                    &volumeMode,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				NFS: &corev1.NFSVolumeSource{
					Server: "nfs.example.com",
					Path:   "/exports",
				},
			},
		},
		Status: corev1.PersistentVolumeStatus{
			Phase: corev1.VolumeBound,
		},
	}

	tests := []struct {
		name           string
		pvs            []*corev1.PersistentVolume
		expectedCount  int
		expectedNames  []string
		expectedFields map[string]interface{}
	}{
		{
			name:          "collect all pvs",
			pvs:           []*corev1.PersistentVolume{pv1, pv2},
			expectedCount: 2,
			expectedNames: []string{"pv1", "pv2"},
		},
		{
			name:          "collect pv with hostpath volume",
			pvs:           []*corev1.PersistentVolume{pv1},
			expectedCount: 1,
			expectedNames: []string{"pv1"},
			expectedFields: map[string]interface{}{
				"access_modes":       "ReadWriteOnce",
				"storage_class":      "fast",
				"status":             "Available",
				"volume_plugin_name": "hostPath",
				"capacity_bytes":     int64(10737418240), // 10Gi in bytes
			},
		},
		{
			name:          "collect pv with nfs volume",
			pvs:           []*corev1.PersistentVolume{pv2},
			expectedCount: 1,
			expectedNames: []string{"pv2"},
			expectedFields: map[string]interface{}{
				"access_modes":       "ReadOnlyMany",
				"status":             "Bound",
				"volume_plugin_name": "nfs",
				"capacity_bytes":     int64(21474836480), // 20Gi in bytes
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objects := make([]runtime.Object, len(tt.pvs))
			for i, pv := range tt.pvs {
				objects[i] = pv
			}
			client := fake.NewSimpleClientset(objects...)
			handler := NewPersistentVolumeHandler(client)
			factory := informers.NewSharedInformerFactory(client, time.Hour)
			err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
			if err != nil {
				t.Fatalf("Failed to setup informer: %v", err)
			}
			factory.Start(context.Background().Done())
			if !cache.WaitForCacheSync(context.Background().Done(), handler.GetInformer().HasSynced) {
				t.Fatal("Failed to sync cache")
			}
			entries, err := handler.Collect(context.Background(), []string{})
			if err != nil {
				t.Fatalf("Failed to collect metrics: %v", err)
			}
			if len(entries) != tt.expectedCount {
				t.Errorf("Expected %d entries, got %d", tt.expectedCount, len(entries))
			}
			entryNames := make([]string, len(entries))
			for i, entry := range entries {
				persistentVolumeData, ok := entry.(types.PersistentVolumeData)
				if !ok {
					t.Fatalf("Expected PersistentVolumeData type, got %T", entry)
				}
				entryNames[i] = persistentVolumeData.Name
			}
			for _, expectedName := range tt.expectedNames {
				found := false
				for _, name := range entryNames {
					if name == expectedName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find pv with name %s", expectedName)
				}
			}
			if tt.expectedFields != nil && len(entries) > 0 {
				persistentVolumeData, ok := entries[0].(types.PersistentVolumeData)
				if !ok {
					t.Fatalf("Expected PersistentVolumeData type, got %T", entries[0])
				}
				for field, expectedValue := range tt.expectedFields {
					switch field {
					case "access_modes":
						if persistentVolumeData.AccessModes != expectedValue.(string) {
							t.Errorf("Expected access_modes %s, got %v", expectedValue, persistentVolumeData.AccessModes)
						}
					case "storage_class":
						if persistentVolumeData.StorageClassName != expectedValue.(string) {
							t.Errorf("Expected storage_class %s, got %v", expectedValue, persistentVolumeData.StorageClassName)
						}
					case "status":
						if persistentVolumeData.Status != expectedValue.(string) {
							t.Errorf("Expected status %s, got %v", expectedValue, persistentVolumeData.Status)
						}
					case "volume_plugin_name":
						if persistentVolumeData.VolumePluginName != expectedValue.(string) {
							t.Errorf("Expected volume_plugin_name %s, got %v", expectedValue, persistentVolumeData.VolumePluginName)
						}
					case "capacity_bytes":
						if persistentVolumeData.CapacityBytes != expectedValue.(int64) {
							t.Errorf("Expected capacity_bytes %d, got %d", expectedValue, persistentVolumeData.CapacityBytes)
						}
					}
				}
			}
			for _, entry := range entries {
				persistentVolumeData, ok := entry.(types.PersistentVolumeData)
				if !ok {
					t.Fatalf("Expected PersistentVolumeData type, got %T", entry)
				}
				if persistentVolumeData.ResourceType != "persistentvolume" {
					t.Errorf("Expected resource type 'persistentvolume', got %s", persistentVolumeData.ResourceType)
				}
				if persistentVolumeData.Name == "" {
					t.Error("Entry name should not be empty")
				}
				if persistentVolumeData.CreatedTimestamp.IsZero() {
					t.Error("Created timestamp should not be zero")
				}
				if persistentVolumeData.AccessModes == "" {
					t.Error("access modes should not be empty")
				}
				if persistentVolumeData.Status == "" {
					t.Error("status should not be empty")
				}
			}
		})
	}
}

func TestPersistentVolumeHandler_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewPersistentVolumeHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	factory.Start(context.Background().Done())
	if !cache.WaitForCacheSync(context.Background().Done(), handler.GetInformer().HasSynced) {
		t.Fatal("Failed to sync cache")
	}
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

func TestPersistentVolumeHandler_InvalidObject(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewPersistentVolumeHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	err := handler.SetupInformer(factory, &testutils.MockLogger{}, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}
	invalidObj := &corev1.Pod{}
	handler.GetInformer().GetStore().Add(invalidObj)
	entries, err := handler.Collect(context.Background(), []string{})
	if err != nil {
		t.Fatalf("Failed to collect metrics: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries with invalid object, got %d", len(entries))
	}
}

// createTestPV creates a test PersistentVolume with various configurations
func createTestPV(name string, phase corev1.PersistentVolumePhase) *corev1.PersistentVolume {
	volumeMode := corev1.PersistentVolumeFilesystem
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test persistent volume",
			},
			CreationTimestamp: metav1.Now(),
		},
		Spec: corev1.PersistentVolumeSpec{
			Capacity: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Gi"),
			},
			AccessModes:                   []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain,
			VolumeMode:                    &volumeMode,
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{
					Path: "/data",
				},
			},
		},
		Status: corev1.PersistentVolumeStatus{
			Phase: phase,
		},
	}

	return pv
}

func TestPersistentVolumeHandler_Collect(t *testing.T) {
	pv1 := createTestPV("test-pv-1", corev1.VolumeAvailable)
	pv2 := createTestPV("test-pv-2", corev1.VolumeBound)

	client := fake.NewSimpleClientset(pv1, pv2)
	handler := NewPersistentVolumeHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}

	factory.Start(nil)
	factory.WaitForCacheSync(nil)

	ctx := context.Background()
	entries, err := handler.Collect(ctx, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Type assert to PersistentVolumeData for assertions
	entry, ok := entries[0].(types.PersistentVolumeData)
	if !ok {
		t.Fatalf("Expected PersistentVolumeData type, got %T", entries[0])
	}

	if entry.Name == "" {
		t.Error("Expected name to not be empty")
	}

	if entry.Status == "" {
		t.Error("Expected status to not be empty")
	}
}
