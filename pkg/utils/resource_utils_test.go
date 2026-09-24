// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestResourceValuePointers(t *testing.T) {
	for _, tt := range []struct {
		name    string
		key     corev1.ResourceName
		extract func(corev1.ResourceList) *int64
		value   string
		want    int64
	}{
		{"cpu", corev1.ResourceCPU, ExtractCPUMillicores, "250m", 250},
		{"memory", corev1.ResourceMemory, ExtractMemoryBytes, "128Mi", 128 * 1024 * 1024},
		{"pods", corev1.ResourcePods, ExtractPodsCount, "110", 110},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, missing := range []corev1.ResourceList{nil, {}} {
				if got := tt.extract(missing); got != nil {
					t.Errorf("missing resource = %d, want nil", *got)
				}
			}
			for _, value := range []struct {
				quantity string
				want     int64
			}{{"0", 0}, {tt.value, tt.want}} {
				resources := corev1.ResourceList{tt.key: resource.MustParse(value.quantity)}
				got := tt.extract(resources)
				if got == nil || *got != value.want {
					t.Fatalf("resource %q = %v, want pointer to %d", value.quantity, got, value.want)
				}
				*got = -1
				if next := tt.extract(resources); next == nil || *next != value.want {
					t.Errorf("changing extracted value affected resource: %v", next)
				}
			}
		})
	}
}
