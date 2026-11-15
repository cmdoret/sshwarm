package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"

	tea "github.com/charmbracelet/bubbletea"
)

// host is a machine's screen: a tab per kind it has (tabsOf), listing its services,
// stacks or containers, or one stack's containers. With machine -1, it lists every
// machine's (list.every).
type host struct {
	list
	machine int    // its index in the reports; -1 for every machine
	stack   string // containers: only this stack's; "" for all
}

func newHost(k *kind, machine int, stack string) *host {
	return &host{list: newList(k, machine < 0), machine: machine, stack: stack}
}

func (h *host) open(m *model) tea.Cmd {
	h.fill(m)

	return nil
}

// fill lists the rows of the kind on screen.
func (h *host) fill(m *model) { h.load(h.rowsOf(m, h.kind, scope{all: m.all, stack: h.stack})) }

// rowsOf is k's rows in scope sc: the machine's, or every machine's. Those get a
// MACHINE column after the first, and keys and owners of "<machine>/<name>", which
// pick undoes: rows of different machines can have the same name.
func (h *host) rowsOf(m *model, k *kind, sc scope) []row {
	if !h.every {
		rows := k.rows(m.reports[h.machine], sc)
		for i := range rows {
			rows[i].machine = h.machine
		}

		return rows
	}

	var rows []row

	for i, r := range m.reports {
		for _, rw := range k.rows(r, sc) {
			rw.cells = slices.Insert(rw.cells, 1, cell{text: r.Host.Name})
			rw.key, rw.machine = r.Host.Name+"/"+rw.key, i

			if rw.owner != "" {
				rw.owner = r.Host.Name + "/" + rw.owner
			}

			rows = append(rows, rw)
		}
	}

	return rows
}

func (h *host) update(m *model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return h.key(m, msg)
	case refillMsg:
		h.fill(m)
	}

	return nil
}

func (h *host) key(m *model, k tea.KeyMsg) tea.Cmd {
	if h.filter.Focused() {
		cmd := h.filterKey(k)
		h.fill(m)

		return cmd
	}

	switch {
	case key.Matches(k, keyNextTab):
		h.nextTab(m, 1)
	case key.Matches(k, keyPrevTab):
		h.nextTab(m, -1)
	case key.Matches(k, keyAll):
		if h.kind == services {
			m.all = !m.all

			return m.refill()
		}
	case key.Matches(k, keyOwner):
		h.owner(m)
	case key.Matches(k, keyEnter):
		if h.kind.drill == nil {
			return h.show(m, h.pick(m), h.kind.logs, nil)
		}

		if p := h.pick(m); p != nil {
			return m.push(newHost(h.kind.drill, p.machine, p.key))
		}
	default:
		return h.list.key(m, k, h.pick(m))
	}

	return nil
}

// tabs are the kinds the screen has a tab for: its machine's, or every machine's,
// with stacks and containers if any has some.
func (h *host) tabs(m *model) []*kind {
	if !h.every {
		return tabsOf(m.reports[h.machine])
	}

	for _, r := range m.reports {
		if len(r.Stacks) > 0 {
			return tabsOf(r)
		}
	}

	return []*kind{services}
}

// nextTab shows the next kind, or with step -1 the previous one, if there's more
// than one.
func (h *host) nextTab(m *model, step int) {
	tabs := h.tabs(m)
	if len(tabs) < 2 {
		return
	}

	h.switchTo(m, tabs[(slices.Index(tabs, h.kind)+step+len(tabs))%len(tabs)])
}

// switchTo shows kind k on this screen, as its tab does.
func (h *host) switchTo(m *model, k *kind) {
	h.kind, h.stack = k, ""
	h.grid = grid{titles: h.titles()}
	h.resize(m.width, m.height)
	h.fill(m)
}

// owner jumps from a container to its stack: back to the stack's screen if the
// container was listed from there, or else to the stacks tab, unfiltered so that the
// stack is in it.
func (h *host) owner(m *model) {
	if h.stack != "" {
		m.pop()

		return
	}

	if h.kind != containers || len(h.grid.rows) == 0 {
		return
	}

	stack := h.grid.rows[h.grid.cursor].owner

	h.filter.SetValue("")
	h.switchTo(m, stacks)
	h.grid.choose(stack)
}

func (h *host) resize(width, height int) {
	w, bodyHeight := bodySize(width, height, false)
	h.grid.width, h.grid.height = w, max(bodyHeight-1, 3) // under the tabs
	h.grid.clamp()
}

func (h *host) view(m *model) frame {
	if !h.every && m.reports[h.machine].Err != nil {
		return frame{
			title: h.title(m), status: m.refreshStatus(),
			body: toneStyles[bad].Render("down: " + m.reports[h.machine].Err.Error()),
			help: []key.Binding{keyRefresh, keyBack},
		}
	}

	var tabs strings.Builder

	for _, k := range h.tabs(m) {
		n := len(h.grid.rows) // the current tab: its rows, as shown
		if k != h.kind {
			n = len(matching(h.rowsOf(m, k, scope{all: m.all}), h.filter.Value()))
		}

		tab := fmt.Sprintf("%s %d", k.names[0], n)
		if k == h.kind {
			tabs.WriteString(pill(tab) + " ")
		} else {
			tabs.WriteString(dimStyle.Render(" "+tab+" ") + " ")
		}
	}

	help := h.help(m)

	if h.kind == services {
		all := keyAll
		if m.all {
			all = as(keyAll, "interesting units")
		}

		help = append(help, all)
	}

	return frame{
		title: h.title(m), status: m.refreshStatus(),
		body: tabs.String() + h.filterView() + "\n" + h.grid.view(),
		help: append(help, keyTab, keyFilter, keyBack), moves: gridMoves,
	}
}

// title is where the screen is, then its machine's OS, revision and uptime; for
// every machine, how many are down, as their rows are missing.
func (h *host) title(m *model) string {
	if h.every {
		down := 0

		for _, r := range m.reports {
			if r.Err != nil {
				down++
			}
		}

		title := crumbs("all machines")
		if down > 0 {
			title += toneStyles[bad].Render(fmt.Sprintf(" · %d down", down))
		}

		return title
	}

	r := m.reports[h.machine]

	where := []string{r.Host.Name}
	if h.stack != "" {
		where = append(where, "stack "+h.stack)
	}

	return crumbs(where...) + dimStyle.Render(" · "+r.OS) + revision(r.Revision) + dimStyle.Render(" · up "+short(r.Uptime))
}

func (h *host) close()       {}
func (h *host) busy() bool   { return false }
func (h *host) typing() bool { return h.filter.Focused() }

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
