package resources

import (
	"context"
	"time"

	admissionregistrationv1beta1 "k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/types"
	"github.com/azure/kube-state-logs/pkg/utils"
)

// ValidatingAdmissionPolicyHandler handles collection of validatingadmissionpolicy metrics
type ValidatingAdmissionPolicyHandler struct {
	utils.BaseHandler
}

// NewValidatingAdmissionPolicyHandler creates a new ValidatingAdmissionPolicyHandler
func NewValidatingAdmissionPolicyHandler(client kubernetes.Interface) *ValidatingAdmissionPolicyHandler {
	return &ValidatingAdmissionPolicyHandler{
		BaseHandler: utils.NewBaseHandler(client),
	}
}

// SetupInformer sets up the validatingadmissionpolicy informer
func (h *ValidatingAdmissionPolicyHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, resyncPeriod time.Duration) error {
	// Create validatingadmissionpolicy informer
	informer := factory.Admissionregistration().V1beta1().ValidatingAdmissionPolicies().Informer()
	h.SetupBaseInformer(informer, logger)
	return nil
}

// Collect gathers validatingadmissionpolicy metrics from the cluster (uses cache)
func (h *ValidatingAdmissionPolicyHandler) Collect(ctx context.Context, namespaces []string) ([]any, error) {
	var entries []any

	// Get all validatingadmissionpolicies from the cache
	policies := utils.SafeGetStoreList(h.GetInformer())
	listTime := time.Now()

	for _, obj := range policies {
		policy, ok := obj.(*admissionregistrationv1beta1.ValidatingAdmissionPolicy)
		if !ok {
			continue
		}

		entry := h.createLogEntry(policy)
		entry.Timestamp = listTime
		entries = append(entries, entry)
	}

	return entries, nil
}

// createLogEntry creates a ValidatingAdmissionPolicyData from a validatingadmissionpolicy
func (h *ValidatingAdmissionPolicyHandler) createLogEntry(policy *admissionregistrationv1beta1.ValidatingAdmissionPolicy) types.ValidatingAdmissionPolicyData {
	createdTimestamp := utils.ExtractCreationTimestamp(policy)

	failurePolicy := ""
	if policy.Spec.FailurePolicy != nil {
		failurePolicy = string(*policy.Spec.FailurePolicy)
	}

	paramKind := ""
	if policy.Spec.ParamKind != nil {
		paramKind = policy.Spec.ParamKind.Kind
	}

	observedGeneration := int64(0)
	if policy.Status.ObservedGeneration != 0 {
		observedGeneration = policy.Status.ObservedGeneration
	}

	data := types.ValidatingAdmissionPolicyData{
		ClusterScopedMetadata: types.ClusterScopedMetadata{
			BaseMetadata: types.BaseMetadata{
				Timestamp:        time.Now(),
				ResourceType:     "validatingadmissionpolicy",
				Name:             utils.ExtractName(policy),
				CreatedTimestamp: createdTimestamp,
			},
			LabeledMetadata: types.LabeledMetadata{
				Labels:      policy.GetLabels(),
				Annotations: policy.GetAnnotations(),
			},
		},
		FailurePolicy:      failurePolicy,
		MatchConstraints:   []string{},
		Validations:        []string{},
		AuditAnnotations:   []string{},
		MatchConditions:    []string{},
		Variables:          []string{},
		ParamKind:          paramKind,
		ObservedGeneration: observedGeneration,
		TypeChecking:       "",
		ExpressionWarnings: []string{},
	}

	return data
}
