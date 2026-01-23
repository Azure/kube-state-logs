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

// createTestCronJob creates a test cronjob with various configurations
func createTestCronJob(name, namespace string, schedule string) *batchv1.CronJob {
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"app":     name,
				"version": "v1",
			},
			Annotations: map[string]string{
				"description": "test cronjob",
			},
			CreationTimestamp: metav1.Now(),
			Generation:        1,
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   schedule,
			ConcurrencyPolicy:          batchv1.ForbidConcurrent,
			SuccessfulJobsHistoryLimit: &[]int32{3}[0],
			FailedJobsHistoryLimit:     &[]int32{1}[0],
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": name,
					},
				},
				Spec: batchv1.JobSpec{
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
				},
			},
		},
		Status: batchv1.CronJobStatus{
			Active: []corev1.ObjectReference{
				{
					APIVersion: "batch/v1",
					Kind:       "Job",
					Name:       "test-job-1",
					Namespace:  namespace,
				},
			},
			LastScheduleTime: &metav1.Time{Time: time.Now().Add(-time.Hour)},
		},
	}

	return cronJob
}

func TestNewCronJobHandler(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCronJobHandler(client)

	if handler == nil {
		t.Fatal("Expected handler to be created, got nil")
	}

	// Verify BaseHandler is embedded
	if handler.BaseHandler == (utils.BaseHandler{}) {
		t.Error("Expected BaseHandler to be embedded")
	}
}

func TestCronJobHandler_SetupInformer(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCronJobHandler(client)
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

func TestCronJobHandler_Collect(t *testing.T) {
	// Create test cronjobs
	cronJob1 := createTestCronJob("test-cronjob-1", "default", "*/5 * * * *")
	cronJob2 := createTestCronJob("test-cronjob-2", "kube-system", "0 0 * * *")

	// Create fake client with test cronjobs
	client := fake.NewSimpleClientset(cronJob1, cronJob2)
	handler := NewCronJobHandler(client)
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

	// Test collecting all cronjobs
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

	// Type assert to CronJobData for assertions
	entry, ok := entries[0].(types.CronJobData)
	if !ok {
		t.Fatalf("Expected CronJobData type, got %T", entries[0])
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}
}

func TestCronJobHandler_createLogEntry(t *testing.T) {
	client := fake.NewSimpleClientset()
	handler := NewCronJobHandler(client)
	cronJob := createTestCronJob("test-cronjob", "default", "*/5 * * * *")
	entry := handler.createLogEntry(cronJob)

	if entry.ResourceType != "cronjob" {
		t.Errorf("Expected resource type 'cronjob', got '%s'", entry.ResourceType)
	}

	if entry.Name != "test-cronjob" {
		t.Errorf("Expected name 'test-cronjob', got '%s'", entry.Name)
	}

	if entry.Namespace != "default" {
		t.Errorf("Expected namespace 'default', got '%s'", entry.Namespace)
	}

	// Verify cronjob-specific fields
	if entry.Schedule != "*/5 * * * *" {
		t.Errorf("Expected schedule '*/5 * * * *', got '%s'", entry.Schedule)
	}

	if entry.ConcurrencyPolicy != "Forbid" {
		t.Errorf("Expected concurrency policy 'Forbid', got '%s'", entry.ConcurrencyPolicy)
	}

	if *entry.SuccessfulJobsHistoryLimit != 3 {
		t.Errorf("Expected successful jobs history limit 3, got %d", *entry.SuccessfulJobsHistoryLimit)
	}

	if *entry.FailedJobsHistoryLimit != 1 {
		t.Errorf("Expected failed jobs history limit 1, got %d", *entry.FailedJobsHistoryLimit)
	}

	if entry.ActiveJobsCount != 1 {
		t.Errorf("Expected active jobs count 1, got %d", entry.ActiveJobsCount)
	}

	if entry.LastScheduleTime == nil {
		t.Error("Expected last schedule time to not be nil")
	}

	// Verify conditions
	if entry.ConditionActive == nil || !*entry.ConditionActive {
		t.Error("Expected ConditionActive to be true")
	}

	// Verify metadata
	if entry.Labels["app"] != "test-cronjob" {
		t.Errorf("Expected label 'app' to be 'test-cronjob', got '%s'", entry.Labels["app"])
	}

	if entry.Annotations["description"] != "test cronjob" {
		t.Errorf("Expected annotation 'description' to be 'test cronjob', got '%s'", entry.Annotations["description"])
	}
}

func TestCronJobHandler_Collect_NamespaceFiltering(t *testing.T) {
	// Create test cronjobs in different namespaces
	cronJob1 := createTestCronJob("test-cronjob-1", "default", "*/5 * * * *")
	cronJob2 := createTestCronJob("test-cronjob-2", "kube-system", "0 0 * * *")
	cronJob3 := createTestCronJob("test-cronjob-3", "monitoring", "0 */6 * * *")

	client := fake.NewSimpleClientset(cronJob1, cronJob2, cronJob3)
	handler := NewCronJobHandler(client)
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
		entryData, ok := entry.(types.CronJobData)
		if !ok {
			t.Fatalf("Expected CronJobData type, got %T", entry)
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
