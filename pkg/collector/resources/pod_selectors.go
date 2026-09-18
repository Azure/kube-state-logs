// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

func matchesPodSelectors(pod *corev1.Pod, labelSelector labels.Selector, fieldSelector fields.Selector) bool {
	if pod == nil {
		return false
	}
	if labelSelector != nil && !labelSelector.Matches(labels.Set(pod.Labels)) {
		return false
	}
	if fieldSelector != nil {
		fieldSet := fields.Set{
			"metadata.name":      pod.Name,
			"metadata.namespace": pod.Namespace,
			"spec.nodeName":      pod.Spec.NodeName,
		}
		if !fieldSelector.Matches(fieldSet) {
			return false
		}
	}
	return true
}
