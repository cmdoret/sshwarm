package swarm

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type view int

const (
	listView view = iota // all machines
	hostView             // one machine: its services and stacks
	logsView
)

const maxLogLines = 5000

type (
	hostsMsg struct {
		hosts []Host
		err   error
	}
	reportsMsg  []Report
	tickMsg     int // refresh generation, so a manual refresh doesn't double the timer
	logLinesMsg struct {
		ch    <-chan string // the stream they come from
		lines []string
		done  bool
	}
)

type model struct {
	ctx               context.Context // cancelled when sshwarm exits: ends every ssh it started
	flake             string
	timeout, interval time.Duration

	hosts      []Host
	reports    []Report
	err        error
	refreshing bool
	refreshed  time.Time
	tickGen    int

	view      view
	host      int  // index in reports
	stacksTab bool // host view: stacks instead of services
	all       bool // services: show every unit, not only the interesting ones
	filter    textinput.Model

	machines, services, stacks grid

	logs      viewport.Model
	logTitle  string
	logRemote string // the log command, without follow
	logLines  []string
	logCh     <-chan string // the current stream, nil if none
	logCancel context.CancelFunc
	follow    bool
}

func newModel(ctx context.Context, flake string, timeout, interval time.Duration) model {
	f := textinput.New()
	f.Prompt = "/"
	return model{
		ctx: ctx, flake: flake, timeout: timeout, interval: interval, filter: f,
		machines: newGrid(machineTitles...),
		services: newGrid("UNIT", "ACTIVE", "SUB", "RESULT", "AGE", "DESCRIPTION"),
		stacks:   newGrid("STACK", "OK", "PROBLEMS"),
		logs:     viewport.New(0, 0),
	}
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		h, err := LoadHosts(m.ctx, m.flake)
		return hostsMsg{h, err}
	}
}

func (m *model) refresh() tea.Cmd {
	if m.refreshing || m.hosts == nil {
		return nil
	}
	m.refreshing = true
	ctx, hosts, timeout := m.ctx, m.hosts, m.timeout
	return func() tea.Msg { return reportsMsg(ProbeAll(ctx, hosts, timeout, true)) }
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.layout(msg.Width, msg.Height)
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
		return m.appendLog(msg)
	case tea.KeyMsg:
		return m.key(msg)
	}
	return m, nil
}

// key handles the keys every view shares, then hands over to the view's own.
func (m model) key(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := k.String()
	if s == "ctrl+c" || s == "q" && !m.filter.Focused() {
		return m, tea.Quit // RunTUI's context then ends the log stream
	}

	if m.filter.Focused() {
		return m.filterKey(k)
	}

	if s == "r" {
		return m, m.refresh()
	}

	switch m.view {
	case hostView:
		return m.hostKey(k)
	case logsView:
		return m.logsKey(k)
	default:
		return m.listKey(k)
	}
}

// filterKey types into the filter: enter keeps it, esc clears it.
func (m model) filterKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.filter.SetValue("")
		fallthrough
	case "enter":
		m.filter.Blur()
		m.rows()

		return m, nil
	}

	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(k)
	m.rows()

	return m, cmd
}

// listKey moves in the machines table; enter opens a machine that's up.
func (m model) listKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	if k.String() != "enter" {
		m.machines.Update(k)

		return m, nil
	}

	i := m.machines.cursor
	if i >= len(m.reports) || m.reports[i].Err != nil {
		return m, nil
	}

	m.host, m.view, m.stacksTab = i, hostView, false
	m.services.SetCursor(0)
	m.stacks.SetCursor(0)
	m.rows()

	return m, nil
}

func (m model) hostKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.view = listView
		m.filter.SetValue("")
	case "tab":
		m.stacksTab = !m.stacksTab && m.reports[m.host].HasDocker
		m.rows()
	case "a":
		m.all = !m.all
		m.rows()
	case "/":
		m.filter.Focus()

		return m, textinput.Blink
	case "enter":
		return m.openLogs()
	default:
		m.tab().Update(k)
	}

	return m, nil
}

func (m model) logsKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch k.String() {
	case "esc":
		m.stopLog()
		m.view = hostView
	case "f":
		return m.startLog(!m.follow)
	case "g", "home":
		m.logs.GotoTop()
	case "G", "end":
		m.logs.GotoBottom()
	default:
		m.logs, cmd = m.logs.Update(k)
	}

	return m, cmd
}

// tab is the host view's current table.
func (m *model) tab() *grid {
	if m.stacksTab {
		return &m.stacks
	}
	return &m.services
}

func (m model) openLogs() (tea.Model, tea.Cmd) {
	g := m.tab()
	if len(g.rows) == 0 {
		return m, nil
	}
	name, host := g.rows[g.cursor].key, m.reports[m.host].Host.Name
	m.logTitle, m.logRemote = host+" · "+name, unitLogs(name)
	if m.stacksTab {
		m.logTitle, m.logRemote = host+" · stack "+name, stackLogs(name)
	}
	m.view = logsView
	return m.startLog(false)
}

func (m model) startLog(follow bool) (tea.Model, tea.Cmd) {
	m.stopLog()
	ctx, cancel := context.WithCancel(m.ctx)
	m.follow, m.logCancel = follow, cancel
	m.logs.SetContent("loading…")
	m.logCh = streamLines(ctx, m.reports[m.host].Host, logCmd(m.logRemote, follow), follow)
	return m, waitLog(m.logCh)
}

// stopLog ends the log stream and drops its lines.
func (m *model) stopLog() {
	if m.logCancel != nil {
		m.logCancel()
	}
	m.logCancel, m.logCh, m.logLines = nil, nil, nil
}

func (m model) appendLog(msg logLinesMsg) (tea.Model, tea.Cmd) {
	if msg.ch != m.logCh {
		return m, nil // from a stream we already left
	}
	bottom := m.logs.AtBottom() || len(m.logLines) == 0
	m.logLines = append(m.logLines, msg.lines...)
	if len(m.logLines) > maxLogLines {
		m.logLines = m.logLines[len(m.logLines)-maxLogLines:]
	}
	if msg.done && m.follow {
		m.logLines = append(m.logLines, "[stream ended]")
	}
	m.setLogContent()
	if bottom {
		m.logs.GotoBottom()
	}
	if msg.done {
		return m, nil
	}
	return m, waitLog(m.logCh)
}

// setLogContent wraps long lines to the window instead of cutting them off.
func (m *model) setLogContent() {
	m.logs.SetContent(lipgloss.NewStyle().Width(m.logs.Width).Render(strings.Join(m.logLines, "\n")))
}

// waitLog delivers the next lines of a stream, gathered for up to 50 ms, so a
// busy stream doesn't re-render the logs for every line.
func waitLog(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return logLinesMsg{ch: ch, done: true}
		}
		lines := []string{line}
		batch := time.After(50 * time.Millisecond)
		for len(lines) < 1000 {
			select {
			case l, ok := <-ch:
				if !ok {
					return logLinesMsg{ch: ch, lines: lines, done: true}
				}
				lines = append(lines, l)
			case <-batch:
				return logLinesMsg{ch: ch, lines: lines}
			}
		}
		return logLinesMsg{ch: ch, lines: lines}
	}
}

// rows fills the tables from the reports, the filter and the toggles.
func (m *model) rows() {
	var rows []row
	for _, r := range m.reports {
		rows = append(rows, machineRow(r))
	}
	m.machines.SetRows(rows)
	if m.host >= len(m.reports) {
		return
	}
	r := m.reports[m.host]
	q := strings.ToLower(m.filter.Value())

	var units []Unit
	for _, u := range r.Units {
		interesting := u.Active == "failed" || strings.HasPrefix(u.Name, "compose-") || u.UnitFileState == "enabled"
		if (m.all || interesting) && strings.Contains(strings.ToLower(u.Name), q) {
			units = append(units, u)
		}
	}
	sort.SliceStable(units, func(i, j int) bool {
		fi, fj := units[i].Active == "failed", units[j].Active == "failed"
		if fi != fj {
			return fi
		}
		return units[i].Name < units[j].Name
	})
	rows = nil
	for _, u := range units {
		rows = append(rows, unitRow(r, u))
	}
	m.services.SetRows(rows)

	rows = nil
	for _, s := range r.Stacks {
		if strings.Contains(strings.ToLower(s.Name), q) {
			rows = append(rows, stackRow(s))
		}
	}
	m.stacks.SetRows(rows)
}

// layout sizes the tables and the viewport to the window; the last column takes the rest.
func (m *model) layout(width, height int) {
	// around: the lines above and below a table (title, help, the host view's tabs).
	fit := func(g *grid, around int, widths ...int) {
		copy(g.widths, widths)
		last := len(g.widths) - 1
		rest := width - 2 // the last column's padding
		for _, w := range g.widths[:last] {
			rest -= w + 2
		}
		g.widths[last] = max(rest, 10)
		g.height = max(height-around, 3)
		g.clamp()
	}
	fit(&m.machines, 2, 22, 10, 13, 8, 4, 6, 6, 6, 6)
	fit(&m.services, 3, max(width/3, 30), 8, 9, 10, 5)
	fit(&m.stacks, 3, 18, 6)
	m.logs.Width, m.logs.Height = width, max(height-2, 1)
	m.setLogContent()
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

	switch m.view {
	case hostView:
		return m.hostScreen()
	case logsView:
		return m.logsScreen()
	default:
		return m.listScreen()
	}
}

// screen stacks a view: a title line, the body, a help line.
func screen(title, body, help string) string {
	return title + "\n" + body + "\n" + dimStyle.Render(help)
}

// refreshStatus says when the reports were last refreshed.
func (m model) refreshStatus() string {
	if m.refreshing {
		return "refreshing…"
	}

	return "refreshed " + m.refreshed.Format(time.TimeOnly)
}

func (m model) listScreen() string {
	title := titleStyle.Render("sshwarm") + dimStyle.Render(fmt.Sprintf(" · %d machines · %s", len(m.reports), m.refreshStatus()))

	return screen(title, m.machines.View(), "enter open · r refresh · q quit")
}

func (m model) hostScreen() string {
	r := m.reports[m.host]
	title := titleStyle.Render(r.Host.Name) + dimStyle.Render(fmt.Sprintf(" · %s · up %s · gen %d · %s", r.Version, short(r.Uptime), r.Generation, m.refreshStatus()))

	if r.Err != nil { // went down since we opened it
		return screen(title, toneStyles[bad].Render("down: "+r.Err.Error()), "r refresh · esc back · q quit")
	}

	svc, stk := onTabStyle, tabStyle
	if m.stacksTab {
		svc, stk = tabStyle, onTabStyle
	}

	tabs := svc.Render(fmt.Sprintf("services %d", len(m.services.rows)))
	if r.HasDocker {
		tabs += stk.Render(fmt.Sprintf("stacks %d", len(m.stacks.rows)))
	}

	if m.filter.Focused() || m.filter.Value() != "" {
		tabs += "  " + m.filter.View()
	}

	scope := "a all units"
	if m.all {
		scope = "a interesting units"
	}

	return screen(title, tabs+"\n"+m.tab().View(), "enter logs · tab switch · / filter · "+scope+" · r refresh · esc back · q quit")
}

func (m model) logsScreen() string {
	title := titleStyle.Render(m.logTitle) + dimStyle.Render(fmt.Sprintf(" · %3.f%%", m.logs.ScrollPercent()*100))

	follow := "f follow"
	if m.follow {
		follow = "f stop following"
	}

	return screen(title, m.logs.View(), follow+" · g/G top/bottom · esc back · q quit")
}

func RunTUI(flake string, timeout, interval time.Duration) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // however the TUI ends, no log stream or probe outlives it
	_, err := tea.NewProgram(newModel(ctx, flake, timeout, interval), tea.WithAltScreen()).Run()
	return err
}
