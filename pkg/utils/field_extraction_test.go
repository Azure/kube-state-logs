// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"reflect"
	"testing"
)

var dropAll = CustomFieldOptions{DropContainerEnv: true, DropContainerArgs: true, DropContainerCommand: true}

func TestDropContainerFields(t *testing.T) {
	newContainers := func() []any {
		return []any{
			map[string]any{
				"name":    "main",
				"image":   "busybox",
				"command": []any{"/app"},
				"args":    []any{"--token=secret"},
				"env": []any{
					map[string]any{"name": "API_TOKEN", "value": "secret"},
				},
				"envFrom": []any{
					map[string]any{"secretRef": map[string]any{"name": "s"}},
				},
			},
		}
	}

	tests := []struct {
		name    string
		path    string
		value   any
		options CustomFieldOptions
		want    any
		wantOK  bool
	}{
		{
			name:   "nothing is dropped by default",
			path:   "spec.template.spec.containers",
			value:  newContainers(),
			want:   newContainers(),
			wantOK: true,
		},
		{
			name:   "selected list is kept by default",
			path:   "spec.env",
			value:  []any{map[string]any{"name": "A", "value": "1"}},
			want:   []any{map[string]any{"name": "A", "value": "1"}},
			wantOK: true,
		},
		{
			name:    "scalar is unchanged",
			path:    "spec.replicas",
			value:   int64(3),
			options: dropAll,
			want:    int64(3),
			wantOK:  true,
		},
		{
			name:    "container list drops env, args and command and keeps other fields",
			path:    "spec.template.spec.containers",
			value:   newContainers(),
			options: dropAll,
			want: []any{
				map[string]any{
					"name":  "main",
					"image": "busybox",
					"envFrom": []any{
						map[string]any{"secretRef": map[string]any{"name": "s"}},
					},
				},
			},
			wantOK: true,
		},
		{
			name:    "env, args and command are dropped independently",
			path:    "spec.template.spec.containers",
			value:   newContainers(),
			options: CustomFieldOptions{DropContainerEnv: true},
			want: []any{
				map[string]any{
					"name":    "main",
					"image":   "busybox",
					"command": []any{"/app"},
					"args":    []any{"--token=secret"},
					"envFrom": []any{
						map[string]any{"secretRef": map[string]any{"name": "s"}},
					},
				},
			},
			wantOK: true,
		},
		{
			name: "pod template drops lists from every container type",
			path: "spec.template",
			value: map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": "web"}},
				"spec": map[string]any{
					"initContainers": []any{
						map[string]any{"name": "init", "env": []any{map[string]any{"name": "A", "value": "1"}}},
					},
					"containers": []any{
						map[string]any{"name": "main", "args": []any{"--b=2"}},
					},
					"ephemeralContainers": []any{
						map[string]any{"name": "debug", "command": []any{"sh"}},
					},
				},
			},
			options: dropAll,
			want: map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": "web"}},
				"spec": map[string]any{
					"initContainers":      []any{map[string]any{"name": "init"}},
					"containers":          []any{map[string]any{"name": "main"}},
					"ephemeralContainers": []any{map[string]any{"name": "debug"}},
				},
			},
			wantOK: true,
		},
		{
			name:    "selected env list is omitted when dropped",
			path:    "spec.env",
			value:   []any{map[string]any{"name": "API_TOKEN", "value": "secret"}},
			options: CustomFieldOptions{DropContainerEnv: true},
			wantOK:  false,
		},
		{
			name:    "selected args list is omitted when dropped",
			path:    "spec.args",
			value:   []any{"--token=secret"},
			options: CustomFieldOptions{DropContainerArgs: true},
			wantOK:  false,
		},
		{
			name:    "selected command list is omitted when dropped",
			path:    "spec.command",
			value:   []any{"sh", "-c", "run --token=secret"},
			options: CustomFieldOptions{DropContainerCommand: true},
			wantOK:  false,
		},
		{
			name:    "fields that are not lists are kept",
			path:    "spec",
			value:   map[string]any{"env": "production", "args": "--verbose", "command": "run"},
			options: dropAll,
			want:    map[string]any{"env": "production", "args": "--verbose", "command": "run"},
			wantOK:  true,
		},
		{
			name:    "selected field that is not a list is kept",
			path:    "spec.env",
			value:   "production",
			options: dropAll,
			want:    "production",
			wantOK:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := DropContainerFields(tt.path, tt.value, tt.options)
			if ok != tt.wantOK {
				t.Fatalf("DropContainerFields() ok = %v, want %v", ok, tt.wantOK)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DropContainerFields() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDropContainerFieldsDoesNotModifyInput(t *testing.T) {
	container := map[string]any{
		"name":    "main",
		"env":     []any{map[string]any{"name": "API_TOKEN", "value": "secret"}},
		"args":    []any{"--token=secret"},
		"command": []any{"/app"},
	}
	containers := []any{container}

	DropContainerFields("spec.containers", containers, dropAll)

	for _, field := range []string{"env", "args", "command"} {
		if _, ok := container[field]; !ok {
			t.Fatalf("DropContainerFields() removed %s from the input object", field)
		}
	}
}
