package utils

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConvertConditionStatus converts a condition status to a three-state boolean pointer
// "True" -> true, "False" -> false, "Unknown" -> nil
func ConvertConditionStatus(status metav1.ConditionStatus) *bool {
	switch status {
	case metav1.ConditionTrue:
		val := true
		return &val
	case metav1.ConditionFalse:
		val := false
		return &val
	case metav1.ConditionUnknown:
		return nil
	default:
		return nil
	}
}

// ConvertCoreConditionStatus converts a corev1 condition status to a three-state boolean pointer
// "True" -> true, "False" -> false, "Unknown" -> nil
func ConvertCoreConditionStatus(status corev1.ConditionStatus) *bool {
	switch status {
	case corev1.ConditionTrue:
		val := true
		return &val
	case corev1.ConditionFalse:
		val := false
		return &val
	case corev1.ConditionUnknown:
		return nil
	default:
		return nil
	}
}
