package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	tabStyle   = lipgloss.NewStyle().Padding(0, 1)
	onTabStyle = tabStyle.Reverse(true)
)

// table is a screen listing the rows of one kind: the machines, or a machine's
// services, stacks or containers.
type table struct {
	kind   *kind
	host   int             // index in the reports; unused for the machines
	stack  string          // containers: only this stack's; "" for all
	filter textinput.Model // on the rows' keys
	grid   grid
}

func newTable(k *kind, host int, stack string) *table {
	f := textinput.New()
	f.Prompt = "/"

	return &table{kind: k, host: host, stack: stack, filter: f, grid: grid{titles: k.titles}}
}

func (t *table) open(m *model) tea.Cmd {
	t.fill(m)

	return nil
}

// fill rebuilds the rows from the reports.
func (t *table) fill(m *model) {
	t.grid.setRows(matching(t.kind.rows(m, t.host, t.stack), t.filter.Value()))
}

// matching keeps the rows whose key contains query, ignoring case.
func matching(rows []row, query string) []row {
	q := strings.ToLower(query)

	return slices.DeleteFunc(rows, func(r row) bool { return !strings.Contains(strings.ToLower(r.key), q) })
}

func (t *table) resize(width, height int) {
	lines := chrome
	if t.kind != machines {
		lines++ // the tabs
	}

	t.grid.width, t.grid.height = width, max(height-lines, 3)
	t.grid.clamp()
}

func (t *table) key(m *model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.pop()
	case "tab":
		t.nextTab(m, 1)
	case "shift+tab":
		t.nextTab(m, -1)
	case "a":
		if t.kind == services {
			m.all = !m.all
			m.fillTables()
		}
	case "/":
		return t.filter.Focus()
	case "l":
		return t.logs(m)
	case "enter":
		if t.kind.enter == nil {
			return t.logs(m)
		}

		if key, ok := t.selected(); ok {
			return t.kind.enter(m, t.host, key)
		}
	default:
		t.grid.key(k)
	}

	return nil
}

// filterKey types into the filter: enter keeps it, esc clears it.
func (t *table) filterKey(m *model, k tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch k.String() {
	case "esc":
		t.filter.SetValue("")
		t.filter.Blur()
	case "enter":
		t.filter.Blur()
	default:
		t.filter, cmd = t.filter.Update(k)
	}

	t.fill(m)

	return cmd
}

// selected is the key of the row under the cursor, if there is one.
func (t *table) selected() (string, bool) {
	if len(t.grid.rows) == 0 {
		return "", false
	}

	return t.grid.rows[t.grid.cursor].key, true
}

// logs opens the logs of the row under the cursor, if its kind has any.
func (t *table) logs(m *model) tea.Cmd {
	key, ok := t.selected()
	if !ok || t.kind.logs == nil {
		return nil
	}

	r := m.reports[t.host]
	title, cmd := t.kind.logs(r, key)

	return m.push(newText(r.Host.Name+" · "+title, r.Host, cmd))
}

// nextTab shows the machine's next kind, or with step -1 the previous one, if it
// has more than one.
func (t *table) nextTab(m *model, step int) {
	if t.kind == machines {
		return
	}

	tabs := tabsOf(m.reports[t.host])
	if len(tabs) < 2 {
		return
	}

	t.kind, t.stack = tabs[(slices.Index(tabs, t.kind)+step+len(tabs))%len(tabs)], ""
	t.grid = grid{titles: t.kind.titles}
	t.resize(m.width, m.height)
	t.fill(m)
}

func (t *table) view(m *model) string {
	filter := ""
	if t.filter.Focused() || t.filter.Value() != "" {
		filter = "  " + t.filter.View()
	}

	if t.kind == machines {
		title := titleStyle.Render("sshwarm") + dimStyle.Render(fmt.Sprintf(" · %d machines · %s", len(m.reports), m.refreshStatus()))

		return m.frame(title+filter, t.grid.view(), t.kind.help+" · / filter · : go to · r refresh · q quit")
	}

	r := m.reports[t.host]

	name := r.Host.Name
	if t.stack != "" {
		name += " · stack " + t.stack
	}

	title := titleStyle.Render(name) + dimStyle.Render(fmt.Sprintf(" · %s · up %s · %s", r.OS, short(r.Uptime), m.refreshStatus()))

	if r.Err != nil {
		return m.frame(title, toneStyles[bad].Render("down: "+r.Err.Error()), "r refresh · esc back · q quit")
	}

	var tabs string

	for _, k := range tabsOf(r) {
		n, style := len(t.grid.rows), onTabStyle // the current tab: its rows, as shown
		if k != t.kind {
			n, style = len(matching(k.rows(m, t.host, ""), t.filter.Value())), tabStyle
		}

		tabs += style.Render(fmt.Sprintf("%s %d", k.names[0], n))
	}

	help := t.kind.help
	if t.kind == services {
		scope := "all"
		if m.all {
			scope = "interesting"
		}

		help += " · a " + scope + " units"
	}

	return m.frame(title, tabs+filter+"\n"+t.grid.view(), help+" · tab switch · / filter · : go to · esc back · q quit")
}
