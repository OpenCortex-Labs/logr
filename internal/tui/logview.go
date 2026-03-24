package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/OpenCortex-Labs/logr/internal/filter"
	"github.com/OpenCortex-Labs/logr/internal/source"
)

const maxLines = 5000

// ── Level styles ──────────────────────────────────────────────────────────────

var (
	styleError = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
	styleWarn  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FFB86C"))
	styleDebug = lipgloss.NewStyle().Foreground(lipgloss.Color("#BD93F9"))
	styleInfo  = lipgloss.NewStyle().Foreground(lipgloss.Color("#8BE9FD"))
	styleDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
)

// ── Service color palette ─────────────────────────────────────────────────────

var serviceColorPalette = []lipgloss.Color{
	"#50FA7B", "#FF79C6", "#A4FFBC", "#F1FA8C",
	"#CF9FFF", "#FF6E6E", "#6EFFCE", "#FFFFA5",
	"#FF92DF", "#69FF47", "#C0E0FF", "#FFB86C",
}

type serviceColorMap map[string]lipgloss.Color

func (sc serviceColorMap) colorFor(name string) lipgloss.Color {
	if c, ok := sc[name]; ok {
		return c
	}
	idx := len(sc) % len(serviceColorPalette)
	c := serviceColorPalette[idx]
	sc[name] = c
	return c
}

// ── Detail panel styles ───────────────────────────────────────────────────────

var (
	detailBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("#5C5FE4")).
				Padding(0, 1)
	detailKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8BE9FD")).Bold(true)
	detailValStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F8F8F2"))
	detailHdrStyle    = lipgloss.NewStyle().Background(lipgloss.Color("#44475A")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	selectedLineStyle = lipgloss.NewStyle().Background(lipgloss.Color("#313453"))
)

const detailPanelHeight = 10

// ── logviewModel ─────────────────────────────────────────────────────────────

type logviewModel struct {
	viewport   viewport.Model
	allEntries []source.LogEntry
	filtered   []source.LogEntry
	rendered   []string
	atBottom   bool
	totalCount int
	errorCount int
	width      int // logview's own width (= terminal width − sidebar width)
	height     int

	svcColors   serviceColorMap
	cursor      int
	showDetail  bool
	detailEntry source.LogEntry

	paused      bool
	pauseBuffer []source.LogEntry
	pausedCount int

	// dirty marks that rendered[] has new entries appended since last
	// viewport.SetContent call. refreshContent() is the only place that
	// clears this flag. AddEntry sets it and defers the actual viewport
	// update to the next View() call via the batchFlush path in app.go.
	dirty bool
}

func newLogviewModel() *logviewModel {
	return &logviewModel{
		viewport:  viewport.New(0, 0),
		atBottom:  true,
		cursor:    -1,
		svcColors: make(serviceColorMap),
	}
}

// ── Size ──────────────────────────────────────────────────────────────────────

// SetSize is called by relayout() whenever the terminal is resized or the
// sidebar is toggled. w is already the logview-only width (terminal − sidebar).
func (m *logviewModel) SetSize(w, h int) {
	if w == m.width && h == m.height {
		return
	}
	m.width = w
	m.height = h
	m.viewport.Width = w
	m.viewport.Height = m.viewportHeight()

	// Re-render all entries at the new width (word-wrap changes).
	m.reRenderAll()
	m.refreshContent()
}

func (m *logviewModel) viewportHeight() int {
	h := m.height
	if m.showDetail {
		h -= detailPanelHeight + 1
	}
	if h < 1 {
		h = 1
	}
	return h
}

// reRenderAll rebuilds rendered[] from filtered[] at the current width.
// Called when width changes so word-wrap is recalculated.
func (m *logviewModel) reRenderAll() {
	m.rendered = make([]string, len(m.filtered))
	for i, e := range m.filtered {
		m.rendered[i] = m.renderEntry(e)
	}
}

// ── Pause / Resume ────────────────────────────────────────────────────────────

func (m *logviewModel) TogglePause() {
	m.paused = !m.paused
	if !m.paused {
		buf := m.pauseBuffer
		m.pauseBuffer = nil
		m.pausedCount = 0
		for _, e := range buf {
			m.addEntryInternal(e)
		}
		m.refreshContent()
	}
}

// ── Entry ingestion ───────────────────────────────────────────────────────────

// AddEntry receives one entry from the fan-in channel.
// It does NOT call refreshContent() directly — that is deferred to
// FlushPending(), which app.go calls once per batchMsg after all entries
// in the batch have been appended. This eliminates the O(n²) viewport
// re-render that caused scroll lag.
func (m *logviewModel) AddEntry(e source.LogEntry, f *filter.Filter) {
	m.totalCount++
	if e.Level == source.LevelError {
		m.errorCount++
	}
	if m.paused {
		m.pauseBuffer = append(m.pauseBuffer, e)
		m.pausedCount++
		return
	}
	if f.Match(e) {
		m.addEntryInternal(e)
		m.dirty = true
	} else {
		m.svcColors.colorFor(e.Service)
		m.allEntries = append(m.allEntries, e)
	}
}

// FlushPending pushes accumulated rendered lines into the viewport.
// Must be called by app.go after processing a full batchMsg.
func (m *logviewModel) FlushPending() {
	if m.dirty {
		m.refreshContent()
		m.dirty = false
	}
}

func (m *logviewModel) addEntryInternal(e source.LogEntry) {
	m.svcColors.colorFor(e.Service)

	if len(m.allEntries) >= maxLines {
		drop := maxLines / 10
		m.allEntries = m.allEntries[drop:]
		m.filtered = m.filtered[:0]
		m.rendered = m.rendered[:0]
		for _, old := range m.allEntries {
			m.filtered = append(m.filtered, old)
			m.rendered = append(m.rendered, m.renderEntry(old))
		}
		if m.cursor >= len(m.rendered) {
			m.cursor = len(m.rendered) - 1
		}
	}
	m.allEntries = append(m.allEntries, e)
	m.filtered = append(m.filtered, e)
	m.rendered = append(m.rendered, m.renderEntry(e))
}

func (m *logviewModel) refilter(f *filter.Filter) {
	m.filtered = m.filtered[:0]
	m.rendered = m.rendered[:0]
	for _, e := range m.allEntries {
		if f.Match(e) {
			m.filtered = append(m.filtered, e)
			m.rendered = append(m.rendered, m.renderEntry(e))
		}
	}
	if m.cursor >= len(m.rendered) {
		m.cursor = len(m.rendered) - 1
	}
	m.refreshContent()
}

// ── Content refresh ───────────────────────────────────────────────────────────

// refreshContent rebuilds the full viewport content string.
// Expensive — called only at batch boundaries, refilter, resize, or cursor move.
func (m *logviewModel) refreshContent() {
	lines := make([]string, len(m.rendered))
	copy(lines, m.rendered)
	if m.cursor >= 0 && m.cursor < len(lines) {
		rows := strings.Split(lines[m.cursor], "\n")
		for i, row := range rows {
			rows[i] = selectedLineStyle.Width(m.width).Render(row)
		}
		lines[m.cursor] = strings.Join(rows, "\n")
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	if m.atBottom {
		m.viewport.GotoBottom()
	}
}

func (m *logviewModel) ScrollToBottom() { m.atBottom = true; m.viewport.GotoBottom() }
func (m *logviewModel) ScrollToTop()    { m.atBottom = false; m.viewport.GotoTop() }

// ── Detail panel ─────────────────────────────────────────────────────────────

func (m *logviewModel) OpenDetail() {
	if m.cursor < 0 || m.cursor >= len(m.filtered) {
		return
	}
	m.detailEntry = m.filtered[m.cursor]
	m.showDetail = true
	m.viewport.Height = m.viewportHeight()
}

func (m *logviewModel) CloseDetail() {
	m.showDetail = false
	m.viewport.Height = m.viewportHeight()
}

func (m *logviewModel) detailView() string {
	e := m.detailEntry
	hdr := detailHdrStyle.Width(m.width - 4).Render(
		fmt.Sprintf(" %-12s  %-8s  %-5s  %s",
			e.Timestamp.Format("15:04:05.000"),
			truncateTUI(e.Service, 8),
			strings.ToUpper(e.Level),
			truncateTUI(e.Message, m.width-48),
		),
	)

	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var rows []string
	maxRows := detailPanelHeight - 3
	if e.Message != "" && len(e.Fields) > 0 && len(rows) < maxRows {
		rows = append(rows, fmt.Sprintf("  %s  %s",
			detailKeyStyle.Render(fmt.Sprintf("%-20s", "message")),
			detailValStyle.Render(e.Message),
		))
	}
	for _, k := range keys {
		if len(rows) >= maxRows {
			break
		}
		rows = append(rows, fmt.Sprintf("  %s  %s",
			detailKeyStyle.Render(fmt.Sprintf("%-20s", k)),
			detailValStyle.Render(fmt.Sprintf("%v", e.Fields[k])),
		))
	}
	if len(rows) == 0 {
		rows = append(rows, styleDim.Render("  (no structured fields — plain text log line)"))
	}
	overflow := len(keys) + 1 - maxRows
	if overflow > 0 {
		rows = append(rows, styleDim.Render(fmt.Sprintf("  … %d more fields", overflow)))
	}
	body := strings.Join(append(rows, styleDim.Render("  [enter] or [esc] to close")), "\n")
	return detailBorderStyle.Width(m.width - 2).Render(hdr + "\n" + body)
}

// ── Bubble Tea ────────────────────────────────────────────────────────────────

func (m *logviewModel) Init() tea.Cmd { return nil }

func (m *logviewModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.atBottom = false
			if m.cursor > 0 {
				m.cursor--
			} else if m.cursor < 0 && len(m.rendered) > 0 {
				m.cursor = len(m.rendered) - 1
			}
			m.refreshContent()

		case "down", "j":
			if m.cursor >= 0 && m.cursor < len(m.rendered)-1 {
				m.cursor++
				m.refreshContent()
			}
			if m.viewport.AtBottom() {
				m.atBottom = true
			}

		case "enter":
			if m.showDetail {
				m.CloseDetail()
			} else if m.cursor >= 0 {
				m.OpenDetail()
			}
			return m, nil

		case "esc":
			if m.showDetail {
				m.CloseDetail()
				return m, nil
			}
		}

	case tea.MouseMsg:
		switch msg.Type {
		case tea.MouseWheelUp:
			m.atBottom = false
		case tea.MouseWheelDown:
			if m.viewport.AtBottom() {
				m.atBottom = true
			}
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	if m.viewport.AtBottom() && !m.atBottom {
		m.atBottom = true
	}
	return m, cmd
}

func (m *logviewModel) View() string {
	if len(m.rendered) == 0 && !m.paused {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			Width(m.width).Height(m.height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("No logs yet — waiting for entries…")
	}
	if m.showDetail {
		return m.viewport.View() + "\n" + m.detailView()
	}
	return m.viewport.View()
}

// ── Rendering ─────────────────────────────────────────────────────────────────

func (m *logviewModel) renderEntry(e source.LogEntry) string {
	ts := styleDim.Render(e.Timestamp.Format("15:04:05.000"))
	svc := lipgloss.NewStyle().Foreground(m.svcColors.colorFor(e.Service)).Bold(true).
		Render(fmt.Sprintf("%-8s", truncateTUI(e.Service, 8)))
	lvl := renderLevel(e.Level)

	msg := e.Message
	if msg == "" {
		msg = e.Raw
	}
	if len(e.Fields) > 0 {
		if summary := renderFieldSummary(e.Fields); summary != "" {
			msg = msg + "  " + styleDim.Render(summary)
		}
	}

	// prefixWidth = ts(12) + sp(1) + svc(8) + sp(1) + lvl(5) + sp(1) = 28
	const prefixWidth = 28
	msgWidth := m.width - prefixWidth
	if msgWidth < 20 || m.width == 0 {
		return fmt.Sprintf("%s %s %s %s", ts, svc, lvl, msg)
	}

	wrapped := wordWrap(msg, msgWidth)
	if len(wrapped) == 0 {
		return fmt.Sprintf("%s %s %s", ts, svc, lvl)
	}

	indent := strings.Repeat(" ", prefixWidth)
	out := make([]string, len(wrapped))
	out[0] = fmt.Sprintf("%s %s %s %s", ts, svc, lvl, wrapped[0])
	for i := 1; i < len(wrapped); i++ {
		out[i] = indent + wrapped[i]
	}
	return strings.Join(out, "\n")
}

// wordWrap splits text on spaces to fit maxWidth.
// ANSI-unaware: operates on byte length. For log lines this is fine.
func wordWrap(text string, maxWidth int) []string {
	if maxWidth <= 0 || len(text) <= maxWidth {
		return []string{text}
	}
	var lines []string
	for len(text) > maxWidth {
		cut := maxWidth
		if idx := strings.LastIndex(text[:maxWidth], " "); idx > 0 {
			cut = idx
		}
		lines = append(lines, text[:cut])
		text = strings.TrimLeft(text[cut:], " ")
	}
	if len(text) > 0 {
		lines = append(lines, text)
	}
	return lines
}

func renderFieldSummary(fields map[string]any) string {
	priority := []string{"request_id", "trace_id", "user_id", "status", "duration_ms", "error"}
	seen := map[string]bool{}
	var parts []string
	for _, k := range priority {
		if v, ok := fields[k]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
			seen[k] = true
			if len(parts) >= 3 {
				break
			}
		}
	}
	for k, v := range fields {
		if len(parts) >= 3 {
			break
		}
		if !seen[k] && !isStandardLogField(k) {
			parts = append(parts, fmt.Sprintf("%s=%v", k, v))
		}
	}
	return strings.Join(parts, " ")
}

func isStandardLogField(k string) bool {
	switch strings.ToLower(k) {
	case "level", "lvl", "severity", "msg", "message", "text", "log",
		"time", "timestamp", "ts", "t", "service", "svc":
		return true
	}
	return false
}

func renderLevel(level string) string {
	label := fmt.Sprintf("%-5s", strings.ToUpper(level))
	switch level {
	case source.LevelError:
		return styleError.Bold(true).Render(label)
	case source.LevelWarn:
		return styleWarn.Render(label)
	case source.LevelDebug:
		return styleDebug.Render(label)
	default:
		return styleInfo.Render(label)
	}
}

func truncateTUI(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
