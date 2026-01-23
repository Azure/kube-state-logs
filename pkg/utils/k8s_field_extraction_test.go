package utils

import (
	"reflect"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestExtractAnnotations(t *testing.T) {
	tests := []struct {
		name string
		obj  metav1.Object
		want map[string]string
	}{
		{
			name: "nil object",
			obj:  nil,
			want: nil,
		},
		{
			name: "pod with nil annotations",
			obj:  &corev1.Pod{},
			want: nil,
		},
		{
			name: "pod with empty annotations",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			want: nil,
		},
		{
			name: "pod with normal annotations",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"app":     "test-app",
						"version": "1.0.0",
					},
				},
			},
			want: map[string]string{
				"app":     "test-app",
				"version": "1.0.0",
			},
		},
		{
			name: "deployment with last-applied-configuration annotation only",
			obj: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"kubectl.kubernetes.io/last-applied-configuration": `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"test"}}`,
					},
				},
			},
			want: nil,
		},
		{
			name: "pod with last-applied-configuration and other annotations",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"app":     "test-app",
						"version": "1.0.0",
						"kubectl.kubernetes.io/last-applied-configuration": `{"apiVersion":"v1","kind":"Pod","metadata":{"name":"test"}}`,
					},
				},
			},
			want: map[string]string{
				"app":     "test-app",
				"version": "1.0.0",
			},
		},
		{
			name: "deployment with multiple annotations including last-applied-configuration",
			obj: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"app":     "test-app",
						"version": "1.0.0",
						"kubectl.kubernetes.io/last-applied-configuration": `{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"name":"test","annotations":{"app":"test-app","version":"1.0.0"}}}`,
						"prometheus.io/scrape":                             "true",
						"prometheus.io/port":                               "8080",
					},
				},
			},
			want: map[string]string{
				"app":                  "test-app",
				"version":              "1.0.0",
				"prometheus.io/scrape": "true",
				"prometheus.io/port":   "8080",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAnnotations(tt.obj)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractAnnotations() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractName(t *testing.T) {
	tests := []struct {
		name string
		obj  metav1.Object
		want string
	}{
		{
			name: "nil object",
			obj:  nil,
			want: "",
		},
		{
			name: "pod with name",
			obj: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
			},
			want: "test-pod",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractName(tt.obj)
			if got != tt.want {
				t.Errorf("ExtractName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractNamespace(t *testing.T) {
	tests := []struct {
		name string
		obj  metav1.Object
		want string
	}{
		{
			name: "nil object",
			obj:  nil,
			want: "",
		},
		{
			name: "deployment with namespace",
			obj: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-namespace",
				},
			},
			want: "test-namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractNamespace(tt.obj)
			if got != tt.want {
				t.Errorf("ExtractNamespace() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractCreationTimestamp(t *testing.T) {
	fixedTime := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	metaTime := metav1.NewTime(fixedTime)

	tests := []struct {
		name string
		obj  metav1.Object
		want time.Time
	}{
		{
			name: "nil object",
			obj:  nil,
			want: time.Time{},
		},
		{
			name: "deployment with creation timestamp",
			obj: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metaTime,
				},
			},
			want: fixedTime,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractCreationTimestamp(tt.obj)
			if !got.Equal(tt.want) {
				t.Errorf("ExtractCreationTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}
