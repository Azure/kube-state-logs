// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"slices"
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

// CustomFieldOptions selects container fields to drop from CRD custom fields.
// Container env, args and command lists can hold credentials such as API keys
// and tokens. The zero value drops nothing.
type CustomFieldOptions struct {
	// DropContainerEnv drops container env lists.
	DropContainerEnv bool
	// DropContainerArgs drops container args lists.
	DropContainerArgs bool
	// DropContainerCommand drops container command lists.
	DropContainerCommand bool
}

func (o CustomFieldOptions) droppedLists() []string {
	var dropped []string
	if o.DropContainerEnv {
		dropped = append(dropped, "env")
	}
	if o.DropContainerArgs {
		dropped = append(dropped, "args")
	}
	if o.DropContainerCommand {
		dropped = append(dropped, "command")
	}
	return dropped
}

// DropContainerFields returns a value selected by ExtractField without the
// container env, args or command lists that opts drops, at any depth. A pod
// template or container spec selected as a custom field otherwise logs their
// literal values. ok is false when the selected field is itself a dropped list.
// When opts drops nothing, the value is returned unchanged. Otherwise a copy is
// returned and the input is not modified, so values from informer caches are
// safe to pass.
func DropContainerFields(path string, value any, opts CustomFieldOptions) (result any, ok bool) {
	dropped := opts.droppedLists()
	if len(dropped) == 0 {
		return value, true
	}
	key := path[strings.LastIndex(path, ".")+1:]
	if isDroppedList(dropped, key, value) {
		return nil, false
	}
	return withoutLists(value, dropped), true
}

func isDroppedList(dropped []string, key string, value any) bool {
	_, isList := value.([]any)
	return isList && slices.Contains(dropped, key)
}

func withoutLists(value any, dropped []string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, child := range v {
			if isDroppedList(dropped, key, child) {
				continue
			}
			out[key] = withoutLists(child, dropped)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, child := range v {
			out[i] = withoutLists(child, dropped)
		}
		return out
	default:
		return value
	}
}
