// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"

	testutils "github.com/azure/kube-state-logs/pkg/collector/testutils"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// createTestJob creates a test job with various configurations
func createTestJob(name, namespace string, completions int32, parallelism int32) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test job",
			},
			CreationTimestamp: metav1.Now(),
			Generation:        1,
		},
		Spec: batchv1.JobSpec{
			Completions: &completions,
			Parallelism: &parallelism,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "app",
							Image: "busybox:latest",
							Command: []string{
								"echo",
								"Hello World",
							},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
				},
			},
			BackoffLimit: &[]int32{4}[0],
		},
		Status: batchv1.JobStatus{
			Active:         2,
			Succeeded:      3,
			Failed:         1,
			StartTime:      &metav1.Time{Time: time.Now().Add(-time.Hour)},
			CompletionTime: &metav1.Time{Time: time.Now()},
			Conditions: []batchv1.JobCondition{
				{
					Type:               batchv1.JobComplete,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Now(),
					Reason:             "JobCompleted",
					Message:            "Job completed successfully",
				},
			},
		},
	}

	return job
}

func TestNewJobHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewJobHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestJobHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewJobHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	// Verify informer is set up
	if handler.GetInformer() == nil {
		t.Error("Expected informer to be set up")
	}
}

func TestJobHandler_Collect(t *testing.T) {
	// Create test jobs
	job1 := createTestJob("test-job-1", "default", 5, 2)
	job2 := createTestJob("test-job-2", "kube-system", 3, 1)

	// Create fake client with test jobs
	client := fake.NewSimpleClientset(job1, job2)
	handler := NewJobHandler(client)
	factory := informers.NewSharedInformerFactory(client, time.Hour)
	logger := &testutils.MockLogger{}

	// Setup informer
	err := handler.SetupInformer(factory, logger, time.Hour)
	if err != nil {
		t.Fatalf("Failed to setup informer: %v", err)
	}

	// Start the factory to populate the cache
	factory.Start(nil)
	factory.WaitForCacheSync(nil)

	// Test collecting all jobs
	ctx := context.Background()
	entries, err := handler.Collect(ctx, []string{})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(entries))
	}

	// Test collecting from specific namespace
	entries, err = handler.Collect(ctx, []string{"default"})
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}

	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry for default namespace, got %d", len(entries))
	}

	// Type assert to JobData for assertions
	entry, ok := entries[0].(types.JobData)
	if !ok {
		t.Fatalf("Expected JobData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestJobHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewJobHandler(client)
	job := createTestJob("test-job", "default", 5, 2)
	entry := handler.createLogEntry(job)

	if entry.ResourceType != "job" {
		t.Errorf("Expected resource type 'job', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-job" {
		t.Errorf("Expected name 'test-job', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify job-specific fields
	if *entry.Completions != 5 {
		t.Errorf("Expected completions 5, got %d", *entry.Completions)
	}

	if *entry.Parallelism != 2 {
		t.Errorf("Expected parallelism 2, got %d", *entry.Parallelism)
	}

	if entry.ActivePods != 2 {
		t.Errorf("Expected active pods 2, got %d", entry.ActivePods)
	}

	if entry.SucceededPods != 3 {
		t.Errorf("Expected succeeded pods 3, got %d", entry.SucceededPods)
	}

	if entry.FailedPods != 1 {
		t.Errorf("Expected failed pods 1, got %d", entry.FailedPods)
	}

	if entry.BackoffLimit != 4 {
		t.Errorf("Expected backoff limit 4, got %d", entry.BackoffLimit)
	}

	// Verify conditions
	if entry.ConditionComplete == nil || !*entry.ConditionComplete {
		t.Error("Expected ConditionComplete to be true")
	}

	if entry.ConditionFailed != nil && *entry.ConditionFailed {
		t.Error("Expected ConditionFailed to be false or nil")
	}

	// Verify metadata
	if entry.Labels["app"] != "test-job" {
		t.Errorf("Expected label 'app' to be 'test-job', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test job" {
		t.Errorf("Expected annotation 'description' to be 'test job', got '%s'", entry.Annotations["description"])
	}
}

func TestJobHandler_createLogEntry_WithOwnerReference(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewJobHandler(client)
	job := createTestJob("test-job", "default", 5, 2)
	job.OwnerReferences = []metav1.OwnerReference{
		{
			APIVersion: "batch/v1",
			Kind:       "CronJob",
			Name:       "test-cronjob",
			UID:        "test-uid",
		},
	}
	entry := handler.createLogEntry(job)

	if entry.CreatedByKind != "CronJob" {
		t.Errorf("Expected created by kind 'CronJob', got '%s'", entry.CreatedByKind)
	}

	if entry.CreatedByName != "test-cronjob" {
		t.Errorf("Expected created by name 'test-cronjob', got '%s'", entry.CreatedByName)
	}
}

func TestJobHandler_createLogEntry_NilValues(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewJobHandler(client)

	// Create a job with Suspend and ActiveDeadlineSeconds explicitly set to nil
	job := createTestJob("test-job", "default", 5, 2)
	job.Spec.Suspend = nil
	job.Spec.ActiveDeadlineSeconds = nil

	entry := handler.createLogEntry(job)

	// Should handle nil values gracefully
	if entry.Suspend != nil {
		t.Errorf("Expected Suspend to be nil, got %v", entry.Suspend)
	}

	if entry.ActiveDeadlineSeconds != nil {
		t.Errorf("Expected ActiveDeadlineSeconds to be nil, got %v", entry.ActiveDeadlineSeconds)
	}
}

func TestJobHandler_createLogEntry_WithValues(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewJobHandler(client)

	// Create a job with Suspend and ActiveDeadlineSeconds set to non-nil values
	job := createTestJob("test-job", "default", 5, 2)
	suspend := true
	activeDeadlineSeconds := int64(3600)
	job.Spec.Suspend = &suspend
	job.Spec.ActiveDeadlineSeconds = &activeDeadlineSeconds

	entry := handler.createLogEntry(job)

	if entry.Suspend == nil {
		t.Fatal("Expected Suspend to be non-nil")
	}
	if !*entry.Suspend {
		t.Errorf("Expected Suspend to be true, got %v", *entry.Suspend)
	}

	if entry.ActiveDeadlineSeconds == nil {
		t.Fatal("Expected ActiveDeadlineSeconds to be non-nil")
	}
	if *entry.ActiveDeadlineSeconds != 3600 {
		t.Errorf("Expected ActiveDeadlineSeconds to be 3600, got %d", *entry.ActiveDeadlineSeconds)
	}
}

func TestJobHandler_Collect_EmptyCache(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewJobHandler(client)
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
		t.Fatalf("Expected 0 entries for empty cache, got %d", len(entries))
	}
}

func TestJobHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test jobs in different namespaces
	job1 := createTestJob("test-job-1", "default", 5, 2)
	job2 := createTestJob("test-job-2", "kube-system", 3, 1)
	job3 := createTestJob("test-job-3", "monitoring", 1, 1)

	client := fake.NewSimpleClientset(job1, job2, job3)
	handler := NewJobHandler(client)
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
		entryData, ok := entry.(types.JobData)
		if !ok {
			t.Fatalf("Expected JobData type, got %T", entry)
		}
		namespaces[entryData.Namespace] = true
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
