// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"context"
	"time"

	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// ClusterRoleHandler handles collection of clusterrole metrics
type ClusterRoleHandler struct {
	utils.BaseHandler
}

// NewClusterRoleHandler creates a new ClusterRoleHandler
func NewClusterRoleHandler(client kubernetes.Interface) *ClusterRoleHandler {
	return &ClusterRoleHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the clusterrole informer
func (h *ClusterRoleHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create clusterrole informer
	informer := factory.Rbac().V1().ClusterRoles().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers clusterrole metrics from the cluster (uses cache)
func (h *ClusterRoleHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all clusterroles from the cache
	clusterroles := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range clusterroles {
		clusterrole, ok := obj.(*rbacv1.ClusterRole)
		if !ok {
			continue
		}

		if !h.MatchesSelectors(clusterrole) {
			continue
		}

		entry := h.createLogEntry(clusterrole)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a ClusterRoleData from a clusterrole
func (h *ClusterRoleHandler) createLogEntry(role *rbacv1.ClusterRole) types.ClusterRoleData {
	// Convert rules
	// See: https://kubernetes.io/docs/reference/access-authn-authz/rbac/#role-and-clusterrole
	var rules []types.PolicyRule
	for _, rule := range role.Rules {
		policyRule := types.PolicyRule{
			APIGroups:     rule.APIGroups,
			Resources:     rule.Resources,
			ResourceNames: rule.ResourceNames,
			Verbs:         rule.Verbs,
		}
		rules = append(rules, policyRule)
	}

	// Create data structure
	data := types.ClusterRoleData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "clusterrole",
				Name:             utils.ExtractName(role),
				CreatedTimestamp: utils.ExtractCreationTimestamp(role),
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      utils.ExtractLabels(role),
				Annotations: utils.ExtractAnnotations(role),
			},
		},
		Rules: rules,
	}

	return data
}
