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

	"github.com/charmbracelet/bubbles/key"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/remote"
	"github.com/cmdoret/sshwarm/internal/swarm"
)

const (
	// chrome is the lines a frame adds around a screen's body: the box's top and
	// bottom edges, and the help line.
	chrome = 3

	// sides is the columns a frame adds around a screen's body: the box's edges.
	sides = 2
)

// screen is one level of the TUI: a table, or a command's output. Enter opens a
// screen over the current one, esc goes back.
type screen interface {
	open(m *model) tea.Cmd                // it's on top: fills it
	update(m *model, msg tea.Msg) tea.Cmd // a key, or a message for it
	resize(width, height int)             // of the window
	view(m *model) frame
	close()       // it's closed: stops what it runs
	busy() bool   // it waits on a machine: the spinner turns
	typing() bool // it takes every key, as a filter being typed does
}

// frame is what a screen shows: its body in a box, with the title and the status on
// its top edge, and its help below; or when full, the body alone, the whole window.
type frame struct {
	title, status, body string
	help                []key.Binding
	full                bool
}

// bodySize is the size of a screen's body in a window width by height: all of it
// when full, or else what the frame leaves.
func bodySize(width, height int, full bool) (w, h int) {
	if full {
		return width, height
	}

	return width - sides, height - chrome
}

type (
	reportsMsg   []swarm.Report
	refillMsg    struct{}            // the reports, or what to show of them, changed
	tickMsg      int                 // refresh generation, so a manual refresh doesn't double the timer
	shellDoneMsg struct{ err error } // a shell ended, with the error of ssh, or of what it ran

	actionDoneMsg string // how an action went, for the message line
)

// execProcess hands the terminal to a command until it ends. Tests replace it.
var execProcess = tea.ExecProcess

type model struct {
	ctx     context.Context // cancelled when sshwarm exits: ends every ssh it started
	hosts   []inventory.Host
	reports []swarm.Report // nil until the first probe; then one per host, in order
	probes  prober
	spin    spinner.Model // turns while the TUI waits on the machines

	readOnly bool    // no actions
	pending  *action // the action asking to be confirmed, if any
	running  int     // actions running

	all           bool // services: show every unit, not only the interesting ones
	width, height int
	fleet         *fleet          // the bottom screen
	screens       []screen        // the machines' first; the top one is shown
	command       textinput.Model // the command bar: ":ctr web-1"
	flash         string          // a message, shown until the next key
}

func newModel(ctx context.Context, hosts []inventory.Host, timeout, interval time.Duration) *model {
	c := textinput.New()
	c.Prompt = ":"

	fleet := newFleet()

	return &model{
		ctx: ctx, hosts: hosts,
		probes:  prober{ctx: ctx, hosts: hosts, timeout: timeout, interval: interval, running: true}, // Init's
		spin:    spinner.New(spinner.WithSpinner(spinner.Meter), spinner.WithStyle(keyStyle)),
		fleet:   fleet,
		screens: []screen{fleet}, command: c,
	}
}

// Init probes the machines a first time.
func (m *model) Init() tea.Cmd { return tea.Batch(m.probes.probe(), m.spin.Tick) }

// refresh probes the machines again: see prober.refresh.
func (m *model) refresh() tea.Cmd { return m.probes.refresh() }

// waiting says whether the TUI waits on the machines: on a probe, an action, or the
// screen on top. The spinner turns meanwhile.
func (m *model) waiting() bool { return m.probes.running || m.running > 0 || m.top().busy() }

func (m *model) top() screen { return m.screens[len(m.screens)-1] }

// push shows s over the current screen.
func (m *model) push(s screen) tea.Cmd {
	s.resize(m.width, m.height)
	m.screens = append(m.screens, s)

	return s.open(m)
}

// pop goes back to the screen below, if any.
func (m *model) pop() { m.popTo(max(len(m.screens)-1, 1)) }

// popTo goes back to the n bottom screens, closing the others: nothing else would
// stop what they run, until sshwarm exits.
func (m *model) popTo(n int) {
	for _, s := range m.screens[n:] {
		s.close()
	}

	m.screens = m.screens[:n]
}

// refill has every screen show the reports again, after a probe or a toggle.
func (m *model) refill() tea.Cmd {
	var cmds []tea.Cmd
	for _, s := range m.screens {
		cmds = append(cmds, s.update(m, refillMsg{}))
	}

	return tea.Batch(cmds...)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	wasWaiting := m.waiting()

	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, s := range m.screens {
			s.resize(m.width, m.height)
		}
	case reportsMsg:
		m.reports = msg
		cmd = tea.Batch(m.probes.done(), m.refill())
	case tickMsg:
		cmd = m.probes.tick(int(msg))
	case spinner.TickMsg:
		if m.waiting() { // else no next tick: the spinner stops until the next wait
			m.spin, cmd = m.spin.Update(msg)
		}
	case logLinesMsg, redescribeMsg:
		cmd = m.top().update(m, msg)
	case actionDoneMsg:
		m.running--
		m.flash = string(msg)
		cmd = m.refresh() // what changed shows
	case shellDoneMsg:
		// The alt screen is back over what the shell printed: say how it ended.
		if msg.err != nil {
			m.flash = "shell ended: " + msg.err.Error()
		}

		cmd = m.refresh() // what was done there shows now
	case tea.KeyMsg:
		cmd = m.key(msg)
	}

	if !wasWaiting && m.waiting() { // a wait begins, whatever began it: the spinner turns again
		cmd = tea.Batch(cmd, m.spin.Tick)
	}

	return m, cmd
}

// key handles the keys every screen shares, unless something is being typed, then
// hands over to the top screen.
func (m *model) key(k tea.KeyMsg) tea.Cmd {
	if key.Matches(k, keyForceQuit) {
		return tea.Quit // Run's context then ends every stream
	}

	if m.pending != nil { // the prompt hid any message since: confirmKey keeps it
		return m.confirmKey(k)
	}

	m.flash = ""

	if m.command.Focused() {
		return m.commandKey(k)
	}

	if m.top().typing() {
		return m.top().update(m, k)
	}

	if key.Matches(k, keyQuit) {
		return tea.Quit
	}

	if m.reports == nil {
		return nil // nothing to open before the first probe
	}

	if key.Matches(k, keyGoTo) {
		return m.command.Focus()
	}

	return m.top().update(m, k)
}

// confirmKey runs the action asked for on y, in the background; any other key
// cancels it.
func (m *model) confirmKey(k tea.KeyMsg) tea.Cmd {
	a := *m.pending
	m.pending = nil

	if !key.Matches(k, keyYes) {
		if m.flash == "" { // else how an action went, which came during the prompt: news
			m.flash = "cancelled"
		}

		return nil
	}

	m.running++
	ctx := m.ctx

	return func() tea.Msg {
		out, err := remote.Run(ctx, a.host, a.cmd)

		return actionDoneMsg(result(a, out, err))
	}
}

// result says how a went, given its output and error: done, or why not, as the last
// line it printed or else its error.
func result(a action, out string, err error) string {
	if err == nil {
		return a.done + " " + a.what
	}

	why := err.Error()

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if last := lines[len(lines)-1]; last != "" {
		why = last
	}

	return fmt.Sprintf("%s %s failed: %s", a.verb, a.what, why)
}

// commandKey types into the command bar: enter goes there, esc cancels.
func (m *model) commandKey(k tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch {
	case key.Matches(k, keyBack):
		m.command.Blur()
		m.command.SetValue("")
	case key.Matches(k, keyEnter):
		m.command.Blur()
		cmd = m.goTo(m.command.Value())
		m.command.SetValue("")
	default:
		m.command, cmd = m.command.Update(k)
	}

	return cmd
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
	m.fleet.grid.choose(m.hosts[host].Name) // so that esc goes back to it

	return m.push(newHost(k, host, ""))
}

// shell hands the terminal to ssh on h, running cmd, or with cmd "", a login shell.
// The TUI is back when it ends.
func (m *model) shell(h inventory.Host, cmd string) tea.Cmd {
	return execProcess(remote.Interactive(m.ctx, h, cmd), func(err error) tea.Msg { return shellDoneMsg{err} })
}

// machine is the index of the machine named, or else of the one on screen.
func (m *model) machine(name string) (int, error) {
	if name != "" {
		return inventory.Find(m.hosts, name)
	}

	for _, s := range slices.Backward(m.screens) {
		if h, ok := s.(*host); ok {
			return h.machine, nil
		}
	}

	return -1, errors.New("which machine? e.g. :ctr web-1")
}

// View shows the top screen, every line cut to the window's width.
func (m *model) View() string {
	if m.reports == nil { // in the middle of the window, where the table will be
		probing := fmt.Sprintf("%s probing %d machines…", m.spin.View(), len(m.hosts))

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, probing)
	}

	lines := strings.Split(m.draw(m.top().view(m)), "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, m.width, "…")
	}

	return strings.Join(lines, "\n")
}

// The frame's top edge: left, the title, a space, fill, a space, the status, right.
const left, right = "╭─ ", " ─╮"

// draw draws a screen like k9s: its body in a rounded box, with the title on the
// box's top edge and the status at its end. The help line follows, unless the
// command bar or a message takes its place. The body is cut or padded to fill the
// box, so the box fills the window. A full screen is its body alone, but for the
// command bar or a message, on its last line.
func (m *model) draw(f frame) string {
	bar := m.bar()

	if f.full {
		lines := strings.Split(f.body, "\n")
		if bar != "" {
			lines[len(lines)-1] = bar
		}

		return strings.Join(lines, "\n")
	}

	inner, height := bodySize(m.width, m.height, false)
	inner, height = max(inner, 0), max(height, 0)
	edge := dimStyle.Render("│")

	var b strings.Builder

	// The title gives way first, the status is news; the fill is at least one wide.
	edges := ansi.StringWidth(left+right) + 2 // and the spaces around the fill
	title := ansi.Truncate(f.title, max(m.width-edges-1-ansi.StringWidth(f.status), 0), "…")
	fill := max(m.width-edges-ansi.StringWidth(title)-ansi.StringWidth(f.status), 1)
	b.WriteString(dimStyle.Render(left) + title + dimStyle.Render(" "+strings.Repeat("─", fill)+" ") + f.status + dimStyle.Render(right) + "\n")

	lines := strings.Split(f.body, "\n")
	for i := range height {
		line := ""
		if i < len(lines) {
			line = ansi.Truncate(lines[i], inner, "…")
		}

		b.WriteString(edge + line + strings.Repeat(" ", inner-ansi.StringWidth(line)) + edge + "\n")
	}

	b.WriteString(dimStyle.Render("╰"+strings.Repeat("─", inner)+"╯") + "\n")

	if bar == "" {
		bar = helpLine(f.help)
	}

	return b.String() + bar
}

// crumbs is a screen's title: sshwarm, then where the screen is, as in
// "sshwarm › web-1 › stack db".
func crumbs(where ...string) string {
	var title strings.Builder
	title.WriteString(pill("sshwarm"))
	for _, w := range where {
		title.WriteString(dimStyle.Render(" › ") + titleStyle.Render(w))
	}

	return title.String()
}

// bar is the action to confirm, or the command bar while it's open, or else the
// message if there is one.
func (m *model) bar() string {
	switch {
	case m.pending != nil:
		return toneStyles[warn].Render(fmt.Sprintf("%s %s? y/N", m.pending.verb, m.pending.what))
	case m.command.Focused():
		return m.command.View()
	case m.flash != "":
		return toneStyles[warn].Render(m.flash)
	}

	return ""
}

// refreshStatus says when the reports were last refreshed, or what's running.
func (m *model) refreshStatus() string {
	if m.running > 0 {
		return m.spin.View() + dimStyle.Render(fmt.Sprintf(" %d running", m.running))
	}

	if m.probes.running {
		return m.spin.View() + dimStyle.Render(" refreshing")
	}

	return dimStyle.Render("refreshed " + m.probes.last.Format(time.TimeOnly))
}

// Run browses the machines until the user quits; readOnly, without actions or shells.
func Run(ctx context.Context, hosts []inventory.Host, timeout, interval time.Duration, readOnly bool) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel() // however the TUI ends, no log stream or probe outlives it

	m := newModel(ctx, hosts, timeout, interval)
	m.readOnly = readOnly
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()

	return err
}
