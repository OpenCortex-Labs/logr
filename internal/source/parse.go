package source

import (
	"encoding/json"
	"strings"
	"time"
)

// enrichEntry fills in Message, Level, Fields from a raw log line.
// Auto-detects JSON → logfmt → plain text.
// Shared between Docker, Kubernetes, File, and Stdin sources.
func enrichEntry(entry LogEntry, line string) LogEntry {
	// Try JSON
	if strings.HasPrefix(strings.TrimSpace(line), "{") {
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err == nil {
			entry.Fields = obj
			entry.Message = extractJSONMessage(obj)
			entry.Level = normalizeLevel(extractJSONLevel(obj))
			return entry
		}
	}

	// Try logfmt
	if fields, ok := parseLogfmt(line); ok {
		entry.Fields = fields
		if msg, ok := fields["msg"].(string); ok {
			entry.Message = msg
		} else if msg, ok := fields["message"].(string); ok {
			entry.Message = msg
		} else {
			entry.Message = line
		}
		if lvl, ok := fields["level"].(string); ok {
			entry.Level = normalizeLevel(lvl)
		}
		if ts, ok := fields["ts"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
				entry.Timestamp = t
			} else if t, err := time.Parse(time.RFC3339, ts); err == nil {
				entry.Timestamp = t
			}
		}
		return entry
	}

	// Plain text
	entry.Message = line
	entry.Level = detectLevelFromText(line)
	return entry
}

// parseTimestampPrefix strips a leading RFC3339Nano timestamp from a log line.
// Returns the parsed time and the remaining line content.
// Used by Docker and Kubernetes sources which prepend timestamps.
//
// RFC3339Nano timestamps vary in length (e.g. "2024-01-01T00:00:00Z" is 20 chars,
// nanosecond variants can be up to 35 chars). We scan for the first space that
// follows a valid timestamp rather than assuming a fixed offset.
func parseTimestampPrefix(raw string) (time.Time, string) {
	// Timestamps are at least 20 chars; scan for a space delimiter up to 36 chars in.
	end := len(raw)
	if end > 36 {
		end = 36
	}
	for i := 20; i <= end; i++ {
		if i >= len(raw) {
			break
		}
		if raw[i] == ' ' {
			if t, err := time.Parse(time.RFC3339Nano, raw[:i]); err == nil {
				return t, raw[i+1:]
			}
		}
	}
	return time.Now(), raw
}

func extractJSONMessage(obj map[string]any) string {
	for _, key := range []string{"msg", "message", "text", "log"} {
		if v, ok := obj[key].(string); ok {
			return v
		}
	}
	return ""
}

func extractJSONLevel(obj map[string]any) string {
	for _, key := range []string{"level", "lvl", "severity"} {
		if v, ok := obj[key].(string); ok {
			return v
		}
	}
	return LevelInfo
}

func normalizeLevel(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "err", "error", "fatal", "critical", "crit":
		return LevelError
	case "warn", "warning":
		return LevelWarn
	case "debug", "trace", "verbose":
		return LevelDebug
	default:
		return LevelInfo
	}
}

func detectLevelFromText(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error") ||
		strings.Contains(lower, "fatal") ||
		strings.Contains(lower, "panic"):
		return LevelError
	case strings.Contains(lower, "warn"):
		return LevelWarn
	case strings.Contains(lower, "debug") || strings.Contains(lower, "trace"):
		return LevelDebug
	default:
		return LevelInfo
	}
}

// parseLogfmt parses logfmt key=value pairs. Values may be quoted with double quotes;
// quoted values can contain spaces and commas. Handles \" inside quoted values.
func parseLogfmt(line string) (map[string]any, bool) {
	fields := map[string]any{}
	line = strings.TrimSpace(line)
	found := 0
	for line != "" {
		// Find key= (key is until first =)
		eq := strings.Index(line, "=")
		if eq <= 0 {
			break
		}
		key := strings.TrimSpace(line[:eq])
		rest := strings.TrimSpace(line[eq+1:])
		if key == "" || rest == "" {
			break
		}
		var value string
		if rest[0] == '"' {
			// Quoted value: find closing " (account for \")
			var i int
			for i = 1; i < len(rest); i++ {
				if rest[i] == '\\' && i+1 < len(rest) && rest[i+1] == '"' {
					i++ // skip \"
					continue
				}
				if rest[i] == '"' {
					value = unescapeLogfmtQuoted(rest[1:i])
					i++
					rest = strings.TrimSpace(rest[i:])
					break
				}
			}
			if i >= len(rest) {
				value = unescapeLogfmtQuoted(rest[1:])
				rest = ""
			}
		} else {
			// Unquoted: value is until next space
			sp := strings.IndexAny(rest, " \t")
			if sp < 0 {
				value = rest
				rest = ""
			} else {
				value = rest[:sp]
				rest = strings.TrimSpace(rest[sp:])
			}
		}
		fields[key] = value
		found++
		line = rest
	}
	return fields, found >= 2
}

func unescapeLogfmtQuoted(s string) string {
	return strings.ReplaceAll(s, `\"`, `"`)
}

func errorEntry(service, msg string) LogEntry {
	return LogEntry{
		Timestamp: time.Now(),
		Service:   service,
		Level:     LevelError,
		Message:   msg,
		Fields:    map[string]any{},
	}
}
