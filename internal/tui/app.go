package tui

import (
	"context"
	"fmt"

	"github.com/OpenCortex-Labs/logr/internal/fanin"
	"github.com/OpenCortex-Labs/logr/internal/filter"
	"github.com/OpenCortex-Labs/logr/internal/source"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Run starts the full-screen TUI. Blocks until user quits.
func Run(ctx context.Context, sources []source.Source, f *filter.Filter) error {
	merged := fanin.Merge(ctx, sources)
	entries := make(chan source.LogEntry, 512)
	go func() {
		for entry := range merged {
			entries <- entry
		}
		close(entries)
	}()
	m := newAppModel(sources, f, entries)
	p := tea.NewProgram(m,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}

type appModel struct {
	sidebar        sidebarModel
	logview        logviewModel
	filter         filterModel
	sources        []source.Source
	f              *filter.Filter
	entries        chan source.LogEntry
	showHelp       bool
	sidebarFocused bool // when true, ↑↓ select source, Enter applies
	width          int
	height         int
}

func newAppModel(sources []source.Source, f *filter.Filter, entries chan source.LogEntry) appModel {
	names := make([]string, len(sources))
	for i, s := range sources {
		names[i] = s.Name()
	}
	return appModel{
		sources: sources,
		f:       f,
		entries: entries,
		sidebar: newSidebarModel(names, f.Level),
		logview: *newLogviewModel(),
		filter:  newFilterModel(f),
	}
}

type batchMsg struct{ entries []source.LogEntry }

func (m appModel) Init() tea.Cmd {
	return m.pollEntries()
}

func (m appModel) pollEntries() tea.Cmd {
	return func() tea.Msg {
		var batch []source.LogEntry
		// Drain up to 200 entries non-blocking first.
		for range 200 {
			select {
			case e, ok := <-m.entries:
				if !ok {
					return nil
				}
				batch = append(batch, e)
			default:
				goto done
			}
		}
	done:
		// If nothing was immediately available, block for one entry so we
		// don't spin-loop burning CPU when the stream is quiet.
		if len(batch) == 0 {
			e, ok := <-m.entries
			if ok {
				batch = append(batch, e)
			}
		}
		return batchMsg{entries: batch}
	}
}

func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.relayout()

	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
			return m, nil
		case "/":
			m.filter.Focus()
			m.sidebarFocused = false
			return m, nil
		case "esc":
			if !m.logview.showDetail {
				m.filter.Blur()
			}
			m.sidebarFocused = false
		case "e":
			m.f.ToggleErrorOnly()
			m.logview.refilter(m.f)
		case "s":
			m.sidebar.Toggle()
			m.relayout()
		case "tab", "left", "right":
			if m.sidebar.visible && !m.filter.active {
				// Tab only enters or exits sidebar; use 1/2 to switch Sources vs Level
				if m.sidebarFocused {
					m.sidebarFocused = false
				} else {
					m.sidebarFocused = true
					m.sidebar.FocusSources()
				}
				return m, tea.Batch(cmds...)
			}
		case "G":
			if !m.sidebarFocused {
				m.logview.ScrollToBottom()
			}
		case "g":
			if !m.sidebarFocused {
				m.logview.ScrollToTop()
			}
		case "p":
			m.logview.TogglePause()
		}

		// Sidebar: 1=Sources 2=Level only; ↑↓ move; Tab = back to logs; Enter apply both
		if m.sidebar.visible && m.sidebarFocused {
			switch msg.String() {
			case "1":
				m.sidebar.FocusSources()
				return m, tea.Batch(cmds...)
			case "2":
				m.sidebar.FocusLevel()
				return m, tea.Batch(cmds...)
			case "up", "k":
				m.sidebar.MoveSelection(true)
				return m, tea.Batch(cmds...)
			case "down", "j":
				m.sidebar.MoveSelection(false)
				return m, tea.Batch(cmds...)
			case "enter":
				m.f.Service = m.sidebar.SelectedSource()
				m.f.Level = m.sidebar.SelectedLevel()
				m.f.LevelExact = (m.f.Level != "")
				m.logview.refilter(m.f)
				return m, tea.Batch(cmds...)
			}
		}

		lv, cmd := m.logview.Update(msg)
		m.logview = *lv.(*logviewModel)
		cmds = append(cmds, cmd)

	case tea.MouseMsg:
		lv, cmd := m.logview.Update(msg)
		m.logview = *lv.(*logviewModel)
		cmds = append(cmds, cmd)

	case batchMsg:
		for _, e := range msg.entries {
			m.sidebar.AddEntry(e)
			m.logview.AddEntry(e, m.f)
		}
		// Flush once per batch — O(n) instead of O(n²).
		m.logview.FlushPending()
		cmds = append(cmds, m.pollEntries())
	}

	fm, cmd := m.filter.Update(msg)
	m.filter = fm.(filterModel)
	if m.filter.submitted {
		m.f.Grep = m.filter.compiledRegex()
		m.logview.refilter(m.f)
		m.filter.submitted = false
	}
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// relayout recalculates sizes for every sub-component.
// logview width  = terminal width − sidebar width (sidebar is 0 when hidden).
// logview height = terminal height − header(1) − filterbar(1) − statusbar(1) = height − 3.
func (m *appModel) relayout() {
	sidebarWidth := 0
	if m.sidebar.visible {
		sidebarWidth = 20 // fixed sidebar width including its right border
	}
	logviewWidth := max(m.width-sidebarWidth, 1)
	m.sidebar.SetSize(sidebarWidth, m.height-3)
	m.logview.SetSize(logviewWidth, m.height-3)
}

func (m appModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}
	if m.showHelp {
		return m.helpView()
	}

	header := m.headerView()
	filterBar := m.filter.View()

	var body string
	if m.sidebar.visible {
		body = lipgloss.JoinHorizontal(lipgloss.Top, m.sidebar.View(), m.logview.View())
	} else {
		body = m.logview.View()
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, filterBar, body, m.statusBarView())
}

func (m appModel) headerView() string {
	status := fmt.Sprintf(" logr  •  %d source(s)  •  %d lines  •  %d errors",
		len(m.sources), m.logview.totalCount, m.logview.errorCount)
	if m.logview.paused {
		badge := lipgloss.NewStyle().
			Background(lipgloss.Color("#FFB86C")).
			Foreground(lipgloss.Color("#1E1B4B")).
			Bold(true).Padding(0, 1).
			Render(fmt.Sprintf("⏸ PAUSED  +%d buffered", m.logview.pausedCount))
		status = status + "  " + badge
	}
	status += "  [? help]"
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#5C5FE4")).
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true).Padding(0, 1).Width(m.width).
		Render(status)
}

func (m appModel) statusBarView() string {
	hint := "[/] filter  [e] errors  [p] pause  [s] sidebar  [↑↓] cursor  [enter] detail  [g/G] top/bottom  [q] quit"
	if m.sidebar.visible && m.sidebarFocused {
		hint = "↑↓  Enter  Tab  •  " + hint
	}
	if m.f.ErrorOnly {
		hint = "⚠ errors  " + hint
	}
	if m.f.Service != "" {
		hint = "📌 " + m.f.Service + "  " + hint
	}
	if m.f.Level != "" {
		hint = "📋 level:" + m.f.Level + "  " + hint
	}
	if m.logview.paused {
		hint = "⏸ pause  " + hint
	}
	// Single line: truncate if too long so status bar doesn't wrap into duplicate-looking lines
	maxLen := max(m.width-2, 40)
	if len(hint) > maxLen {
		hint = hint[:maxLen-3] + "..."
	}
	return lipgloss.NewStyle().
		Background(lipgloss.Color("#2D2D2D")).
		Foreground(lipgloss.Color("#AAAAAA")).
		Padding(0, 1).Width(m.width).MaxHeight(1).Render(hint)
}

func (m appModel) helpView() string {
	content := `logr keyboard shortcuts

  /        open filter bar
  esc      close filter bar / close detail
  e        toggle errors-only mode
  s        toggle sidebar
  1 / 2    in sidebar: Sources (1) or Level (2) — only way to switch
  tab      in sidebar: back to logs
  ↑↓ / jk  in sidebar: move (▸ = active list); in logs: scroll
  enter    in sidebar: apply both source + level; in logs: detail
  p        pause / resume live stream
  g / G    scroll to top / bottom
  PgUp/Dn  scroll one page
  ?        toggle this help
  q        quit`

	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2).Width(54).Render(content)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
