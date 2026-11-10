// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
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

// containerEnvField is the container field that lists environment variables.
// Its literal values often hold credentials such as API keys and tokens.
const containerEnvField = "env"

// SanitizeCustomField returns a copy of a value selected by ExtractField
// without the kubectl last-applied-configuration annotation, which holds a copy
// of the whole spec, at any depth. Unless includeContainerEnv is set, every
// container environment list is removed as well: a pod template or container
// spec selected as a custom field would otherwise log each variable's literal
// value. ok is false when the selected field is itself a removed environment
// list. The input is not modified, so values from informer caches are safe to
// pass.
func SanitizeCustomField(path string, value any, includeContainerEnv bool) (result any, ok bool) {
	removeEnv := !includeContainerEnv
	key := path[strings.LastIndex(path, ".")+1:]
	if removeEnv && isContainerEnv(key, value) {
		return nil, false
	}
	return sanitize(value, removeEnv), true
}

func isContainerEnv(key string, value any) bool {
	_, isList := value.([]any)
	return key == containerEnvField && isList
}

func sanitize(value any, removeEnv bool) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if key == corev1.LastAppliedConfigAnnotation || (removeEnv && isContainerEnv(key, child)) {
				continue
			}
			out[key] = sanitize(child, removeEnv)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = sanitize(child, removeEnv)
		}
		return out
	default:
		return value
	}
}
