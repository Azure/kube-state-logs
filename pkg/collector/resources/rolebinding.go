// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"sync/atomic"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// RoleBindingHandler handles collection of rolebinding metrics
type RoleBindingHandler struct {
	utils.BaseHandler
}

// NewRoleBindingHandler creates a new RoleBindingHandler
func NewRoleBindingHandler(client kubernetes.Interface) *RoleBindingHandler {
	return &RoleBindingHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the rolebinding informer
func (h *RoleBindingHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create rolebinding informer
	informer := factory.Rbac().V1().RoleBindings().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers rolebinding metrics from the cluster (uses cache)
func (h *RoleBindingHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all rolebindings from the cache
	rolebindings := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range rolebindings {
		rolebinding, ok := obj.(*rbacv1.RoleBinding)
		if !ok {
			continue
		}

		if !utils.ShouldIncludeNamespace(namespaces, rolebinding.Namespace) {
			continue
		}

		entry := h.createLogEntry(rolebinding)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a RoleBindingData from a rolebinding
func (h *RoleBindingHandler) createLogEntry(rb *rbacv1.RoleBinding) types.RoleBindingData {
	// Convert role ref
	roleRef := types.RoleRef{
		APIGroup: rb.RoleRef.APIGroup,
		Kind:     rb.RoleRef.Kind,
		Name:     rb.RoleRef.Name,
	}

	// Convert subjects
	var subjects []types.Subject
	for _, subject := range rb.Subjects {
		subj := types.Subject{
			Kind:      subject.Kind,
			Name:      subject.Name,
			Namespace: subject.Namespace,
			APIGroup:  subject.APIGroup,
		}
		subjects = append(subjects, subj)
	}

	// Create data structure
	data := types.RoleBindingData{
		NamespacedLabeledMetadata: types.NamespacedLabeledMetadata{
			NamespacedMetadata: types.NamespacedMetadata{
				BaseMetadata: types.BaseMetadata{
					Timestamp:         time.Now(),
					ResourceType:      "rolebinding",
					Name:              utils.ExtractName(rb),
					CreatedTimestamp:  utils.ExtractCreationTimestamp(rb),
					EventType:         "snapshot",
					DeletionTimestamp: utils.ExtractDeletionTimestamp(rb),
				},
				Namespace: utils.ExtractNamespace(rb),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(rb),
				Annotations: utils.ExtractAnnotations(rb),
			},
		},
		RoleRef:  roleRef,
		Subjects: subjects,
	}

	return data
}

// SetupEventHandlers registers informer event handlers for immediate
// logging on resource creation and deletion.
func (h *RoleBindingHandler) SetupEventHandlers(logger interfaces.Logger, namespaces []string, hasSynced *atomic.Bool) {
	utils.SetupEventHandlers(h.GetInformer(), func(obj *rbacv1.RoleBinding, eventType string) any {
		entry := h.createLogEntry(obj)
		entry.EventType = eventType
		return entry
	}, func(obj *rbacv1.RoleBinding) bool {
		return utils.ShouldIncludeNamespace(namespaces, obj.Namespace)
	}, logger, hasSynced)
}
