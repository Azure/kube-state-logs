// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package collector

import (
	"context"
	"testing"
	"time"

	"github.com/azure/kube-state-logs/pkg/collector/resources"
	"github.com/azure/kube-state-logs/pkg/config"
	"github.com/azure/kube-state-logs/pkg/interfaces"
	"github.com/azure/kube-state-logs/pkg/utils"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type pendingPodHandler struct {
	utils.BaseHandler
}

func (h *pendingPodHandler) SetupInformer(factory informers.SharedInformerFactory, logger interfaces.Logger, _ time.Duration) error {
	h.SetupBaseInformer(factory.Core().V1().Pods().Informer(), logger)
	return nil
}

func (pendingPodHandler) Collect(context.Context, []string) ([]any, error) {
	return nil, nil
}

func TestRunReturnsNilOnContextCancellation(t *testing.T) {
	client := fake.NewSimpleClientset()
	factory := informers.NewSharedInformerFactory(client, 0)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())

	collector := &Collector{
		config:      &config.Config{},
		handlers:    make(map[string]interfaces.ResourceHandler),
		crdHandlers: make(map[string]*resources.CRDHandler),
		factory:     factory,
		podFactory:  factory,
		dynFactory:  dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0),
		stopCh:      make(chan struct{}),
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := collector.Run(ctx); err != nil {
		t.Fatalf("Run() returned an error for expected cancellation: %v", err)
	}
	if collector.Ready() {
		t.Fatal("collector is ready after cancellation")
	}
}

func TestRunReturnsNilWhenCanceledDuringInformerSync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client := fake.NewSimpleClientset()
	listStarted := make(chan struct{})
	client.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		close(listStarted)
		<-ctx.Done()
		return true, nil, ctx.Err()
	})

	factory := informers.NewSharedInformerFactory(client, 0)
	dynamicClient := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme())
	collector := &Collector{
		config:      &config.Config{Resources: []string{"pod"}},
		handlers:    map[string]interfaces.ResourceHandler{"pod": &pendingPodHandler{}},
		crdHandlers: make(map[string]*resources.CRDHandler),
		factory:     factory,
		podFactory:  factory,
		dynFactory:  dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0),
		stopCh:      make(chan struct{}),
	}

	done := make(chan error, 1)
	go func() {
		done <- collector.Run(ctx)
	}()

	select {
	case <-listStarted:
	case <-time.After(time.Second):
		t.Fatal("pod informer did not start listing")
	}
	if collector.Ready() {
		t.Fatal("collector is ready while cache sync is pending")
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() returned an error for cancellation during sync: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() did not stop after cancellation")
	}
	if collector.Ready() {
		t.Fatal("collector is ready after cancellation during cache sync")
	}
}

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
