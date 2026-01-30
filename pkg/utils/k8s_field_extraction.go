// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ExtractCreationTimestamp extracts the creation timestamp from a Kubernetes object
func ExtractCreationTimestamp(obj metav1.Object) time.Time {
	if obj == nil {
		return time.Time{}
	}
	return obj.GetCreationTimestamp().Time
}

// ExtractLabels extracts labels from a Kubernetes object
func ExtractLabels(obj metav1.Object) map[string]string {
	if obj == nil {
		return nil
	}
	return obj.GetLabels()
}

// ExtractAnnotations extracts annotations from a Kubernetes object,
// filtering out noisy annotations like last-applied-configuration
func ExtractAnnotations(obj metav1.Object) map[string]string {
	if obj == nil {
		return nil
	}

	annotations := obj.GetAnnotations()
	if len(annotations) == 0 {
		return nil
	}

	if _, exists := annotations[corev1.LastAppliedConfigAnnotation]; !exists {
		// No filtering needed, return as-is
		return annotations
	}

	// Filter out the kubectl last-applied-configuration annotation
	// as it will contain nearly the full spec.
	filtered := make(map[string]string, len(annotations)-1)
	for key, value := range annotations {
		if key != corev1.LastAppliedConfigAnnotation {
			filtered[key] = value
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return filtered
}

// ExtractName extracts the name from a Kubernetes object
func ExtractName(obj metav1.Object) string {
	if obj == nil {
		return ""
	}
	return obj.GetName()
}

// ExtractNamespace extracts the namespace from a Kubernetes object
func ExtractNamespace(obj metav1.Object) string {
	if obj == nil {
		return ""
	}
	return obj.GetNamespace()
}

// ExtractGeneration extracts the generation from a Kubernetes object
func ExtractGeneration(obj metav1.Object) int64 {
	if obj == nil {
		return 0
	}
	return obj.GetGeneration()
}

// ExtractDeletionTimestamp extracts the deletion timestamp from a Kubernetes object
func ExtractDeletionTimestamp(obj metav1.Object) *time.Time {
	if obj == nil || obj.GetDeletionTimestamp() == nil {
		return nil
	}
	return &obj.GetDeletionTimestamp().Time
}
