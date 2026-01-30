// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// CronJobHandler handles collection of cronjob metrics
type CronJobHandler struct {
	utils.BaseHandler
}

// NewCronJobHandler creates a new CronJobHandler
func NewCronJobHandler(client kubernetes.Interface) *CronJobHandler {
	return &CronJobHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the cronjob informer
func (h *CronJobHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create cronjob informer
	informer := factory.Batch().V1().CronJobs().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers cronjob metrics from the cluster (uses cache)
func (h *CronJobHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all cronjobs from the cache
	cronjobs := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range cronjobs {
		cronjob, ok := obj.(*batchv1.CronJob)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, cronjob.Namespace) {
			continue
		}

		entry := h.createLogEntry(cronjob)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a CronJobData from a cronjob
func (h *CronJobHandler) createLogEntry(cronjob *batchv1.CronJob) types.CronJobData {
	concurrencyPolicy := string(cronjob.Spec.ConcurrencyPolicy)

	var suspend *bool
	if cronjob.Spec.Suspend != nil {
		suspend = cronjob.Spec.Suspend
	}

	var successfulJobsHistoryLimit *int32
	var failedJobsHistoryLimit *int32
	if cronjob.Spec.SuccessfulJobsHistoryLimit != nil {
		successfulJobsHistoryLimit = cronjob.Spec.SuccessfulJobsHistoryLimit
	}
	if cronjob.Spec.FailedJobsHistoryLimit != nil {
		failedJobsHistoryLimit = cronjob.Spec.FailedJobsHistoryLimit
	}

	var lastScheduleTime *time.Time
	if cronjob.Status.LastScheduleTime != nil {
		lastScheduleTime = &cronjob.Status.LastScheduleTime.Time
	}

	// Check conditions in a single loop
	conditionActive := len(cronjob.Status.Active) > 0
	conditionActivePtr := &conditionActive

	data := types.CronJobData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:        time.Now(),
					ResourceType:     "cronjob",
					Name:             utils.ExtractName(cronjob),
					CreatedTimestamp: utils.ExtractCreationTimestamp(cronjob),
				},
				Namespace: utils.ExtractNamespace(cronjob),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(cronjob),
				Annotations: utils.ExtractAnnotations(cronjob),
			},
		},
		Schedule:                   cronjob.Spec.Schedule,
		ConcurrencyPolicy:          concurrencyPolicy,
		Suspend:                    suspend,
		SuccessfulJobsHistoryLimit: successfulJobsHistoryLimit,
		FailedJobsHistoryLimit:     failedJobsHistoryLimit,
		ActiveJobsCount:            int32(len(cronjob.Status.Active)),
		LastScheduleTime:           lastScheduleTime,
		NextScheduleTime:           nil,
		ConditionActive:            conditionActivePtr,
	}

	return data
}
