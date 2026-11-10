// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"slices"
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

// CustomFieldOptions selects which container fields are kept in CRD custom
// fields. Container env and args lists often hold credentials such as API keys
// and tokens, so both are removed unless included.
type CustomFieldOptions struct {
	// IncludeContainerEnv keeps container env lists.
	IncludeContainerEnv bool
	// IncludeContainerArgs keeps container args lists.
	IncludeContainerArgs bool
}

func (o CustomFieldOptions) removedLists() []string {
	var removed []string
	if !o.IncludeContainerEnv {
		removed = append(removed, "env")
	}
	if !o.IncludeContainerArgs {
		removed = append(removed, "args")
	}
	return removed
}

// SanitizeCustomField returns a copy of a value selected by ExtractField
// without the kubectl last-applied-configuration annotation, which holds a copy
// of the whole spec, at any depth. Container env and args lists are removed as
// well, at any depth, unless opts includes them: a pod template or container
// spec selected as a custom field would otherwise log their literal values. ok
// is false when the selected field is itself a removed list. The input is not
// modified, so values from informer caches are safe to pass.
func SanitizeCustomField(path string, value any, opts CustomFieldOptions) (result any, ok bool) {
	removed := opts.removedLists()
	key := path[strings.LastIndex(path, ".")+1:]
	if isRemovedList(removed, key, value) {
		return nil, false
	}
	return sanitize(value, removed), true
}

func isRemovedList(removed []string, key string, value any) bool {
	_, isList := value.([]any)
	return isList && slices.Contains(removed, key)
}

func sanitize(value any, removed []string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if key == corev1.LastAppliedConfigAnnotation || isRemovedList(removed, key, child) {
				continue
			}
			out[key] = sanitize(child, removed)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = sanitize(child, removed)
		}
		return out
	default:
		return value
	}
}
