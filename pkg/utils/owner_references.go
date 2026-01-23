package utils

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetOwnerReferenceInfo extracts the kind and name of the first owner reference from a Kubernetes object
// Returns empty strings if no owner references exist
func GetOwnerReferenceInfo(obj metav1.Object) (kind, name string) {
	if len(obj.GetOwnerReferences()) > 0 {
		ownerRef := obj.GetOwnerReferences()[0]
		return ownerRef.Kind, ownerRef.Name
	}
	return "", ""
}
