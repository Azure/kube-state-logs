// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package utils

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestConditionStatusPointers(t *testing.T) {
	for _, tt := range []struct {
		status string
		want   *bool
	}{
		{status: "True", want: new(true)},
		{status: "False", want: new(false)},
		{status: "Unknown"},
		{status: ""},
		{status: "invalid"},
	} {
		t.Run(tt.status, func(t *testing.T) {
			for _, got := range []*bool{
				ConvertConditionStatus(metav1.ConditionStatus(tt.status)),
				ConvertCoreConditionStatus(corev1.ConditionStatus(tt.status)),
			} {
				if tt.want == nil {
					if got != nil {
						t.Errorf("status %q = %v, want nil", tt.status, *got)
					}
				} else if got == nil || *got != *tt.want {
					t.Errorf("status %q = %v, want pointer to %v", tt.status, got, *tt.want)
				}
			}
		})
	}
}
