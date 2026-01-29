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

func TestConfig_NodeFilteringFields(t *testing.T) {
	tests := []struct {
		name                 string
		node                 string
		trackUnscheduledPods bool
		wantNode             string
		wantTrackUnscheduled bool
	}{
		{
			name:                 "Empty node filter",
			node:                 "",
			trackUnscheduledPods: false,
			wantNode:             "",
			wantTrackUnscheduled: false,
		},
		{
			name:                 "Node filter set",
			node:                 "worker-node-1",
			trackUnscheduledPods: false,
			wantNode:             "worker-node-1",
			wantTrackUnscheduled: false,
		},
		{
			name:                 "Track unscheduled pods enabled",
			node:                 "",
			trackUnscheduledPods: true,
			wantNode:             "",
			wantTrackUnscheduled: true,
		},
		{
			name:                 "Both set (node takes precedence in practice)",
			node:                 "node-abc",
			trackUnscheduledPods: true,
			wantNode:             "node-abc",
			wantTrackUnscheduled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				LogInterval:          time.Minute,
				Node:                 tt.node,
				TrackUnscheduledPods: tt.trackUnscheduledPods,
			}

			// Validate should not error
			if err := cfg.Validate(); err != nil {
				t.Errorf("Validate() returned error: %v", err)
			}

			// Fields should be preserved
			if cfg.Node != tt.wantNode {
				t.Errorf("Node = %q, want %q", cfg.Node, tt.wantNode)
			}
			if cfg.TrackUnscheduledPods != tt.wantTrackUnscheduled {
				t.Errorf("TrackUnscheduledPods = %v, want %v", cfg.TrackUnscheduledPods, tt.wantTrackUnscheduled)
			}
		})
	}
}
