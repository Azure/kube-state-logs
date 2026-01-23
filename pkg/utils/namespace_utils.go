package utils

import "slices"

// ShouldIncludeNamespace checks if a namespace should be included based on the provided namespace filter
// If namespaces is empty, all namespaces are included
// If namespaces is not empty, only namespaces in the list are included
func ShouldIncludeNamespace(namespaces []string, namespace string) bool {
	return len(namespaces) == 0 || slices.Contains(namespaces, namespace)
}
