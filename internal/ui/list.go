package ui

import (
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// list is what the fleet and a machine's screen share: one kind's rows, filtered,
// sorted and scrolled, and the keys that act on the row under the cursor.
type list struct {
	kind   *kind
	filter textinput.Model // on the rows' keys
	grid   grid
}

func newList(k *kind) list {
	f := textinput.New()
	f.Prompt = "/"

	return list{kind: k, filter: f, grid: grid{titles: k.titles}}
}

// pick is the row under the cursor: its key, and its machine's report and index.
type pick struct {
	machine int
	r       swarm.Report
	key     string
}

// load shows rows: those whose key contains the filter, ignoring case.
func (l *list) load(rows []row) { l.grid.setRows(matching(rows, l.filter.Value())) }

// matching keeps the rows whose key contains query, ignoring case.
func matching(rows []row, query string) []row {
	q := strings.ToLower(query)

	return slices.DeleteFunc(rows, func(r row) bool { return !strings.Contains(strings.ToLower(r.key), q) })
}

// filterKey types into the filter: enter keeps it, esc clears it. The screen then
// loads its rows again.
func (l *list) filterKey(k tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch {
	case key.Matches(k, keyBack):
		l.filter.SetValue("")
		l.filter.Blur()
	case key.Matches(k, keyEnter):
		l.filter.Blur()
	default:
		l.filter, cmd = l.filter.Update(k)
	}

	return cmd
}

// key handles the keys every list takes; p is the row under the cursor, nil if none.
func (l *list) key(m *model, k tea.KeyMsg, p *pick) tea.Cmd {
	for _, a := range actions {
		if key.Matches(k, a.key) {
			l.ask(m, a.action, p)

			return nil
		}
	}

	switch {
	case key.Matches(k, keyBack):
		m.pop()
	case key.Matches(k, keyRefresh):
		return m.refresh()
	case key.Matches(k, keyFilter):
		return l.filter.Focus()
	case key.Matches(k, keyLogs):
		return l.show(m, p, l.kind.logs, nil)
	case key.Matches(k, keyDescribe):
		return l.show(m, p, l.kind.describe, l.kind.card)
	case key.Matches(k, keyShell):
		return l.openShell(m, p)
	case key.Matches(k, keySortName):
		l.grid.sortOn(0)
	case key.Matches(k, keySortStat):
		l.grid.sortOn(l.kind.status)
	case key.Matches(k, keySortAge):
		if l.kind.age > 0 {
			l.grid.sortOn(l.kind.age)
		}
	default:
		l.grid.key(k)
	}

	return nil
}

// ask asks to confirm a on p, if its kind has such an action. The row is set now: a
// refresh could move another under the cursor before it's confirmed.
func (l *list) ask(m *model, a action, p *pick) {
	if p == nil {
		return
	}

	if m.readOnly {
		m.flash = "read-only: no actions"

		return
	}

	a, ok := l.kind.act(p.r, a, p.key)
	if !ok {
		return
	}

	a.host = p.r.Host
	m.pending = &a
}

// openShell opens a shell on p's machine, or in its container. Not when read-only: a
// shell can do anything an action can.
func (l *list) openShell(m *model, p *pick) tea.Cmd {
	if p == nil || l.kind.shell == nil {
		return nil
	}

	if m.readOnly {
		m.flash = "read-only: no shells"

		return nil
	}

	return m.shell(p.r.Host, l.kind.shell(p.r, p.key))
}

// show runs the command that command gives for p, and shows its output over the
// list: logs, or with a card to draw it, a description. A nil command means the kind
// has none.
func (l *list) show(m *model, p *pick, command func(swarm.Report, string) (string, string), card cardFunc) tea.Cmd {
	if p == nil || command == nil {
		return nil
	}

	title, cmd := command(p.r, p.key)

	return m.push(newOutput(title, p.r.Host, cmd, card))
}

// help is the list's keys, for the help line: its kind's, then the grid's.
func (l *list) help(m *model) []key.Binding {
	help := slices.Clone(l.kind.keys)
	if !m.readOnly {
		help = append(help, l.kind.actionKeys...)
	}

	if l.grid.hidden() > 0 {
		help = append(help, keyScroll)
	}

	return append(help, keyColumn, keySort)
}

// filterView is the filter, for after the title, while it's typed or set.
func (l *list) filterView() string {
	if l.filter.Focused() || l.filter.Value() != "" {
		return " " + l.filter.View()
	}

	return ""
}
