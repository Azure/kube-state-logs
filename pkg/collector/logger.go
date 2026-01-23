package collector

import (
	"encoding/json"
	"os"

	"github.com/azure/kube-state-logs/pkg/interfaces"
)

// LoggerImpl handles structured JSON logging
type LoggerImpl struct {
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
	return l.encoder.Encode(entry)
}
