package utils

import (
	"strings"
)

// ExtractField extracts a field from an object using a dot-separated path
// Returns nil if the path doesn't exist or any intermediate step fails
func ExtractField(obj map[string]any, path string) any {
	if obj == nil || path == "" {
		return nil
	}

	parts := strings.Split(path, ".")
	current := obj

	for i, part := range parts {
		if current == nil {
			return nil
		}

		if i == len(parts)-1 {
			// Last part, return the value
			return current[part]
		}

		// Navigate deeper
		if next, ok := current[part].(map[string]any); ok {
			current = next
		} else {
			return nil
		}
	}

	return nil
}
