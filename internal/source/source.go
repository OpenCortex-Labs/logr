package source

import (
	"context"
	"time"
)

const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// LogEntry is the normalized log line from any source.
type LogEntry struct {
	Timestamp time.Time
	Service   string
	Level     string
	Message   string
	Fields    map[string]any
	Raw       string
}

// Source is implemented by docker, k8s, file, stdin.
type Source interface {
	Name() string
	Stream(ctx context.Context) (<-chan LogEntry, error)
	Close() error
}
