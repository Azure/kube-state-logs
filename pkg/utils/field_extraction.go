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

// RemoveContainerEnv returns a copy of a value selected by ExtractField with
// every container environment list removed, at any depth. A pod template or
// container spec selected as a custom field would otherwise log each variable's
// literal value. The kubectl last-applied-configuration annotation is removed
// as well, because it holds a copy of the whole spec, including those lists.
// ok is false when the selected field is itself an environment list.
// The input is not modified, so values from informer caches are safe to pass.
func RemoveContainerEnv(path string, value any) (result any, ok bool) {
	key := path[strings.LastIndex(path, ".")+1:]
	if isContainerEnv(key, value) {
		return nil, false
	}
	return withoutContainerEnv(value), true
}

func isContainerEnv(key string, value any) bool {
	_, isList := value.([]any)
	return key == containerEnvField && isList
}

func withoutContainerEnv(value any) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if isContainerEnv(key, child) || key == corev1.LastAppliedConfigAnnotation {
				continue
			}
			out[key] = withoutContainerEnv(child)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = withoutContainerEnv(child)
		}
		return out
	default:
		return value
	}
}
