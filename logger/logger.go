package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Logger writes level and message in logfmt so the logr CLI can parse and filter.
// It can have multiple writers (e.g. stdout + file) and optional key-value fields (inherited by child loggers).
type Logger struct {
	writers []io.Writer
	fields  []string // key, value pairs for logfmt (inherited)
}

// Default logger writing to stdout. Prefer passing a Logger explicitly via run.Options.Logger
// instead of relying on SetDefault, which mutates shared global state.
var Default = &Logger{writers: []io.Writer{os.Stdout}}

// defaultMu guards reads/writes of the Default pointer.
var defaultMu sync.RWMutex

// SetDefault atomically replaces the logger used by the package-level convenience functions
// (Info, Error, Warn, Debug). Prefer injecting a Logger via run.Options.Logger to avoid
// mutating global state.
func SetDefault(l *Logger) {
	if l == nil {
		return
	}
	defaultMu.Lock()
	Default = l
	defaultMu.Unlock()
}

// getDefault returns the current default logger under a read lock.
func getDefault() *Logger {
	defaultMu.RLock()
	l := Default
	defaultMu.RUnlock()
	return l
}

// NewLogger returns a Logger that writes to the given file.
func NewLogger(out *os.File) *Logger {
	if out == nil {
		return &Logger{writers: []io.Writer{os.Stdout}}
	}
	return &Logger{writers: []io.Writer{out}}
}

// NewLoggerWriter returns a Logger that writes to the given writer.
func NewLoggerWriter(w io.Writer) *Logger {
	if w == nil {
		return &Logger{writers: []io.Writer{os.Stdout}}
	}
	return &Logger{writers: []io.Writer{w}}
}

// NewLoggerMulti returns a Logger that writes each log line to all of the given writers.
func NewLoggerMulti(writers ...io.Writer) *Logger {
	var out []io.Writer
	for _, w := range writers {
		if w != nil {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return &Logger{writers: []io.Writer{os.Stdout}}
	}
	return &Logger{writers: out}
}

// WriterConfig describes one log output for config-driven setup (e.g. ~/.logr.yaml).
// Type: "stdout" or "file". For "file", Path must be set.
type WriterConfig struct {
	Type string `mapstructure:"type" yaml:"type"`
	Path string `mapstructure:"path" yaml:"path"`
}

// NewLoggerFromConfig builds a Logger from a list of writer configs (e.g. from config file).
// Type "stdout" uses os.Stdout; "file" opens Path (creates parent dirs if needed).
func NewLoggerFromConfig(configs []WriterConfig) (*Logger, error) {
	var writers []io.Writer
	for _, c := range configs {
		switch strings.ToLower(strings.TrimSpace(c.Type)) {
		case "stdout", "":
			writers = append(writers, os.Stdout)
		case "file":
			p := strings.TrimSpace(c.Path)
			if p == "" {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
				return nil, fmt.Errorf("loggers file %q: %w", p, err)
			}
			f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return nil, fmt.Errorf("loggers file %q: %w", p, err)
			}
			writers = append(writers, f)
		default:
			// ignore unknown type
		}
	}
	if len(writers) == 0 {
		return NewLogger(os.Stdout), nil
	}
	return NewLoggerMulti(writers...), nil
}

// With returns a new Logger that inherits this logger's writers and fields and adds the given key=value to every log line.
// Use for request-scoped or component-scoped loggers, e.g. l.With("request_id", id).
func (l *Logger) With(key, value string) *Logger {
	if l == nil {
		return Default
	}
	next := &Logger{
		writers: l.writers,
		fields:  make([]string, 0, len(l.fields)+2),
	}
	next.fields = append(next.fields, l.fields...)
	next.fields = append(next.fields, key, value)
	return next
}

// Info logs at info level.
func Info(msg string) {
	getDefault().Info(msg)
}

// Infof logs at info level with format.
func Infof(format string, args ...any) {
	getDefault().Infof(format, args...)
}

// Error logs at error level.
func Error(msg string) {
	getDefault().Error(msg)
}

// Errorf logs at error level with format.
func Errorf(format string, args ...any) {
	getDefault().Errorf(format, args...)
}

// Warn logs at warn level.
func Warn(msg string) {
	getDefault().Warn(msg)
}

// Warnf logs at warn level with format.
func Warnf(format string, args ...any) {
	getDefault().Warnf(format, args...)
}

// Debug logs at debug level.
func Debug(msg string) {
	getDefault().Debug(msg)
}

// Debugf logs at debug level with format.
func Debugf(format string, args ...any) {
	getDefault().Debugf(format, args...)
}

// Info logs at info level.
func (l *Logger) Info(msg string) {
	l.emit("info", msg)
}

// Infof logs at info level with format.
func (l *Logger) Infof(format string, args ...any) {
	l.emit("info", fmt.Sprintf(format, args...))
}

// Error logs at error level.
func (l *Logger) Error(msg string) {
	l.emit("error", msg)
}

// Errorf logs at error level with format.
func (l *Logger) Errorf(format string, args ...any) {
	l.emit("error", fmt.Sprintf(format, args...))
}

// Warn logs at warn level.
func (l *Logger) Warn(msg string) {
	l.emit("warn", msg)
}

// Warnf logs at warn level with format.
func (l *Logger) Warnf(format string, args ...any) {
	l.emit("warn", fmt.Sprintf(format, args...))
}

// Debug logs at debug level.
func (l *Logger) Debug(msg string) {
	l.emit("debug", msg)
}

// Debugf logs at debug level with format.
func (l *Logger) Debugf(format string, args ...any) {
	l.emit("debug", fmt.Sprintf(format, args...))
}

func (l *Logger) emit(level, msg string) {
	if l == nil || len(l.writers) == 0 {
		return
	}
	msg = strings.ReplaceAll(msg, `"`, `\"`)
	line := fmt.Sprintf("level=%s msg=%q ts=%s", level, msg, time.Now().UTC().Format(time.RFC3339Nano))
	for i := 0; i < len(l.fields); i += 2 {
		if i+1 >= len(l.fields) {
			break
		}
		k, v := l.fields[i], l.fields[i+1]
		v = strings.ReplaceAll(v, `"`, `\"`)
		v = strings.ReplaceAll(v, "\n", " ")
		line += fmt.Sprintf(" %s=%q", k, v)
	}
	line += "\n"
	for _, w := range l.writers {
		w.Write([]byte(line))
		if f, ok := w.(*os.File); ok {
			f.Sync()
		}
	}
}
