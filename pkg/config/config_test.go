// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package config

import (
	"testing"
	"time"
)

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name                      string
		config                    Config
		expectedLogInterval       time.Duration
		expectedResourceIntervals map[string]time.Duration
	}{
		{
			name: "Valid configuration should not change",
			config: Config{
				LogInterval: 30 * time.Second,
				ResourceConfigs: []ResourceConfig{
					{Name: "pod", Interval: 10 * time.Second},
					{Name: "deployment", Interval: 20 * time.Second},
				},
			},
			expectedLogInterval: 30 * time.Second,
			expectedResourceIntervals: map[string]time.Duration{
				"pod":        10 * time.Second,
				"deployment": 20 * time.Second,
			},
		},
		{
			name: "Zero LogInterval should be fixed",
			config: Config{
				LogInterval: 0,
				ResourceConfigs: []ResourceConfig{
					{Name: "pod", Interval: 10 * time.Second},
				},
			},
			expectedLogInterval: time.Minute,
			expectedResourceIntervals: map[string]time.Duration{
				"pod": 10 * time.Second,
			},
		},
		{
			name: "Negative LogInterval should be fixed",
			config: Config{
				LogInterval: -5 * time.Second,
				ResourceConfigs: []ResourceConfig{
					{Name: "service", Interval: 15 * time.Second},
				},
			},
			expectedLogInterval: time.Minute,
			expectedResourceIntervals: map[string]time.Duration{
				"service": 15 * time.Second,
			},
		},
		{
			name: "Zero resource interval should be fixed",
			config: Config{
				LogInterval: 30 * time.Second,
				ResourceConfigs: []ResourceConfig{
					{Name: "pod", Interval: 0},
					{Name: "deployment", Interval: 20 * time.Second},
				},
			},
			expectedLogInterval: 30 * time.Second,
			expectedResourceIntervals: map[string]time.Duration{
				"pod":        30 * time.Second, // Should use LogInterval
				"deployment": 20 * time.Second,
			},
		},
		{
			name: "Negative resource interval should be fixed",
			config: Config{
				LogInterval: 45 * time.Second,
				ResourceConfigs: []ResourceConfig{
					{Name: "node", Interval: -10 * time.Second},
					{Name: "namespace", Interval: 25 * time.Second},
				},
			},
			expectedLogInterval: 45 * time.Second,
			expectedResourceIntervals: map[string]time.Duration{
				"node":      45 * time.Second, // Should use LogInterval
				"namespace": 25 * time.Second,
			},
		},
		{
			name: "Both LogInterval and resource intervals invalid",
			config: Config{
				LogInterval: 0,
				ResourceConfigs: []ResourceConfig{
					{Name: "pod", Interval: -5 * time.Second},
					{Name: "deployment", Interval: 0},
				},
			},
			expectedLogInterval: time.Minute,
			expectedResourceIntervals: map[string]time.Duration{
				"pod":        time.Minute, // Should use default LogInterval
				"deployment": time.Minute, // Should use default LogInterval
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err != nil {
				t.Errorf("Validate() returned error: %v", err)
			}

			// Check LogInterval
			if tt.config.LogInterval != tt.expectedLogInterval {
				t.Errorf("LogInterval = %v, want %v", tt.config.LogInterval, tt.expectedLogInterval)
			}

			// Check ResourceConfig intervals
			for _, rc := range tt.config.ResourceConfigs {
				expected, exists := tt.expectedResourceIntervals[rc.Name]
				if !exists {
					t.Errorf("Unexpected resource config for %s", rc.Name)
					continue
				}
				if rc.Interval != expected {
					t.Errorf("Resource %s interval = %v, want %v", rc.Name, rc.Interval, expected)
				}
			}
		})
	}
}

func TestParseContainerEnvVars(t *testing.T) {
	cases := []struct {
		in       string
		expected []string
	}{
		{"", []string{}},
		{"GOMAXPROCS", []string{"GOMAXPROCS"}},
		{"GOMAXPROCS,FOO,BAR", []string{"GOMAXPROCS", "FOO", "BAR"}},
		{" GOMAXPROCS , FOO ", []string{"GOMAXPROCS", "FOO"}},
	}
	for _, c := range cases {
		out := ParseContainerEnvVars(c.in)
		if len(out) != len(c.expected) {
			t.Errorf("expected len %d got %d for input %q", len(c.expected), len(out), c.in)
			continue
		}
		for i := range out {
			if out[i] != c.expected[i] {
				t.Errorf("expected %v got %v", c.expected, out)
				break
			}
		}
	}
}

func TestParseResourceConfigs(t *testing.T) {
	defaultInterval := 5 * time.Minute
	tests := []struct {
		name        string
		input       string
		expectErr   bool
		expectCount int
		expectItems []struct {
			name     string
			interval time.Duration
			labels   string
			fields   string
		}
	}{
		{
			name:        "empty input",
			input:       "",
			expectErr:   false,
			expectCount: 0,
			expectItems: nil,
		},
		{
			name:        "labels and fields with escaped comma",
			input:       `configmap:1m:labels=app=foo\,env=prod:fields=metadata.name=my-cm`,
			expectErr:   false,
			expectCount: 1,
			expectItems: []struct {
				name     string
				interval time.Duration
				labels   string
				fields   string
			}{
				{
					name:     "configmap",
					interval: time.Minute,
					labels:   "app=foo,env=prod",
					fields:   "metadata.name=my-cm",
				},
			},
		},
		{
			name:        "labels only without interval",
			input:       `configmap:labels=app=foo`,
			expectErr:   false,
			expectCount: 1,
			expectItems: []struct {
				name     string
				interval time.Duration
				labels   string
				fields   string
			}{
				{
					name:     "configmap",
					interval: defaultInterval,
					labels:   "app=foo",
					fields:   "",
				},
			},
		},
		{
			name:        "label selector with in operator",
			input:       `configmap:1m:labels=environment in (frontend\,backend)`,
			expectErr:   false,
			expectCount: 1,
			expectItems: []struct {
				name     string
				interval time.Duration
				labels   string
				fields   string
			}{
				{
					name:     "configmap",
					interval: time.Minute,
					labels:   "environment in (backend,frontend)",
					fields:   "",
				},
			},
		},
		{
			name:        "label selector with notin operator",
			input:       `configmap:1m:labels=environment notin (frontend\,backend)`,
			expectErr:   false,
			expectCount: 1,
			expectItems: []struct {
				name     string
				interval time.Duration
				labels   string
				fields   string
			}{
				{
					name:     "configmap",
					interval: time.Minute,
					labels:   "environment notin (backend,frontend)",
					fields:   "",
				},
			},
		},
		{
			name:        "two resources",
			input:       `configmap:1m:labels=app=foo\,env=prod,pod:30s:fields=metadata.name=my-pod`,
			expectErr:   false,
			expectCount: 2,
			expectItems: []struct {
				name     string
				interval time.Duration
				labels   string
				fields   string
			}{
				{
					name:     "configmap",
					interval: time.Minute,
					labels:   "app=foo,env=prod",
					fields:   "",
				},
				{
					name:     "pod",
					interval: 30 * time.Second,
					labels:   "",
					fields:   "metadata.name=my-pod",
				},
			},
		},
		{
			name:      "invalid interval",
			input:     "configmap:notaduration",
			expectErr: true,
		},
		{
			name:      "unterminated escape",
			input:     "configmap:1m:labels=app=foo\\",
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			configs, err := ParseResourceConfigs(tt.input, defaultInterval)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(configs) != tt.expectCount {
				t.Fatalf("expected %d configs, got %d", tt.expectCount, len(configs))
			}

			if tt.expectCount == 0 {
				return
			}

			if len(tt.expectItems) == 0 {
				return
			}

			if len(tt.expectItems) != len(configs) {
				t.Fatalf("expected %d config items, got %d", len(tt.expectItems), len(configs))
			}

			for i, expected := range tt.expectItems {
				rc := configs[i]
				if rc.Name != expected.name {
					t.Errorf("expected name %q, got %q", expected.name, rc.Name)
				}
				if rc.Interval != expected.interval {
					t.Errorf("expected interval %v, got %v", expected.interval, rc.Interval)
				}
				if expected.labels != "" && rc.LabelSelector.String() != expected.labels {
					t.Errorf("expected labels %q, got %q", expected.labels, rc.LabelSelector.String())
				}
				if expected.fields != "" && rc.FieldSelector.String() != expected.fields {
					t.Errorf("expected fields %q, got %q", expected.fields, rc.FieldSelector.String())
				}
			}
		})
	}
}
