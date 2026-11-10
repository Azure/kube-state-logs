// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"reflect"
	"testing"
)

func TestSanitizeCustomField(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		value   any
		options CustomFieldOptions
		want    any
		wantOK  bool
	}{
		{
			name:   "scalar is unchanged",
			path:   "spec.replicas",
			value:  int64(3),
			want:   int64(3),
			wantOK: true,
		},
		{
			name: "container list drops env, args and command and keeps other fields",
			path: "spec.template.spec.containers",
			value: []any{
				map[string]any{
					"name":    "main",
					"image":   "busybox",
					"command": []any{"/app"},
					"args":    []any{"--token=secret"},
					"env": []any{
						map[string]any{"name": "API_TOKEN", "value": "secret"},
						map[string]any{
							"name": "FROM_SECRET",
							"valueFrom": map[string]any{
								"secretKeyRef": map[string]any{"name": "s", "key": "k"},
							},
						},
					},
					"envFrom": []any{
						map[string]any{"secretRef": map[string]any{"name": "s"}},
					},
				},
			},
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
			name: "pod template drops env from every container type",
			path: "spec.template",
			value: map[string]any{
				"metadata": map[string]any{"labels": map[string]any{"app": "web"}},
				"spec": map[string]any{
					"initContainers": []any{
						map[string]any{"name": "init", "env": []any{map[string]any{"name": "A", "value": "1"}}},
					},
					"containers": []any{
						map[string]any{"name": "main", "env": []any{map[string]any{"name": "B", "value": "2"}}},
					},
					"ephemeralContainers": []any{
						map[string]any{"name": "debug", "env": []any{map[string]any{"name": "C", "value": "3"}}},
					},
				},
			},
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
			name: "last-applied-configuration annotation is removed",
			path: "metadata",
			value: map[string]any{
				"name": "pool",
				"annotations": map[string]any{
					"kubectl.kubernetes.io/last-applied-configuration": `{"spec":{"containers":[{"env":[{"name":"API_TOKEN","value":"secret"}]}]}}`,
					"purpose": "test",
				},
			},
			want: map[string]any{
				"name":        "pool",
				"annotations": map[string]any{"purpose": "test"},
			},
			wantOK: true,
		},
		{
			name:   "selected env list is omitted",
			path:   "spec.env",
			value:  []any{map[string]any{"name": "API_TOKEN", "value": "secret"}},
			wantOK: false,
		},
		{
			name:   "env that is not a list is kept",
			path:   "spec",
			value:  map[string]any{"env": "production"},
			want:   map[string]any{"env": "production"},
			wantOK: true,
		},
		{
			name:   "selected env that is not a list is kept",
			path:   "spec.env",
			value:  "production",
			want:   "production",
			wantOK: true,
		},
		{
			name: "included env is kept while last-applied-configuration is removed",
			path: "spec.template",
			value: map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						"kubectl.kubernetes.io/last-applied-configuration": "{}",
					},
				},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "main", "env": []any{map[string]any{"name": "A", "value": "1"}}},
					},
				},
			},
			options: CustomFieldOptions{IncludeContainerEnv: true},
			want: map[string]any{
				"metadata": map[string]any{"annotations": map[string]any{}},
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "main", "env": []any{map[string]any{"name": "A", "value": "1"}}},
					},
				},
			},
			wantOK: true,
		},
		{
			name:    "selected env list is kept when included",
			path:    "spec.env",
			value:   []any{map[string]any{"name": "A", "value": "1"}},
			options: CustomFieldOptions{IncludeContainerEnv: true},
			want:    []any{map[string]any{"name": "A", "value": "1"}},
			wantOK:  true,
		},
		{
			name:   "selected args list is omitted",
			path:   "spec.args",
			value:  []any{"--token=secret"},
			wantOK: false,
		},
		{
			name:    "selected args list is kept when included",
			path:    "spec.args",
			value:   []any{"--token=secret"},
			options: CustomFieldOptions{IncludeContainerArgs: true},
			want:    []any{"--token=secret"},
			wantOK:  true,
		},
		{
			name:   "selected command list is omitted",
			path:   "spec.command",
			value:  []any{"sh", "-c", "run --token=secret"},
			wantOK: false,
		},
		{
			name:    "selected command list is kept when included",
			path:    "spec.command",
			value:   []any{"/app"},
			options: CustomFieldOptions{IncludeContainerCommand: true},
			want:    []any{"/app"},
			wantOK:  true,
		},
		{
			name:   "args that is not a list is kept",
			path:   "spec",
			value:  map[string]any{"args": "--verbose"},
			want:   map[string]any{"args": "--verbose"},
			wantOK: true,
		},
		{
			name: "env, args and command are removed independently",
			path: "spec.containers",
			value: []any{
				map[string]any{
					"name":    "main",
					"env":     []any{map[string]any{"name": "A", "value": "1"}},
					"args":    []any{"--token=secret"},
					"command": []any{"/app"},
				},
			},
			options: CustomFieldOptions{IncludeContainerArgs: true},
			want: []any{
				map[string]any{"name": "main", "args": []any{"--token=secret"}},
			},
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := SanitizeCustomField(tt.path, tt.value, tt.options)
			if ok != tt.wantOK {
				t.Fatalf("SanitizeCustomField() ok = %v, want %v", ok, tt.wantOK)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("SanitizeCustomField() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestSanitizeCustomFieldDoesNotModifyInput(t *testing.T) {
	container := map[string]any{
		"name":    "main",
		"env":     []any{map[string]any{"name": "API_TOKEN", "value": "secret"}},
		"args":    []any{"--token=secret"},
		"command": []any{"/app"},
	}
	containers := []any{container}

	SanitizeCustomField("spec.containers", containers, CustomFieldOptions{})

	for _, field := range []string{"env", "args", "command"} {
		if _, ok := container[field]; !ok {
			t.Fatalf("SanitizeCustomField() removed %s from the input object", field)
		}
	}
}
