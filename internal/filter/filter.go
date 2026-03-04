package filter

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/Mihir99-mk/logr/internal/source"
)

// Filter holds the current filtering criteria.
type Filter struct {
	Level     string
	Grep      *regexp.Regexp
	Since     time.Time
	Until     time.Time
	ErrorOnly bool

	// Service, when non-empty, shows only entries from this source (e.g. "redis", "postgres").
	Service string

	// LevelExact: when true, Level filter shows only that level; when false (e.g. CLI --level), Level is minimum (level and above).
	LevelExact bool

	// Fields holds key=value pairs that must all match parsed log fields.
	// e.g. {"user_id": "42", "status": "500"}
	Fields map[string]string

	// Sample, when > 1, passes only every Nth matching entry.
	// The counter is internal — callers use Match() normally.
	Sample      int
	sampleCount int
}

// Match returns true if the entry passes all active filters.
// For sampling it maintains an internal counter; the counter only
// advances when all other criteria pass, so every Nth *qualifying* line
// is emitted rather than every Nth raw line.
func (f *Filter) Match(e source.LogEntry) bool {
	if f.Service != "" && !strings.EqualFold(e.Service, f.Service) {
		return false
	}
	if f.ErrorOnly && e.Level != source.LevelError {
		return false
	}
	if f.Level != "" {
		if f.LevelExact {
			if !strings.EqualFold(e.Level, f.Level) {
				return false
			}
		} else if !levelGTE(e.Level, f.Level) {
			return false
		}
	}
	if f.Grep != nil && !f.Grep.MatchString(e.Raw) && !f.Grep.MatchString(e.Message) {
		return false
	}
	if !f.Since.IsZero() && e.Timestamp.Before(f.Since) {
		return false
	}
	if !f.Until.IsZero() && e.Timestamp.After(f.Until) {
		return false
	}
	// Field filtering: every specified key must be present and equal.
	for k, want := range f.Fields {
		got, ok := e.Fields[k]
		if !ok {
			return false
		}
		if fmt.Sprintf("%v", got) != want {
			return false
		}
	}
	// Sampling: advance counter; emit only every Nth match.
	if f.Sample > 1 {
		f.sampleCount++
		if f.sampleCount%f.Sample != 0 {
			return false
		}
	}
	return true
}

// ToggleErrorOnly flips the errors-only mode on/off.
func (f *Filter) ToggleErrorOnly() {
	f.ErrorOnly = !f.ErrorOnly
}

// levelGTE returns true if entryLevel is >= minLevel in severity.
func levelGTE(entryLevel, minLevel string) bool {
	order := map[string]int{
		source.LevelDebug: 0,
		source.LevelInfo:  1,
		source.LevelWarn:  2,
		source.LevelError: 3,
	}
	return order[entryLevel] >= order[minLevel]
}
