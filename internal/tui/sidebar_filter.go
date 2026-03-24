package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/OpenCortex-Labs/logr/internal/filter"
	"github.com/OpenCortex-Labs/logr/internal/source"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Sidebar ───────────────────────────────────────────────────────────────────

var (
	sidebarBorder    = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, true, false, false).BorderForeground(lipgloss.Color("#333333"))
	activeSrcStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#50FA7B")).Bold(true)
	inactiveSrcStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555"))
	sidebarHdr       = lipgloss.NewStyle().Foreground(lipgloss.Color("#AAAAAA")).Bold(true).MarginBottom(1)
	sidebarSelected  = lipgloss.NewStyle().Background(lipgloss.Color("#3D3D5C")).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1)
)

type sourceStats struct {
	name   string
	total  int
	errors int
	active bool
}

// levelOptions for sidebar LEVEL section: display label and filter value.
var levelOptions = []struct {
	label string
	value string // "" = all
}{
	{"all", ""},
	{"error", source.LevelError},
	{"warn", source.LevelWarn},
	{"info", source.LevelInfo},
	{"debug", source.LevelDebug},
}

type sidebarModel struct {
	sources              []sourceStats
	visible              bool
	width                int
	height               int
	selectedIndex        int  // 0 = all, 1..n = sources (source section)
	levelSelectedIndex   int  // 0 = all, 1..4 = error,warn,info,debug (level section)
	sourceSectionFocused bool // true = SOURCES list focused, false = LEVEL list focused
}

func newSidebarModel(names []string, initialLevel string) sidebarModel {
	stats := make([]sourceStats, len(names))
	for i, n := range names {
		stats[i] = sourceStats{name: n, active: true}
	}
	s := sidebarModel{sources: stats, visible: true, sourceSectionFocused: true}
	for i, opt := range levelOptions {
		if strings.EqualFold(opt.value, initialLevel) {
			s.levelSelectedIndex = i
			break
		}
	}
	return s
}

func (s *sidebarModel) SetSize(w, h int) { s.width = w; s.height = h }
func (s *sidebarModel) Toggle()          { s.visible = !s.visible }

// SelectedSource returns the source name to filter by, or "" for all.
func (s *sidebarModel) SelectedSource() string {
	if s.selectedIndex <= 0 || s.selectedIndex > len(s.sources) {
		return ""
	}
	return s.sources[s.selectedIndex-1].name
}

func (s *sidebarModel) MoveSelection(up bool) {
	if s.sourceSectionFocused {
		sourceCount := len(s.sources)
		if sourceCount == 0 {
			return
		}
		if up {
			s.selectedIndex--
			if s.selectedIndex < 0 {
				s.selectedIndex = 0
			}
		} else {
			s.selectedIndex++
			if s.selectedIndex > sourceCount {
				s.selectedIndex = sourceCount
			}
		}
	} else {
		maxLevel := len(levelOptions) - 1
		if up {
			s.levelSelectedIndex--
			if s.levelSelectedIndex < 0 {
				s.levelSelectedIndex = 0
			}
		} else {
			s.levelSelectedIndex++
			if s.levelSelectedIndex > maxLevel {
				s.levelSelectedIndex = maxLevel
			}
		}
	}
}

// ToggleSectionFocus switches between Sources and Level list in the sidebar.
func (s *sidebarModel) ToggleSectionFocus() {
	s.sourceSectionFocused = !s.sourceSectionFocused
}

// FocusSources moves focus to the Sources list (↑↓ will move there).
func (s *sidebarModel) FocusSources() {
	s.sourceSectionFocused = true
}

// FocusLevel moves focus to the Level list (↑↓ will move there).
func (s *sidebarModel) FocusLevel() {
	s.sourceSectionFocused = false
}

// SelectedLevel returns the level to filter by, or "" for all.
func (s *sidebarModel) SelectedLevel() string {
	if s.levelSelectedIndex < 0 || s.levelSelectedIndex >= len(levelOptions) {
		return ""
	}
	return levelOptions[s.levelSelectedIndex].value
}

// SourceSectionFocused returns true if SOURCES list is focused (false = LEVEL list).
func (s sidebarModel) SourceSectionFocused() bool {
	return s.sourceSectionFocused
}

func (s *sidebarModel) AddEntry(e source.LogEntry) {
	svc := strings.TrimSpace(e.Service)
	if svc == "" {
		return
	}
	for i, src := range s.sources {
		if strings.EqualFold(src.name, svc) || (len(src.name) > 0 && strings.HasPrefix(strings.ToLower(svc), strings.ToLower(src.name))) {
			s.sources[i].total++
			if e.Level == source.LevelError {
				s.sources[i].errors++
			}
			return
		}
	}
	s.sources = append(s.sources, sourceStats{
		name:   svc,
		total:  1,
		errors: boolInt(e.Level == source.LevelError),
		active: true,
	})
}

func (s sidebarModel) View() string {
	if !s.visible || s.width == 0 {
		return ""
	}
	var rows []string
	// Sources — ▸ shows which list gets ↑↓
	var srcHdr string
	if s.sourceSectionFocused {
		srcHdr = "▸ Sources"
	} else {
		srcHdr = "  Sources"
	}
	rows = append(rows, sidebarHdr.Render(truncateTUI(srcHdr, s.width-2)))
	sel := s.selectedIndex == 0
	allLabel := "all"
	if sel {
		rows = append(rows, sidebarSelected.Render(truncateTUI(allLabel, s.width-4)))
	} else {
		rows = append(rows, styleDim.Render(truncateTUI(allLabel, s.width-4)))
	}
	for i, src := range s.sources {
		idx := i + 1
		dot, style := "●", activeSrcStyle
		if !src.active {
			dot, style = "○", inactiveSrcStyle
		}
		label := fmt.Sprintf("%s %s %d", dot, truncateTUI(src.name, s.width-8), src.total)
		if src.errors > 0 {
			badge := lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")).Render(fmt.Sprintf(" %d!", src.errors))
			label += badge
		}
		if s.selectedIndex == idx {
			rows = append(rows, sidebarSelected.Render(label))
		} else {
			rows = append(rows, style.Render(label))
		}
	}
	// Level — ▸ shows which list gets ↑↓; Enter applies both
	var levelHdr string
	if !s.sourceSectionFocused {
		levelHdr = "▸ Level"
	} else {
		levelHdr = "  Level"
	}
	rows = append(rows, "")
	rows = append(rows, sidebarHdr.Render(truncateTUI(levelHdr, s.width-2)))
	for i, opt := range levelOptions {
		label := truncateTUI(opt.label, s.width-4)
		if s.levelSelectedIndex == i {
			rows = append(rows, sidebarSelected.Render(label))
		} else {
			rows = append(rows, styleDim.Render(label))
		}
	}
	return sidebarBorder.Width(s.width).Height(s.height).Padding(1, 1).Render(strings.Join(rows, "\n"))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ── Filter bar ────────────────────────────────────────────────────────────────

var (
	filterActiveStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#1A1A2E")).Foreground(lipgloss.Color("#FFFFFF")).Padding(0, 1)
	filterInactiveStyle = lipgloss.NewStyle().Background(lipgloss.Color("#111111")).Foreground(lipgloss.Color("#555555")).Padding(0, 1)
	filterErrorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555")).Padding(0, 1)
)

type filterModel struct {
	input     textinput.Model
	active    bool
	submitted bool
	err       error
}

func newFilterModel(f *filter.Filter) filterModel {
	ti := textinput.New()
	ti.Placeholder = "filter regex… (enter to apply, esc to cancel)"
	ti.CharLimit = 100
	if f.Grep != nil {
		ti.SetValue(f.Grep.String())
	}
	return filterModel{input: ti}
}

func (m *filterModel) Focus() { m.active = true; m.input.Focus() }
func (m *filterModel) Blur()  { m.active = false; m.input.Blur(); m.submitted = true }

func (m filterModel) Init() tea.Cmd {
	return nil
}

func (m filterModel) compiledRegex() *regexp.Regexp {
	v := m.input.Value()
	if v == "" {
		return nil
	}
	re, err := regexp.Compile(v)
	if err != nil {
		return nil
	}
	return re
}

func (m filterModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if !m.active {
		return m, nil
	}
	var cmd tea.Cmd
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "enter":
			if v := m.input.Value(); v != "" {
				if _, err := regexp.Compile(v); err != nil {
					m.err = err
					return m, nil
				}
			}
			m.err = nil
			m.submitted = true
			m.active = false
			m.input.Blur()
			return m, nil
		case "esc":
			m.active = false
			m.input.Blur()
			m.submitted = true
			return m, nil
		}
	}
	m.input, cmd = m.input.Update(msg)
	if v := m.input.Value(); v != "" {
		if _, err := regexp.Compile(v); err != nil {
			m.err = err
		} else {
			m.err = nil
		}
	} else {
		m.err = nil
	}
	return m, cmd
}

func (m filterModel) View() string {
	prefix := "  filter: "
	if m.err != nil {
		return filterErrorStyle.Render("⚠ invalid regex: " + m.err.Error())
	}
	if m.active {
		return filterActiveStyle.Render(prefix + m.input.View())
	}
	if v := m.input.Value(); v != "" {
		return filterActiveStyle.Render(prefix + v)
	}
	return filterInactiveStyle.Render(prefix + "_")
}
