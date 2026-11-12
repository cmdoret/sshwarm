// Package ui shows the machines: as a table on stdout (PrintStatus), or as a TUI
// (Run) with their services, stacks, containers and logs.
package ui

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	dimStyle   = lipgloss.NewStyle().Faint(true)
)

// chrome is the lines frame draws around a screen's body: its title and help lines.
const chrome = 2

// screen is one level of the TUI: a table, or a command's output. Enter opens a
// screen over the current one, esc goes back.
type screen interface {
	open(m *model) tea.Cmd // fills it, once on top
	key(m *model, k tea.KeyMsg) tea.Cmd
	resize(width, height int)
	view(m *model) string
}

type (
	reportsMsg   []swarm.Report
	tickMsg      int                 // refresh generation, so a manual refresh doesn't double the timer
	shellDoneMsg struct{ err error } // a shell ended, with the error of ssh, or of what it ran
)

// execProcess hands the terminal to a command until it ends. Tests replace it.
var execProcess = tea.ExecProcess

type model struct {
	ctx               context.Context // cancelled when sshwarm exits: ends every ssh it started
	timeout, interval time.Duration

	hosts      []swarm.Host
	reports    []swarm.Report // nil until the first probe; then one per host, in order
	refreshing bool
	refreshed  time.Time
	tickGen    int

	all           bool // services: show every unit, not only the interesting ones
	width, height int
	screens       []screen        // the machines at the bottom; the top one is shown
	command       textinput.Model // the command bar: ":ctr web-1"
	flash         string          // a message, shown until the next key
}

func newModel(ctx context.Context, hosts []swarm.Host, timeout, interval time.Duration) model {
	c := textinput.New()
	c.Prompt = ":"

	return model{
		ctx: ctx, hosts: hosts, timeout: timeout, interval: interval, refreshing: true,
		screens: []screen{newTable(machines, -1, "")}, command: c,
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

func (m model) top() screen { return m.screens[len(m.screens)-1] }

// push shows s over the current screen.
func (m *model) push(s screen) tea.Cmd {
	s.resize(m.width, m.height)
	m.screens = append(m.screens, s)

	return s.open(m)
}

// pop goes back to the screen below, if any.
func (m *model) pop() { m.popTo(max(len(m.screens)-1, 1)) }

// popTo goes back to the n bottom screens. It stops the streams of the logs it
// closes: nothing else would, until sshwarm exits.
func (m *model) popTo(n int) {
	for _, s := range m.screens[n:] {
		if t, ok := s.(*text); ok {
			t.stop()
		}
	}

	m.screens = m.screens[:n]
}

// fillTables rebuilds every table's rows, after a probe or a toggle.
func (m *model) fillTables() {
	for _, s := range m.screens {
		if t, ok := s.(*table); ok {
			t.fill(m)
		}
	}
}

// Update returns a command computed before m: in `return m, m.refresh()`, Go
// doesn't say whether m is read before or after refresh changes it.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, s := range m.screens {
			s.resize(m.width, m.height)
		}
	case reportsMsg:
		m.reports, m.refreshing, m.refreshed = msg, false, time.Now()
		m.fillTables()
		m.tickGen++
		gen := m.tickGen
		cmd = tea.Tick(m.interval, func(time.Time) tea.Msg { return tickMsg(gen) })
	case tickMsg:
		if int(msg) == m.tickGen {
			cmd = m.refresh()
		}
	case logLinesMsg:
		if t, ok := m.top().(*text); ok && msg.ch == t.ch {
			cmd = t.add(msg)
		}
	case shellDoneMsg:
		// The alt screen is back over what the shell printed: say how it ended.
		if msg.err != nil {
			m.flash = "shell ended: " + msg.err.Error()
		}

		cmd = m.refresh() // what was done there shows now
	case tea.KeyMsg:
		return m.key(msg)
	}

	return m, cmd
}

// key handles the keys every screen shares, unless something is being typed, then
// hands over to the top screen.
func (m model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.flash = ""

	s := k.String()
	if s == "ctrl+c" {
		return m, tea.Quit // Run's context then ends every stream
	}

	if m.command.Focused() {
		return m.commandKey(k)
	}

	var cmd tea.Cmd

	if t, ok := m.top().(*table); ok && t.filter.Focused() {
		cmd = t.filterKey(&m, k)

		return m, cmd
	}

	if s == "q" {
		return m, tea.Quit
	}

	if m.reports == nil {
		return m, nil // nothing to open before the first probe
	}

	switch s {
	case "r":
		cmd = m.refresh()
	case ":":
		cmd = m.command.Focus()
	default:
		cmd = m.top().key(&m, k)
	}

	return m, cmd
}

// commandKey types into the command bar: enter goes there, esc cancels.
func (m model) commandKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch k.String() {
	case "esc":
		m.command.Blur()
		m.command.SetValue("")
	case "enter":
		m.command.Blur()
		cmd = m.goTo(m.command.Value())
		m.command.SetValue("")
	default:
		m.command, cmd = m.command.Update(k)
	}

	return m, cmd
}

// goTo opens the table a command names: a kind, then a machine by name, or else
// the machine on screen. Like k9s, it starts over from the machines, so esc goes
// back there; ":machines" just goes back.
func (m *model) goTo(command string) tea.Cmd {
	kindName, name, _ := strings.Cut(strings.TrimSpace(command), " ")
	if kindName == "" {
		return nil
	}

	k := kindNamed(kindName)
	if k == nil {
		var names []string
		for _, k := range kinds {
			names = append(names, strings.Join(k.names, "/"))
		}

		m.flash = fmt.Sprintf("no %q here: try %s", kindName, strings.Join(names, ", "))

		return nil
	}

	if k == machines {
		m.popTo(1)

		return nil
	}

	host, err := m.machine(strings.TrimSpace(name))
	if err != nil {
		m.flash = err.Error()

		return nil
	}

	if r := m.reports[host]; r.Err == nil && !slices.Contains(tabsOf(r), k) {
		m.flash = fmt.Sprintf("%s has no %s", r.Host.Name, k.names[0])

		return nil
	}

	m.popTo(1)

	return m.push(newTable(k, host, ""))
}

// shell hands the terminal to ssh on h, running remote, or with remote "", a login
// shell. The TUI is back when it ends.
func (m *model) shell(h swarm.Host, remote string) tea.Cmd {
	return execProcess(swarm.Interactive(m.ctx, h, remote), func(err error) tea.Msg { return shellDoneMsg{err} })
}

// machine is the index of the machine named, or else of the one on screen.
func (m model) machine(name string) (int, error) {
	if name != "" {
		return swarm.Find(m.hosts, name)
	}

	for _, s := range slices.Backward(m.screens) {
		if t, ok := s.(*table); ok && t.kind != machines {
			return t.host, nil
		}
	}

	return -1, errors.New("which machine? e.g. :ctr web-1")
}

// View shows the top screen, every line cut to the window's width.
func (m model) View() string {
	if m.reports == nil {
		return fmt.Sprintf("probing %d machines…\n", len(m.hosts))
	}

	lines := strings.Split(m.top().view(&m), "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}

	return strings.Join(lines, "\n")
}

// frame lays out a screen: a title line, the body, and a help line, unless the
// command bar or a message takes its place.
func (m *model) frame(title, body, help string) string {
	bar := m.bar()
	if bar == "" {
		bar = dimStyle.Render(help)
	}

	return title + "\n" + body + "\n" + bar
}

// bar is the command bar while it's open, or the message if there is one.
func (m *model) bar() string {
	switch {
	case m.command.Focused():
		return m.command.View()
	case m.flash != "":
		return toneStyles[warn].Render(m.flash)
	}

	return ""
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
