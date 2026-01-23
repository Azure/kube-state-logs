package collector

import (
	"testing"
	"time"
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
