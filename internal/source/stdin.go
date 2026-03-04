package source

import (
	"bufio"
	"context"
	"os"
	"strings"
	"time"
)

type StdinSource struct {
	entries chan LogEntry
	cancel  context.CancelFunc
}

func NewStdinSource() *StdinSource {
	return &StdinSource{entries: make(chan LogEntry, 512)}
}

func (s *StdinSource) Name() string { return "stdin" }

func (s *StdinSource) Stream(ctx context.Context) (<-chan LogEntry, error) {
	ctx, s.cancel = context.WithCancel(ctx)
	go s.readStdin(ctx)
	return s.entries, nil
}

func (s *StdinSource) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *StdinSource) readStdin(ctx context.Context) {
	defer close(s.entries)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	service := "stdin"

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line := scanner.Text()
		if line == "" {
			continue
		}

		entry := parseStdinLine(line, &service)
		select {
		case s.entries <- entry:
		case <-ctx.Done():
			return
		}
	}
}

func parseStdinLine(raw string, defaultService *string) LogEntry {
	entry := LogEntry{
		Timestamp: time.Now(),
		Service:   *defaultService,
		Level:     LevelInfo,
		Raw:       raw,
		Fields:    map[string]any{},
	}

	line := raw

	if parts := sternPrefix(raw); parts != nil {
		entry.Service = parts[0]
		*defaultService = parts[0]
		line = parts[1]
	} else if parts := kubectlPrefix(raw); parts != nil {
		entry.Service = parts[0]
		*defaultService = parts[0]
		line = parts[1]
	} else if parts := composePrefix(raw); parts != nil {
		entry.Service = parts[0]
		*defaultService = parts[0]
		line = parts[1]
	}

	return enrichEntry(entry, line)
}

func sternPrefix(line string) []string {
	parts := strings.SplitN(line, " ", 3)
	if len(parts) == 3 && strings.HasSuffix(parts[1], ":") {
		service := parts[1][:len(parts[1])-1]
		if isIdentifier(service) {
			return []string{service, parts[2]}
		}
	}
	return nil
}

func kubectlPrefix(line string) []string {
	if strings.HasPrefix(line, "[") {
		end := strings.Index(line, "]")
		if end > 1 {
			service := strings.TrimPrefix(line[1:end], "pod/")
			service = trimPodSuffix(service)
			return []string{service, strings.TrimSpace(line[end+1:])}
		}
	}
	return nil
}

func composePrefix(line string) []string {
	if idx := strings.Index(line, " | "); idx > 0 {
		service := strings.TrimSpace(line[:idx])
		if i := strings.LastIndex(service, "_"); i > 0 {
			service = service[:i]
		}
		return []string{service, strings.TrimSpace(line[idx+3:])}
	}
	return nil
}

func trimPodSuffix(name string) string {
	parts := strings.Split(name, "-")
	if len(parts) > 2 {
		return strings.Join(parts[:len(parts)-2], "-")
	}
	return name
}

func isIdentifier(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.') {
			return false
		}
	}
	return true
}
