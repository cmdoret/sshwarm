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
	kind  *kind
	host  int    // index in the reports; -1 for the machines
	query string // the filter, on the rows' keys
	grid  grid
}

func newTable(k *kind, host int) *table {
	return &table{kind: k, host: host, grid: newGrid(k.titles...)}
}

func (t *table) open(m *model) tea.Cmd {
	t.refresh(m)

	return nil
}

// refresh rebuilds the rows from the reports.
func (t *table) refresh(m *model) {
	t.grid.SetRows(matching(t.kind.rows(m, t.host), t.query))
}

// matching keeps the rows whose key contains query, ignoring case.
func matching(rows []row, query string) []row {
	q := strings.ToLower(query)

	return slices.DeleteFunc(rows, func(r row) bool { return !strings.Contains(strings.ToLower(r.key), q) })
}

func (t *table) resize(width, height int) {
	around := 2 // the title and help lines
	if t.host >= 0 {
		around = 3 // and the tabs
	}

	g := &t.grid
	copy(g.widths, t.kind.widths(width))

	last := len(g.widths) - 1
	rest := width - 2 // the last column's padding

	for _, w := range g.widths[:last] {
		rest -= w + 2
	}

	g.widths[last] = max(rest, 10)
	g.height = max(height-around, 3)
	g.clamp()
}

func (t *table) key(m *model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.pop()
	case "tab":
		t.nextTab(m)
	case "a":
		m.all = !m.all
		m.rows()
	case "/":
		m.filter.Focus()

		return textinput.Blink
	case "enter":
		if len(t.grid.rows) > 0 {
			return t.kind.enter(m, t.host, t.grid.rows[t.grid.cursor].key)
		}
	default:
		t.grid.Update(k)
	}

	return nil
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
	if m.filter.Focused() || t.query != "" {
		filter = "  " + m.filter.View()
	}

	if t.host < 0 {
		title := titleStyle.Render("sshwarm") + dimStyle.Render(fmt.Sprintf(" · %d machines · %s", len(m.reports), m.refreshStatus()))

		return frame(title+filter, t.grid.View(), "enter open · / filter · r refresh · q quit")
	}

	r := m.reports[t.host]
	title := titleStyle.Render(r.Host.Name) + dimStyle.Render(fmt.Sprintf(" · %s · up %s · gen %d · %s", r.Version, short(r.Uptime), r.Generation, m.refreshStatus()))

	if r.Err != nil { // went down since we opened it
		return frame(title, toneStyles[bad].Render("down: "+r.Err.Error()), "r refresh · esc back · q quit")
	}

	var bar string

	for _, k := range tabsOf(r) {
		style := tabStyle
		if k == t.kind {
			style = onTabStyle
		}

		bar += style.Render(fmt.Sprintf("%s %d", k.name, len(matching(k.rows(m, t.host), t.query))))
	}

	scope := "a all units"
	if m.all {
		scope = "a interesting units"
	}

	return frame(title, bar+filter+"\n"+t.grid.View(), "enter logs · tab switch · / filter · "+scope+" · r refresh · esc back · q quit")
}
