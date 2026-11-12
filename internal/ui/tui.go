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
	reportsMsg []swarm.Report
	tickMsg    int // refresh generation, so a manual refresh doesn't double the timer
)

type model struct {
	ctx               context.Context // cancelled when sshwarm exits: ends every ssh it started
	timeout, interval time.Duration

	hosts      []swarm.Host
	reports    []swarm.Report
	refreshing bool
	refreshed  time.Time
	tickGen    int

	all           bool // services: show every unit, not only the interesting ones
	width, height int
	stack         []screen // the machines at the bottom; the top one is shown
}

func newModel(ctx context.Context, hosts []swarm.Host, timeout, interval time.Duration) model {
	return model{
		ctx: ctx, hosts: hosts, timeout: timeout, interval: interval, refreshing: true,
		stack: []screen{newTable(machines, -1)},
	}
}

// Init probes the machines a first time.
func (m model) Init() tea.Cmd { return m.probe() }

// refresh probes the machines again, unless a probe is still running.
func (m *model) refresh() tea.Cmd {
	if m.refreshing {
		return nil
	}

	m.refreshing = true

	return m.probe()
}

func (m model) probe() tea.Cmd {
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
	if m.reports == nil {
		return fmt.Sprintf("probing %d machines…\n", len(m.hosts))
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

// Run browses the machines until the user quits.
func Run(hosts []swarm.Host, timeout, interval time.Duration) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // however the TUI ends, no log stream or probe outlives it
	_, err := tea.NewProgram(newModel(ctx, hosts, timeout, interval), tea.WithAltScreen()).Run()

	return err
}
