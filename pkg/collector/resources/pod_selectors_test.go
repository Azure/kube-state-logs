// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
)

func TestMatchesPodSelectors(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pod-a", Namespace: "team-a", Labels: map[string]string{"app": "api"}},
		Spec:       corev1.PodSpec{NodeName: "node-a"},
	}
	tests := []struct {
		name           string
		labelSelector  labels.Selector
		fieldSelector  fields.Selector
		expectedResult bool
	}{
		{name: "all", labelSelector: labels.Everything(), fieldSelector: fields.Everything(), expectedResult: true},
		{name: "label match", labelSelector: labels.SelectorFromSet(map[string]string{"app": "api"}), fieldSelector: fields.Everything(), expectedResult: true},
		{name: "label mismatch", labelSelector: labels.SelectorFromSet(map[string]string{"app": "worker"}), fieldSelector: fields.Everything()},
		{name: "node match", labelSelector: labels.Everything(), fieldSelector: fields.OneTermEqualSelector("spec.nodeName", "node-a"), expectedResult: true},
		{name: "node mismatch", labelSelector: labels.Everything(), fieldSelector: fields.OneTermEqualSelector("spec.nodeName", "node-b")},
		{name: "metadata match", labelSelector: labels.Everything(), fieldSelector: fields.OneTermEqualSelector("metadata.namespace", "team-a"), expectedResult: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if actual := matchesPodSelectors(pod, tt.labelSelector, tt.fieldSelector); actual != tt.expectedResult {
				t.Fatalf("matchesPodSelectors() = %v, want %v", actual, tt.expectedResult)
			}
		})
	}
}
