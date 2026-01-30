// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	testutils "github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
)

// createTestLease creates a test Lease with various configurations
func createTestLease(name, namespace string) *coordinationv1.Lease {
	now := metav1.Now()
	nowMicro := metav1.NewMicroTime(now.Time)
	holderIdentity := "test-holder"
	leaseDurationSeconds := int32(15)
	leaseTransitions := int32(1)
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test lease",
			},
			CreationTimestamp: now,
			Generation:        1,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &holderIdentity,
			LeaseDurationSeconds: &leaseDurationSeconds,
			RenewTime:            &nowMicro,
			AcquireTime:          &nowMicro,
			LeaseTransitions:     &leaseTransitions,
		},
	}
	return lease
}

func TestLeaseHandler_Collect(t *testing.T) {
	lease1 := createTestLease("test-lease-1", "default")
	lease2 := createTestLease("test-lease-2", "kube-system")
	client := fake.NewSimpleClientset(lease1, lease2)
	handler := NewLeaseHandler(client)
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
	entries, err = handler.Collect(ctx, []string{"default"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for default namespace, got %d", len(entries))
	}
	leaseData, ok := entries[0].(types.LeaseData)
	if !ok {
		t.Fatalf("Expected LeaseData type, got %T", entries[0])
	}
	if leaseData.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", leaseData.Namespace)
	}
}

func TestLeaseHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewLeaseHandler(client)
	lease := createTestLease("test-lease", "default")
	entry := handler.createLogEntry(lease)
	if entry.ResourceType != "lease" {
		t.Errorf("Expected resource type 'lease', got '%s'", entry.ResourceType)
	}
	if entry.Name != "test-lease" {
		t.Errorf("Expected name 'test-lease', got '%s'", entry.Name)
	}
	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
	if entry.HolderIdentity != "test-holder" {
		t.Errorf("Expected holder identity 'test-holder', got '%s'", entry.HolderIdentity)
	}
	if entry.LeaseDurationSeconds != 15 {
		t.Errorf("Expected lease duration seconds 15, got %d", entry.LeaseDurationSeconds)
	}
	if entry.LeaseTransitions != 1 {
		t.Errorf("Expected lease transitions 1, got %d", entry.LeaseTransitions)
	}
	if entry.RenewTime == nil {
		t.Error("Expected renew time to not be nil")
	}
	if entry.AcquireTime == nil {
		t.Error("Expected acquire time to not be nil")
	}
}

func TestLeaseHandler_Collect_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewLeaseHandler(client)
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
	if len(entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(entries))
	}
}

func TestLeaseHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test leases in different namespaces
	lease1 := createTestLease("test-lease-1", "default")
	lease2 := createTestLease("test-lease-2", "kube-system")
	lease3 := createTestLease("test-lease-3", "monitoring")

	client := fake.NewSimpleClientset(lease1, lease2, lease3)
	handler := NewLeaseHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}

	factory.Start(nil)
	factory.WaitForCacheSync(nil)

	ctx := context.Background()

	// Test multiple namespace filtering
	entries, err := handler.Collect(ctx, []string{"default", "monitoring"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries for default and monitoring namespaces, got %d", len(entries))
	}

	// Verify correct namespaces
	namespaces := make(map[string]bool)
	for _, entry := range entries {
		leaseData, ok := entry.(types.LeaseData)
		if !ok {
			t.Fatalf("Expected LeaseData type, got %T", entry)
		}
		namespaces[leaseData.Namespace] = true
	}

	if !namespaces["default"] {
		t.Error("Expected entry from default namespace")
	}

	if !namespaces["monitoring"] {
		t.Error("Expected entry from monitoring namespace")
	}

	if namespaces["kube-system"] {
		t.Error("Did not expect entry from kube-system namespace")
	}
}
