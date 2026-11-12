package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// table is a screen listing the rows of one kind: the machines, or a machine's
// services or stacks.
type table struct {
	kind   *kind
	host   int             // index in the reports; -1 for the machines
	filter textinput.Model // on the rows' keys
	grid   grid
}

func newTable(k *kind, host int) *table {
	f := textinput.New()
	f.Prompt = "/"

	return &table{kind: k, host: host, filter: f, grid: newGrid(k.titles...)}
}

func (t *table) open(m *model) tea.Cmd {
	t.refresh(m)

	return nil
}

// refresh rebuilds the rows from the reports.
func (t *table) refresh(m *model) {
	t.grid.setRows(matching(t.kind.rows(m, t.host), t.filter.Value()))
}

// matching keeps the rows whose key contains query, ignoring case.
func matching(rows []row, query string) []row {
	q := strings.ToLower(query)

	return slices.DeleteFunc(rows, func(r row) bool { return !strings.Contains(strings.ToLower(r.key), q) })
}

func (t *table) resize(width, height int) {
	chrome := 2 // the title and help lines that view draws
	if t.host >= 0 {
		chrome = 3 // and the tabs
	}

	t.grid.width, t.grid.height = width, max(height-chrome, 3)
	t.grid.clamp()
}

func (t *table) key(m *model, k tea.KeyMsg) tea.Cmd {
	if t.filter.Focused() {
		return t.filterKey(m, k)
	}

	switch k.String() {
	case "esc":
		m.pop()
	case "tab":
		t.nextTab(m)
	case "a":
		m.all = !m.all
		m.rows()
	case "/":
		return t.filter.Focus()
	case "enter":
		if len(t.grid.rows) > 0 {
			return t.kind.enter(m, t.host, t.grid.rows[t.grid.cursor].key)
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

	t.refresh(m)

	return cmd
}

// nextTab shows the machine's next kind, if it has more than one.
func (t *table) nextTab(m *model) {
	if t.host < 0 {
		return
	}

	tabs := tabsOf(m.reports[t.host])
	if len(tabs) < 2 {
		return
	}

	t.kind = tabs[(slices.Index(tabs, t.kind)+1)%len(tabs)]
	t.grid = newGrid(t.kind.titles...)
	t.resize(m.width, m.height)
	t.refresh(m)
}

func (t *table) view(m *model) string {
	filter := ""
	if t.filter.Focused() || t.filter.Value() != "" {
		filter = "  " + t.filter.View()
	}

	if t.host < 0 {
		title := titleStyle.Render("sshwarm") + dimStyle.Render(fmt.Sprintf(" · %d machines · %s", len(m.reports), m.refreshStatus()))

		return frame(title+filter, t.grid.view(), "enter open · / filter · r refresh · q quit")
	}

	r := m.reports[t.host]
	title := titleStyle.Render(r.Host.Name) + dimStyle.Render(fmt.Sprintf(" · %s · up %s · %s", r.Version, short(r.Uptime), m.refreshStatus()))

	if r.Err != nil { // went down since we opened it
		return frame(title, toneStyles[bad].Render("down: "+r.Err.Error()), "r refresh · esc back · q quit")
	}

	var bar string

	for _, k := range tabsOf(r) {
		style := tabStyle
		if k == t.kind {
			style = onTabStyle
		}

		bar += style.Render(fmt.Sprintf("%s %d", k.name, len(matching(k.rows(m, t.host), t.filter.Value()))))
	}

	scope := "a all units"
	if m.all {
		scope = "a interesting units"
	}

	return frame(title, bar+filter+"\n"+t.grid.view(), "enter logs · tab switch · / filter · "+scope+" · r refresh · esc back · q quit")
}
