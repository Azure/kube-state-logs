package collector

import (
	"testing"
	"time"

	"github.com/azure/kube-state-logs/pkg/config"
	"k8s.io/client-go/kubernetes/fake"
)

func TestValidateTickerInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		resource string
		expected time.Duration
	}{
		{
			name:     "Valid positive interval",
			interval: 30 * time.Second,
			resource: "pod",
			expected: 30 * time.Second,
		},
		{
			name:     "Zero interval should return default",
			interval: 0,
			resource: "deployment",
			expected: time.Minute,
		},
		{
			name:     "Negative interval should return default",
			interval: -5 * time.Second,
			resource: "service",
			expected: time.Minute,
		},
		{
			name:     "Very small positive interval should be preserved",
			interval: time.Millisecond,
			resource: "node",
			expected: time.Millisecond,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateTickerInterval(tt.interval, tt.resource)
			if result != tt.expected {
				t.Errorf("validateTickerInterval(%v, %s) = %v, want %v", tt.interval, tt.resource, result, tt.expected)
			}
		})
	}
}

func TestValidateTickerInterval_PreventsNewTickerPanic(t *testing.T) {
	// Test that our validation prevents time.NewTicker from panicking
	testCases := []time.Duration{
		0,
		-1 * time.Second,
		-1 * time.Minute,
	}

	for _, interval := range testCases {
		t.Run("interval_"+interval.String(), func(t *testing.T) {
			// This should not panic
			validatedInterval := validateTickerInterval(interval, "test")

			// Verify we can safely create a ticker with the validated interval
			ticker := time.NewTicker(validatedInterval)
			ticker.Stop()

			// Ensure the validated interval is positive
			if validatedInterval <= 0 {
				t.Errorf("validateTickerInterval returned non-positive interval %v", validatedInterval)
			}
		})
	}
}

func TestNew_PodFactoryConfiguration(t *testing.T) {
	tests := []struct {
		name                     string
		node                     string
		trackUnscheduledPods     bool
		expectSeparatePodFactory bool
	}{
		{
			name:                     "No filtering - podFactory same as factory",
			node:                     "",
			trackUnscheduledPods:     false,
			expectSeparatePodFactory: false,
		},
		{
			name:                     "Node filter set - separate podFactory",
			node:                     "worker-node-1",
			trackUnscheduledPods:     false,
			expectSeparatePodFactory: true,
		},
		{
			name:                     "Track unscheduled pods - separate podFactory",
			node:                     "",
			trackUnscheduledPods:     true,
			expectSeparatePodFactory: true,
		},
		{
			name:                     "Both set - node filter takes precedence, separate podFactory",
			node:                     "node-abc",
			trackUnscheduledPods:     true,
			expectSeparatePodFactory: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				LogInterval:          time.Minute,
				Resources:            []string{"pod"},
				Node:                 tt.node,
				TrackUnscheduledPods: tt.trackUnscheduledPods,
			}

			fakeClient := fake.NewSimpleClientset()

			// Use the actual createInformerFactories function
			factory, podFactory := createInformerFactories(fakeClient, cfg)

			// Check if podFactory is the same object as factory (no filtering)
			// or a different object (filtering enabled)
			hasSeparateFactory := factory != podFactory
			if hasSeparateFactory != tt.expectSeparatePodFactory {
				t.Errorf("separate podFactory = %v, want %v", hasSeparateFactory, tt.expectSeparatePodFactory)
			}
		})
	}
}

func TestShouldUsePodFactory(t *testing.T) {
	tests := []struct {
		resourceType        string
		shouldUsePodFactory bool
	}{
		{"pod", true},
		{"container", true},
		{"deployment", false},
		{"node", false},
		{"service", false},
		{"configmap", false},
		{"secret", false},
		{"statefulset", false},
		{"daemonset", false},
		{"replicaset", false},
		{"job", false},
		{"cronjob", false},
	}

	for _, tt := range tests {
		t.Run(tt.resourceType, func(t *testing.T) {
			result := shouldUsePodFactory(tt.resourceType)
			if result != tt.shouldUsePodFactory {
				t.Errorf("shouldUsePodFactory(%q) = %v, want %v", tt.resourceType, result, tt.shouldUsePodFactory)
			}
		})
	}
}

func TestCreateInformerFactories_NamespaceScoping(t *testing.T) {
	tests := []struct {
		name       string
		namespaces []string
	}{
		{
			name:       "All namespaces (empty)",
			namespaces: []string{},
		},
		{
			name:       "Single namespace",
			namespaces: []string{"default"},
		},
		{
			name:       "Multiple namespaces",
			namespaces: []string{"default", "kube-system"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				LogInterval: time.Minute,
				Namespaces:  tt.namespaces,
			}

			fakeClient := fake.NewSimpleClientset()

			// Should not panic
			factory, podFactory := createInformerFactories(fakeClient, cfg)

			// Both should be non-nil
			if factory == nil {
				t.Error("factory is nil")
			}
			if podFactory == nil {
				t.Error("podFactory is nil")
			}

			// Without node filtering, they should be the same
			if factory != podFactory {
				t.Error("without node filtering, factory and podFactory should be the same")
			}
		})
	}
}
