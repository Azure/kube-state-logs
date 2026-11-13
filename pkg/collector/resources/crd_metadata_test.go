// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"github.com/azure/kube-state-logs/pkg/utils"
)

func TestCRDHandler_DropMetadataFields(t *testing.T) {
	allLabels := map[string]string{"app": "frontend", "private-token": "label-secret"}
	allAnnotations := map[string]string{
		"vault.hashicorp.com/agent-inject-secret-db": "annotation-secret",
		"description": "visible",
	}
	tests := []struct {
		name             string
		dropFields       []string
		omitCustomFields bool
		wantLabels       map[string]string
		wantAnnotations  map[string]string
		wantPresent      []string
		wantAbsent       []string
	}{
		{
			name:            "no patterns preserve metadata",
			wantLabels:      allLabels,
			wantAnnotations: allAnnotations,
			wantPresent:     []string{"label-secret", "annotation-secret"},
		},
		{
			name:            "drop selected labels and annotations",
			dropFields:      []string{"metadata.labels.private*", "metadata.annotations.vault*"},
			wantLabels:      map[string]string{"app": "frontend"},
			wantAnnotations: map[string]string{"description": "visible"},
			wantAbsent:      []string{"label-secret", "annotation-secret"},
		},
		{
			name:            "recursive patterns filter both metadata maps",
			dropFields:      []string{"**.private*", "**.vault*"},
			wantLabels:      map[string]string{"app": "frontend"},
			wantAnnotations: map[string]string{"description": "visible"},
			wantAbsent:      []string{"label-secret", "annotation-secret"},
		},
		{
			name:            "drop labels parent",
			dropFields:      []string{"metadata.labels"},
			wantAnnotations: allAnnotations,
			wantPresent:     []string{"annotation-secret"},
			wantAbsent:      []string{"label-secret"},
		},
		{
			name:        "drop every annotation key",
			dropFields:  []string{"metadata.annotations.*"},
			wantLabels:  allLabels,
			wantPresent: []string{"label-secret"},
			wantAbsent:  []string{"annotation-secret", "visible"},
		},
		{
			name:        "drop metadata parent",
			dropFields:  []string{"metadata"},
			wantPresent: []string{"test-workload"},
			wantAbsent:  []string{"label-secret", "annotation-secret"},
		},
		{
			name:             "filter emitted annotations without custom fields",
			dropFields:       []string{"metadata.annotations.vault*"},
			omitCustomFields: true,
			wantLabels:       allLabels,
			wantAnnotations:  map[string]string{"description": "visible"},
			wantPresent:      []string{"label-secret"},
			wantAbsent:       []string{"annotation-secret"},
		},
		{
			name:            "unrelated patterns preserve metadata",
			dropFields:      []string{"spec.password"},
			wantLabels:      allLabels,
			wantAnnotations: allAnnotations,
			wantPresent:     []string{"label-secret", "annotation-secret"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &unstructured.Unstructured{Object: map[string]any{
				"apiVersion": "example.com/v1",
				"kind":       "Workload",
				"metadata": map[string]any{
					"name":      "test-workload",
					"namespace": "default",
					"labels": map[string]any{
						"app":           "frontend",
						"private-token": "label-secret",
					},
					"annotations": map[string]any{
						"vault.hashicorp.com/agent-inject-secret-db": "annotation-secret",
						"description": "visible",
					},
				},
			}}
			original := obj.DeepCopy()
			patterns, err := utils.ParseFieldPatterns(tt.dropFields)
			if err != nil {
				t.Fatal(err)
			}
			var customFields []string
			if !tt.omitCustomFields {
				customFields = []string{"metadata", "metadata.labels", "metadata.annotations"}
			}
			handler := NewCRDHandler(nil, schema.GroupVersionResource{}, "workload", customFields, patterns)
			entry := handler.createLogEntry(obj)
			if !reflect.DeepEqual(entry.Labels, tt.wantLabels) {
				t.Errorf("labels = %#v, want %#v", entry.Labels, tt.wantLabels)
			}
			if !reflect.DeepEqual(entry.Annotations, tt.wantAnnotations) {
				t.Errorf("annotations = %#v, want %#v", entry.Annotations, tt.wantAnnotations)
			}
			output, err := json.Marshal(entry)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range tt.wantPresent {
				if !strings.Contains(string(output), value) {
					t.Errorf("output does not contain %q: %s", value, output)
				}
			}

			for _, value := range tt.wantAbsent {
				if strings.Contains(string(output), value) {
					t.Errorf("output contains excluded %q: %s", value, output)
				}
			}
			if !reflect.DeepEqual(obj, original) {
				t.Error("source object was modified")
			}
		})
	}
}

func TestCRDHandler_DropMetadataFieldsPreservesAnnotationExclusions(t *testing.T) {
	obj := &unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"annotations": map[string]any{
			"kubectl.kubernetes.io/last-applied-configuration": "excluded",
			"description": "visible",
		}},
	}}
	patterns, err := utils.ParseFieldPatterns([]string{"spec.password"})
	if err != nil {
		t.Fatal(err)
	}
	handler := NewCRDHandler(nil, schema.GroupVersionResource{}, "workload", nil, patterns)
	entry := handler.createLogEntry(obj)
	want := map[string]string{"description": "visible"}
	if !reflect.DeepEqual(entry.Annotations, want) {
		t.Errorf("annotations = %#v, want %#v", entry.Annotations, want)
	}
}
