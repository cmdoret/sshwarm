package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmdoret/sshwarm/internal/swarm"
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

	t.grid.width, t.grid.height = width-sides, max(height-lines, 3)
	t.grid.clamp()
}

func (t *table) key(m *model, k tea.KeyMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.pop()
	case "r":
		return m.refresh()
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
		return t.show(m, t.kind.logs, nil)
	case "d":
		return t.show(m, t.kind.describe, t.kind.card)
	case "s":
		if host, key, ok := t.selected(m); ok && t.kind.shell != nil {
			r := m.reports[host]

			return m.shell(r.Host, t.kind.shell(r, key))
		}
	case "enter":
		if t.kind.enter == nil {
			return t.show(m, t.kind.logs, nil)
		}

		if host, key, ok := t.selected(m); ok {
			return t.kind.enter(m, host, key)
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

// selected is the row under the cursor, if there is one: its key, and the index of
// its machine in the reports.
func (t *table) selected(m *model) (host int, key string, ok bool) {
	if len(t.grid.rows) == 0 {
		return -1, "", false
	}

	key = t.grid.rows[t.grid.cursor].key
	if t.kind != machines {
		return t.host, key, true
	}

	return slices.IndexFunc(m.reports, func(r swarm.Report) bool { return r.Host.Name == key }), key, true
}

// show runs the command that command gives for the row under the cursor, and shows
// its output over the table: logs, or with a card to draw it, a description. A nil
// command means the kind has none.
func (t *table) show(m *model, command func(swarm.Report, string) (string, string), card cardFunc) tea.Cmd {
	host, key, ok := t.selected(m)
	if !ok || command == nil {
		return nil
	}

	r := m.reports[host]
	title, cmd := command(r, key)

	return m.push(newText(title, r.Host, cmd, card))
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
		filter = " " + t.filter.View()
	}

	help := t.kind.help
	if t.grid.hidden() > 0 {
		help += " · ←→ scroll"
	}

	if t.kind == machines {
		title := crumbs(fmt.Sprintf("machines %d", len(m.reports))) + filter

		return m.frame(title, m.refreshStatus(), t.grid.view(), help+" · / filter · : go to · r refresh · q quit")
	}

	r := m.reports[t.host]

	where := []string{r.Host.Name}
	if t.stack != "" {
		where = append(where, "stack "+t.stack)
	}

	title := crumbs(where...) + dimStyle.Render(" · "+r.OS) + revision(r.Revision) + dimStyle.Render(" · up "+short(r.Uptime))

	if r.Err != nil {
		return m.frame(title, m.refreshStatus(), toneStyles[bad].Render("down: "+r.Err.Error()), "r refresh · esc back · q quit")
	}

	var tabs string

	for _, k := range tabsOf(r) {
		n := len(t.grid.rows) // the current tab: its rows, as shown
		if k != t.kind {
			n = len(matching(k.rows(m, t.host, ""), t.filter.Value()))
		}

		tab := fmt.Sprintf("%s %d", k.names[0], n)
		if k == t.kind {
			tabs += pill(tab) + " "
		} else {
			tabs += dimStyle.Render(" "+tab+" ") + " "
		}
	}

	if t.kind == services {
		scope := "all"
		if m.all {
			scope = "interesting"
		}

		help += " · a " + scope + " units"
	}

	return m.frame(title, m.refreshStatus(), tabs+filter+"\n"+t.grid.view(), help+" · tab switch · / filter · : go to · esc back · q quit")
}

// revision is " · rev <commit>" for the title, warning when the tree was dirty; ""
// without a revision (not NixOS, or the flake doesn't set it).
func revision(rev string) string {
	if rev == "" {
		return ""
	}

	style := dimStyle
	if strings.HasSuffix(rev, "-dirty") {
		style = toneStyles[warn]
	}

	return dimStyle.Render(" · rev ") + style.Render(shortRev(rev))
}
