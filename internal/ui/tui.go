// Package ui shows the machines: as a table on stdout (PrintStatus), or as a TUI
// (Run) with their services, stacks and logs.
package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// screen is one level of the TUI: a table, or a command's output. Enter opens a
// screen over the current one, esc goes back.
type screen interface {
	open(m *model) tea.Cmd // fills it, once on top
	key(m *model, k tea.KeyMsg) tea.Cmd
	resize(width, height int)
	view(m *model) string
}

type (
	hostsMsg struct {
		hosts []swarm.Host
		err   error
	}
	reportsMsg []swarm.Report
	tickMsg    int // refresh generation, so a manual refresh doesn't double the timer
)

type model struct {
	ctx               context.Context // cancelled when sshwarm exits: ends every ssh it started
	flake             string
	timeout, interval time.Duration

	hosts      []swarm.Host
	reports    []swarm.Report
	err        error
	refreshing bool
	refreshed  time.Time
	tickGen    int

	all           bool // services: show every unit, not only the interesting ones
	width, height int
	stack         []screen // the machines at the bottom; the top one is shown
}

func newModel(ctx context.Context, flake string, timeout, interval time.Duration) model {
	return model{
		ctx: ctx, flake: flake, timeout: timeout, interval: interval,
		stack: []screen{newTable(machines, -1)},
	}
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		h, err := swarm.LoadFlake(m.ctx, m.flake)
		return hostsMsg{h, err}
	}
}

func (m *model) refresh() tea.Cmd {
	if m.refreshing || m.hosts == nil {
		return nil
	}

	m.refreshing = true
	ctx, hosts, timeout := m.ctx, m.hosts, m.timeout

	return func() tea.Msg { return reportsMsg(swarm.ProbeAll(ctx, hosts, timeout)) }
}

func (m model) top() screen { return m.stack[len(m.stack)-1] }

// push shows s over the current screen.
func (m *model) push(s screen) tea.Cmd {
	s.resize(m.width, m.height)
	m.stack = append(m.stack, s)

	return s.open(m)
}

// pop goes back to the screen below, if any.
func (m *model) pop() {
	if len(m.stack) > 1 {
		m.stack = m.stack[:len(m.stack)-1]
	}
}

// rows refreshes every table from the reports and the toggles.
func (m *model) rows() {
	for _, s := range m.stack {
		if t, ok := s.(*table); ok {
			t.refresh(m)
		}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, s := range m.stack {
			s.resize(m.width, m.height)
		}
	case hostsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}

		m.hosts = msg.hosts

		return m, m.refresh()
	case reportsMsg:
		m.reports, m.refreshing, m.refreshed = msg, false, time.Now()
		m.rows()
		m.tickGen++
		gen := m.tickGen

		return m, tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg(gen) })
	case tickMsg:
		if int(msg) == m.tickGen {
			return m, m.refresh()
		}
	case logLinesMsg:
		if t, ok := m.top().(*text); ok && msg.ch == t.ch {
			return m, t.add(msg)
		}
	case tea.KeyMsg:
		return m.key(msg)
	}

	return m, nil
}

// key handles the keys every screen shares, unless a filter is being typed, then
// hands over to the top screen.
func (m model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	t, ok := m.top().(*table)
	typing := ok && t.filter.Focused()

	s := k.String()
	if s == "ctrl+c" || s == "q" && !typing {
		return m, tea.Quit // Run's context then ends every stream
	}

	if s == "r" && !typing {
		return m, m.refresh()
	}

	return m, m.top().key(&m, k)
}

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	dimStyle   = lipgloss.NewStyle().Faint(true)
	tabStyle   = lipgloss.NewStyle().Padding(0, 1)
	onTabStyle = tabStyle.Reverse(true)
)

func (m model) View() string {
	switch {
	case m.err != nil:
		return fmt.Sprintf("error: %v\n\nq to quit\n", m.err)
	case m.reports == nil:
		return "probing the machines from the flake…\n"
	}

	return m.top().view(&m)
}

// frame lays out a screen: a title line, the body, a help line.
func frame(title, body, help string) string {
	return title + "\n" + body + "\n" + dimStyle.Render(help)
}

// refreshStatus says when the reports were last refreshed.
func (m model) refreshStatus() string {
	if m.refreshing {
		return "refreshing…"
	}

	return "refreshed " + m.refreshed.Format(time.TimeOnly)
}

// Run browses the machines of the flake in flakeDir until the user quits.
func Run(flakeDir string, timeout, interval time.Duration) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // however the TUI ends, no log stream or probe outlives it
	_, err := tea.NewProgram(newModel(ctx, flakeDir, timeout, interval), tea.WithAltScreen()).Run()

	return err
}
