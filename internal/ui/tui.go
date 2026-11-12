// Package ui shows the machines: as a table on stdout (PrintStatus), or as a TUI
// (Run) with their services, stacks and logs.
package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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

	all           bool            // services: show every unit, not only the interesting ones
	filter        textinput.Model // edits the top table's filter
	width, height int
	stack         []screen // the machines at the bottom; the top one is shown
}

func newModel(ctx context.Context, flake string, timeout, interval time.Duration) model {
	f := textinput.New()
	f.Prompt = "/"

	return model{
		ctx: ctx, flake: flake, timeout: timeout, interval: interval, filter: f,
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

	return func() tea.Msg { return reportsMsg(swarm.ProbeAll(ctx, hosts, timeout, true)) }
}

func (m model) top() screen { return m.stack[len(m.stack)-1] }

// push shows s over the current screen, with an empty filter.
func (m *model) push(s screen) tea.Cmd {
	s.resize(m.width, m.height)
	m.stack = append(m.stack, s)
	m.filter.SetValue("")

	return s.open(m)
}

// pop goes back to the screen below, and its filter.
func (m *model) pop() {
	if len(m.stack) == 1 {
		return
	}

	m.stack = m.stack[:len(m.stack)-1]
	if t, ok := m.top().(*table); ok {
		m.filter.SetValue(t.query)
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

// key handles the keys every screen shares, then hands over to the top screen.
func (m model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if s == "ctrl+c" || s == "q" && !m.filter.Focused() {
		return m, tea.Quit // Run's context then ends every stream
	}

	if m.filter.Focused() {
		return m.filterKey(k)
	}

	if s == "r" {
		return m, m.refresh()
	}

	return m, m.top().key(&m, k)
}

// filterKey types into the top table's filter: enter keeps it, esc clears it.
func (m model) filterKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch k.String() {
	case "esc":
		m.filter.SetValue("")
		m.filter.Blur()
	case "enter":
		m.filter.Blur()
	default:
		m.filter, cmd = m.filter.Update(k)
	}

	if t, ok := m.top().(*table); ok {
		t.query = m.filter.Value()
		t.refresh(&m)
	}

	return m, cmd
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
