// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

func TestBaseHandler_SetSelectors_Defaults(t *testing.T) {
	handler := NewBaseHandler(nil)
	handler.SetSelectors(nil, nil)

	obj := &corev1.ConfigMap{}
	if !handler.MatchesSelectors(obj) {
		t.Fatalf("expected object to match default selectors")
	}
}

func TestBaseHandler_MatchesSelectors_Label(t *testing.T) {
	handler := NewBaseHandler(nil)
	selector, err := labels.Parse("app=web")
	if err != nil {
		t.Fatalf("failed to parse label selector: %v", err)
	}
	handler.SetSelectors(selector, fields.Everything())

	obj := &corev1.ConfigMap{}
	obj.Labels = map[string]string{"app": "web"}
	if !handler.MatchesSelectors(obj) {
		t.Fatalf("expected object to match label selector")
	}

	obj.Labels = map[string]string{"app": "api"}
	if handler.MatchesSelectors(obj) {
		t.Fatalf("expected object to not match label selector")
	}
}

func TestBaseHandler_MatchesSelectors_Field(t *testing.T) {
	handler := NewBaseHandler(nil)
	selector, err := fields.ParseSelector("metadata.name=cm-1,metadata.namespace=default")
	if err != nil {
		t.Fatalf("failed to parse field selector: %v", err)
	}
	handler.SetSelectors(labels.Everything(), selector)

	obj := &corev1.ConfigMap{}
	obj.Name = "cm-1"
	obj.Namespace = "default"
	if !handler.MatchesSelectors(obj) {
		t.Fatalf("expected object to match field selector")
	}

	obj.Namespace = "kube-system"
	if handler.MatchesSelectors(obj) {
		t.Fatalf("expected object to not match field selector")
	}
}

func TestBaseHandler_MatchesSelectors_NilObject(t *testing.T) {
	handler := NewBaseHandler(nil)
	handler.SetSelectors(labels.Everything(), fields.Everything())

	if handler.MatchesSelectors(nil) {
		t.Fatalf("expected nil object to not match selectors")
	}
}
