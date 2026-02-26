// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package collector

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/azure/kube-state-logs/pkg/interfaces"
)

// LoggerImpl handles structured JSON logging.
// It is safe for concurrent use from multiple goroutines.
type LoggerImpl struct {
	mu      sync.Mutex
	encoder *json.Encoder
}

// NewLogger creates a new Logger instance
func NewLogger() interfaces.Logger {
	return &LoggerImpl{
		encoder: json.NewEncoder(os.Stdout),
	}
}

// Log writes a log entry as JSON to stdout
func (l *LoggerImpl) Log(entry any) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.encoder.Encode(entry)
}
