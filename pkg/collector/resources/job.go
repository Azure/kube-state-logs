// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"sync/atomic"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// JobHandler handles collection of job metrics
type JobHandler struct {
	utils.BaseHandler
}

// NewJobHandler creates a new JobHandler
func NewJobHandler(client kubernetes.Interface) *JobHandler {
	return &JobHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the job informer
func (h *JobHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create job informer
	informer := factory.Batch().V1().Jobs().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers job metrics from the cluster (uses cache)
func (h *JobHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all jobs from the cache
	jobs := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range jobs {
		job, ok := obj.(*batchv1.Job)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, job.Namespace) {
			continue
		}

		entry := h.createLogEntry(job)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a JobData from a job
func (h *JobHandler) createLogEntry(job *batchv1.Job) types.JobData {
	// Determine job type
	jobType := "Job"
	if len(job.OwnerReferences) > 0 {
		jobType = job.OwnerReferences[0].Kind
	}

	// Get job conditions in a single loop
	var conditionComplete, conditionFailed *bool
	conditions := make(map[string]*bool)

	for _, condition := range job.Status.Conditions {
		val := utils.ConvertCoreConditionStatus(condition.Status)

		switch condition.Type {
		case batchv1.JobComplete:
			conditionComplete = val
		case batchv1.JobFailed:
			conditionFailed = val
		default:
			// Add unknown conditions to the map
			conditions[string(condition.Type)] = val
		}
	}

	// Get suspend status
	var suspend *bool
	if job.Spec.Suspend != nil {
		suspend = job.Spec.Suspend
	}

	// Get active deadline seconds
	var activeDeadlineSeconds *int64
	if job.Spec.ActiveDeadlineSeconds != nil {
		activeDeadlineSeconds = job.Spec.ActiveDeadlineSeconds
	}

	// Get backoff limit
	backoffLimit := int32(6)
	if job.Spec.BackoffLimit != nil {
		backoffLimit = *job.Spec.BackoffLimit
	}

	createdByKind, createdByName := utils.GetOwnerReferenceInfo(job)

	data := types.JobData{
		ControllerCreatedResourceMetadata: types.ControllerCreatedResourceMetadata{
			NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
				NamespacedMetadata: types.NamespacedMetadata{
					BaseMetadata: types.BaseMetadata{
						Timestamp:         time.Now(),
						ResourceType:      "job",
						Name:              utils.ExtractName(job),
						CreatedTimestamp:  utils.ExtractCreationTimestamp(job),
						EventType:         "snapshot",
						DeletionTimestamp: utils.ExtractDeletionTimestamp(job),
					},
					Namespace: utils.ExtractNamespace(job),
				},
				LabeledMetadata: types.LabeledMetadata{
					Labels:      utils.ExtractLabels(job),
					Annotations: utils.ExtractAnnotations(job),
				},
			},
			ControllerCreatedMetadata: types.ControllerCreatedMetadata{
				CreatedByKind: createdByKind,
				CreatedByName: createdByName,
			},
		},
		ActivePods:            job.Status.Active,
		SucceededPods:         job.Status.Succeeded,
		FailedPods:            job.Status.Failed,
		Completions:           job.Spec.Completions,
		Parallelism:           job.Spec.Parallelism,
		BackoffLimit:          backoffLimit,
		ActiveDeadlineSeconds: activeDeadlineSeconds,
		ConditionComplete:     conditionComplete,
		ConditionFailed:       conditionFailed,
		JobType:               jobType,
		Suspend:               suspend,
		Conditions:            conditions,
	}

	return data
}

// SetupEventHandlers registers informer event handlers for immediate
// logging on resource creation and deletion.
func (h *JobHandler) SetupEventHandlers(logger interfaces.Logger, namespaces []string, hasSynced *atomic.Bool) {
	utils.SetupEventHandlers(h.GetInformer(), func(obj *batchv1.Job, eventType string) any {
		entry := h.createLogEntry(obj)
		entry.EventType = eventType
		return entry
	}, func(obj *batchv1.Job) bool {
		return utils.ShouldIncludeNamespace(namespaces, obj.Namespace)
	}, logger, hasSynced)
}
