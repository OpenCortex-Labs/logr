package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/Mihir99-mk/logr/internal/source"
)

const (
	FormatPretty = "pretty"
	FormatJSON   = "json"
	FormatLogfmt = "logfmt"
	FormatTable  = "table"
)

type Printer struct {
	format     string
	color      bool
	writer     io.Writer
	tw         *tabwriter.Writer
	headers    bool
	levelColor func(source.LogEntry) string
	dimColor   func(string) string
	svcColor   func(string) string
}

func NewPrinter(format string, useColor bool) *Printer {
	if format == "" {
		format = FormatPretty
	}
	p := &Printer{
		format: format,
		color:  useColor && isTerminal(os.Stdout),
		writer: os.Stdout,
	}
	p.tw = tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	p.setupColors()
	return p
}

func NewPrinterWriter(format string, useColor bool, w io.Writer) *Printer {
	p := &Printer{format: format, color: useColor, writer: w}
	p.tw = tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	p.setupColors()
	return p
}

func (p *Printer) setupColors() {
	if !p.color {
		p.levelColor = func(e source.LogEntry) string { return strings.ToUpper(e.Level) }
		p.dimColor = func(s string) string { return s }
		p.svcColor = func(s string) string { return s }
		return
	}
	p.levelColor = func(e source.LogEntry) string {
		label := strings.ToUpper(e.Level)
		switch e.Level {
		case source.LevelError:
			return color.New(color.FgRed, color.Bold).Sprint(label)
		case source.LevelWarn:
			return color.New(color.FgYellow, color.Bold).Sprint(label)
		case source.LevelDebug:
			return color.New(color.FgMagenta).Sprint(label)
		default:
			return color.New(color.FgCyan).Sprint(label)
		}
	}
	p.dimColor = func(s string) string { return color.New(color.FgHiBlack).Sprint(s) }
	p.svcColor = func(s string) string { return color.New(color.FgGreen, color.Bold).Sprint(s) }
}

func (p *Printer) Print(e source.LogEntry) error {
	switch p.format {
	case FormatJSON:
		return p.printJSON(e)
	case FormatLogfmt:
		return p.printLogfmt(e)
	case FormatTable:
		return p.printTable(e)
	default:
		return p.printPretty(e)
	}
}

func (p *Printer) Flush() {
	if p.tw != nil {
		p.tw.Flush()
	}
}

func (p *Printer) printPretty(e source.LogEntry) error {
	ts := p.dimColor(e.Timestamp.Format("15:04:05.000"))
	svc := p.svcColor(fmt.Sprintf("%-8s", truncate(e.Service, 8)))
	lvl := fmt.Sprintf("%-5s", p.levelColor(e))
	msg := e.Message
	if msg == "" {
		msg = e.Raw
	}
	extras := formatExtras(e.Fields)
	if extras != "" {
		msg = msg + " " + p.dimColor(extras)
	}
	_, err := fmt.Fprintf(p.writer, "%s [%s] %s %s\n", ts, svc, lvl, msg)
	return err
}

func (p *Printer) printJSON(e source.LogEntry) error {
	obj := map[string]any{
		"timestamp": e.Timestamp.UTC().Format(time.RFC3339Nano),
		"service":   e.Service,
		"level":     e.Level,
		"message":   e.Message,
	}
	if len(e.Fields) > 0 {
		obj["fields"] = e.Fields
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(p.writer, "%s\n", b)
	return err
}

func (p *Printer) printLogfmt(e source.LogEntry) error {
	msg := e.Message
	if msg == "" {
		msg = e.Raw
	}
	if strings.ContainsAny(msg, " \t") {
		msg = fmt.Sprintf("%q", msg)
	}
	parts := []string{
		"time=" + e.Timestamp.UTC().Format(time.RFC3339),
		"level=" + e.Level,
		"service=" + e.Service,
		"msg=" + msg,
	}
	for k, v := range e.Fields {
		if isStandardField(k) {
			continue
		}
		vs := fmt.Sprintf("%v", v)
		if strings.ContainsAny(vs, " \t") {
			vs = fmt.Sprintf("%q", vs)
		}
		parts = append(parts, k+"="+vs)
	}
	_, err := fmt.Fprintf(p.writer, "%s\n", strings.Join(parts, " "))
	return err
}

func (p *Printer) printTable(e source.LogEntry) error {
	if !p.headers {
		fmt.Fprintln(p.tw, "TIMESTAMP\tSERVICE\tLEVEL\tMESSAGE")
		fmt.Fprintln(p.tw, "---------\t-------\t-----\t-------")
		p.headers = true
	}
	msg := e.Message
	if msg == "" {
		msg = e.Raw
	}
	_, err := fmt.Fprintf(p.tw, "%s\t%s\t%s\t%s\n",
		e.Timestamp.Format("2006-01-02 15:04:05"),
		truncate(e.Service, 12),
		strings.ToUpper(e.Level),
		truncate(msg, 80),
	)
	if err != nil {
		return err
	}
	return p.tw.Flush()
}

func formatExtras(fields map[string]any) string {
	var parts []string
	for k, v := range fields {
		if !isStandardField(k) {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, " ")
}

func isStandardField(k string) bool {
	switch strings.ToLower(k) {
	case "level", "lvl", "severity", "msg", "message", "text", "log",
		"time", "timestamp", "ts", "t", "service", "svc":
		return true
	}
	return false
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func isTerminal(f *os.File) bool {
	stat, _ := f.Stat()
	return (stat.Mode() & os.ModeCharDevice) != 0
}
