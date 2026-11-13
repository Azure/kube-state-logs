// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"os"
	"path"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/util/yaml"
)

func TestParseFieldPattern(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    FieldPattern
		wantErr bool
	}{
		{name: "simple path", pattern: "spec.template.spec.containers.env", want: FieldPattern{"spec", "template", "spec", "containers", "env"}},
		{name: "trims spaces", pattern: "  spec.env ", want: FieldPattern{"spec", "env"}},
		{name: "glob and recursive", pattern: "**.*[Pp]assword*", want: FieldPattern{"**", "*[Pp]assword*"}},
		{name: "empty", pattern: "", wantErr: true},
		{name: "empty segment", pattern: "spec..env", wantErr: true},
		{name: "trailing recursive", pattern: "spec.**", wantErr: true},
		{name: "partial recursive", pattern: "spec.a**", wantErr: true},
		{name: "recursive after escaped star", pattern: "spec.\\***", wantErr: true},
		{name: "escaped star before wildcard", pattern: "spec.\\**", want: FieldPattern{"spec", "\\**"}},
		{name: "wildcard before escaped star", pattern: "spec.*\\*", want: FieldPattern{"spec", "*\\*"}},
		{name: "stars in class", pattern: "spec.[**]", want: FieldPattern{"spec", "[**]"}},
		{name: "wildcard around class", pattern: "spec.*[*]*", want: FieldPattern{"spec", "*[*]*"}},
		{name: "bad glob", pattern: "spec.[", wantErr: true},
		{name: "empty class", pattern: "spec.[]", wantErr: true},
		{name: "reversed range", pattern: "spec.[z-a]", wantErr: true},
		{name: "trailing escape", pattern: "spec.a\\", wantErr: true},
		{name: "unescaped dash in class", pattern: "spec.[-a]", wantErr: true},
		{name: "separator character", pattern: "spec:env", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseFieldPattern(tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseFieldPattern(%q) error = %v, wantErr %v", tt.pattern, err, tt.wantErr)
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseFieldPattern(%q) = %v, want %v", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestMatchGlob(t *testing.T) {
	tests := []struct {
		pattern string
		name    string
		want    bool
	}{
		{"*", "", true},
		{"*", "a/b", true},
		{"vault*", "vault.hashicorp.com/agent-inject-secret-db", true},
		{"*secret*", "app.io/client-secret", true},
		{"*/client-*", "app.io/client-secret", true},
		{"app.io?client-secret", "app.io/client-secret", true},
		{"app.io[/]client-secret", "app.io/client-secret", true},
		{"app.io[^a-z]client-secret", "app.io/client-secret", true},
		{"a*b*c", "axxbyyc", true},
		{"a*b*c", "axxbyy", false},
		{"a?c", "ac", false},
		{"[a-c]x", "bx", true},
		{"[^a-c]x", "bx", false},
		{"\\*x", "*x", true},
		{"\\*x", "ax", false},
		{"\\**", "*token", true},
		{"\\**", "token", false},
		{"[**]", "*", true},
		{"[\\]]", "]", true},
		{"日*", "日本", true},
		{"abc", "abcd", false},
	}
	for _, tt := range tests {
		if err := validateGlob(tt.pattern); err != nil {
			t.Fatalf("validateGlob(%q) = %v", tt.pattern, err)
		}
		if got := matchGlob(tt.pattern, tt.name); got != tt.want {
			t.Errorf("matchGlob(%q, %q) = %v, want %v", tt.pattern, tt.name, got, tt.want)
		}
	}
}

// TestMatchGlobAgreesWithPathMatch checks that matchGlob only differs from
// path.Match in treating '/' as an ordinary character.
func TestMatchGlobAgreesWithPathMatch(t *testing.T) {
	patterns := []string{"*", "a*", "*b", "a*b*c", "?", "a?c", "[a-c]*", "[^a-c]*", "\\*", "[\\-]", "*[Pp]assword*"}
	names := []string{"", "a", "b", "abc", "axbxc", "dbPassword", "password", "*", "-", "x"}
	for _, p := range patterns {
		if err := validateGlob(p); err != nil {
			t.Fatalf("validateGlob(%q) = %v", p, err)
		}
		for _, n := range names {
			want, err := path.Match(p, n)
			if err != nil {
				t.Fatalf("path.Match(%q, %q): %v", p, n, err)
			}
			if got := matchGlob(p, n); got != want {
				t.Errorf("matchGlob(%q, %q) = %v, path.Match = %v", p, n, got, want)
			}
		}
	}
}

func TestDropMatchingFieldsSlashKeys(t *testing.T) {
	annotations := map[string]any{
		"vault.hashicorp.com/agent-inject-secret-db": "x",
		"app.io/client-secret":                       "y",
		"plainsecret":                                "z",
		"app.io/name":                                "keep",
	}
	patterns, err := ParseFieldPatterns([]string{"metadata.annotations.vault*", "**.*secret*"})
	if err != nil {
		t.Fatalf("ParseFieldPatterns: %v", err)
	}
	got, keep := DropMatchingFields("metadata", map[string]any{"annotations": annotations}, patterns)
	if !keep {
		t.Fatal("metadata was dropped")
	}
	want := map[string]any{"annotations": map[string]any{"app.io/name": "keep"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v, want %#v", got, want)
	}
	if len(annotations) != 4 {
		t.Error("source annotations were modified")
	}
}

func newTemplate() map[string]any {
	return map[string]any{
		"spec": map[string]any{
			"containers": []any{
				map[string]any{
					"name":    "main",
					"image":   "busybox",
					"env":     []any{map[string]any{"name": "TOKEN", "value": "secret"}},
					"args":    []any{"--key=secret"},
					"command": []any{"run"},
				},
			},
			"initContainers": []any{
				map[string]any{
					"name":    "init",
					"env":     []any{map[string]any{"name": "DB_PASSWORD", "value": "secret"}},
					"command": []any{"init"},
				},
			},
			"dbPassword": "secret",
		},
	}
}

func TestDropMatchingFields(t *testing.T) {
	tests := []struct {
		name      string
		fieldPath string
		patterns  []string
		wantKeep  bool
		want      any
	}{
		{
			name:      "no patterns keeps value",
			fieldPath: "spec.template",
			wantKeep:  true,
			want:      newTemplate(),
		},
		{
			name:      "unrelated pattern keeps value",
			fieldPath: "spec.template",
			patterns:  []string{"status.message", "spec.other.env"},
			wantKeep:  true,
			want:      newTemplate(),
		},
		{
			name:      "pattern matching field drops it",
			fieldPath: "spec.template.spec.containers",
			patterns:  []string{"spec.template.spec.containers"},
			wantKeep:  false,
		},
		{
			name:      "pattern matching parent drops field",
			fieldPath: "spec.template.spec.containers",
			patterns:  []string{"spec.template"},
			wantKeep:  false,
		},
		{
			name:      "wildcard matching field drops it",
			fieldPath: "spec.template.spec.dbPassword",
			patterns:  []string{"**.*Password"},
			wantKeep:  false,
		},
		{
			name:      "nested path through list",
			fieldPath: "spec.template",
			patterns:  []string{"spec.template.spec.containers.env"},
			wantKeep:  true,
			want: map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "main", "image": "busybox", "args": []any{"--key=secret"}, "command": []any{"run"}},
					},
					"initContainers": []any{
						map[string]any{"name": "init", "env": []any{map[string]any{"name": "DB_PASSWORD", "value": "secret"}}, "command": []any{"init"}},
					},
					"dbPassword": "secret",
				},
			},
		},
		{
			name:      "glob segment and recursive pattern",
			fieldPath: "spec.template",
			patterns:  []string{"spec.template.spec.*ontainers.env", "**.command", "**.*Password"},
			wantKeep:  true,
			want: map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "main", "image": "busybox", "args": []any{"--key=secret"}},
					},
					"initContainers": []any{
						map[string]any{"name": "init"},
					},
				},
			},
		},
		{
			name:      "recursive pattern prefix crossing field path",
			fieldPath: "spec.template.spec.containers",
			patterns:  []string{"spec.**.containers.args", "**.env"},
			wantKeep:  true,
			want: []any{
				map[string]any{"name": "main", "image": "busybox", "command": []any{"run"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			patterns, err := ParseFieldPatterns(tt.patterns)
			if err != nil {
				t.Fatalf("ParseFieldPatterns: %v", err)
			}
			original := newTemplate()
			value := ExtractField(map[string]any{"spec": map[string]any{"template": original}}, tt.fieldPath)

			got, keep := DropMatchingFields(tt.fieldPath, value, patterns)
			if keep != tt.wantKeep {
				t.Fatalf("keep = %v, want %v", keep, tt.wantKeep)
			}
			if keep && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v\nwant %#v", got, tt.want)
			}
			if !reflect.DeepEqual(original, newTemplate()) {
				t.Error("source value was modified")
			}
		})
	}
}

func TestDocumentedCRDDropFields(t *testing.T) {
	document, err := os.ReadFile("../../docs/resources.md")
	if err != nil {
		t.Fatal(err)
	}
	_, section, ok := strings.Cut(string(document), "#### Dropping Fields\n")
	if !ok {
		t.Fatal("drop-fields documentation section not found")
	}
	_, section, ok = strings.Cut(section, "```yaml\n")
	if !ok {
		t.Fatal("drop-fields YAML example not found")
	}
	example, _, ok := strings.Cut(section, "\n```")
	if !ok {
		t.Fatal("drop-fields YAML example is not closed")
	}
	var values struct {
		Config struct {
			CRDConfigs []struct {
				DropFields []string `json:"dropFields"`
			} `json:"crdConfigs"`
		} `json:"config"`
	}
	if err := yaml.Unmarshal([]byte(example), &values); err != nil {
		t.Fatalf("parse drop-fields YAML example: %v", err)
	}
	if len(values.Config.CRDConfigs) != 1 {
		t.Fatalf("drop-fields example has %d CRD configurations, want 1", len(values.Config.CRDConfigs))
	}

	container := func(name string) map[string]any {
		return map[string]any{
			"name":    name,
			"image":   "busybox",
			"env":     []any{map[string]any{"name": "TOKEN", "value": "env-secret"}},
			"args":    []any{"--password=args-secret"},
			"command": []any{"command-secret"},
		}
	}
	value := map[string]any{"spec": map[string]any{
		"containers":          []any{container("main")},
		"initContainers":      []any{container("init")},
		"ephemeralContainers": []any{container("debug")},
		"dbPassword":          "password-secret",
	}}
	patterns, err := ParseFieldPatterns(values.Config.CRDConfigs[0].DropFields)
	if err != nil {
		t.Fatal(err)
	}
	got, keep := DropMatchingFields("spec.template", value, patterns)
	want := map[string]any{"spec": map[string]any{
		"containers":          []any{map[string]any{"name": "main", "image": "busybox"}},
		"initContainers":      []any{map[string]any{"name": "init", "image": "busybox"}},
		"ephemeralContainers": []any{map[string]any{"name": "debug", "image": "busybox"}},
	}}
	if !keep || !reflect.DeepEqual(got, want) {
		t.Errorf("filtered value = %#v, keep = %v; want %#v", got, keep, want)
	}
}
