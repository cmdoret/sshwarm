package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"

	tea "github.com/charmbracelet/bubbletea"
)

// fleet is the bottom screen: a row per machine. Enter opens a machine's screen.
type fleet struct{ list }

func newFleet() *fleet { return &fleet{newList(machines, false)} }

func (f *fleet) open(m *model) tea.Cmd {
	f.fill(m)

	return nil
}

// fill lists the machines.
func (f *fleet) fill(m *model) {
	var rows []row

	for i, r := range m.reports {
		rw := machineRow(r)
		rw.machine = i
		rows = append(rows, rw)
	}

	f.load(rows)
}

func (f *fleet) update(m *model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return f.key(m, msg)
	case refillMsg:
		f.fill(m)
	}

	return nil
}

func (f *fleet) key(m *model, k tea.KeyMsg) tea.Cmd {
	if f.filter.Focused() {
		cmd := f.filterKey(k)
		f.fill(m)

		return cmd
	}

	if !key.Matches(k, keyEnter) {
		return f.list.key(m, k, f.pick(m))
	}

	if p := f.pick(m); p != nil {
		return m.push(newHost(services, p.machine, ""))
	}

	return nil
}

func (f *fleet) resize(width, height int) {
	w, h := bodySize(width, height, false)
	f.grid.width, f.grid.height = w, max(h, 3)
	f.grid.clamp()
}

func (f *fleet) view(m *model) frame {
	return frame{
		title:  crumbs(fmt.Sprintf("machines %d", len(m.reports))) + f.filterView(),
		status: m.refreshStatus(),
		body:   f.grid.view(),
		help:   append(f.help(m), keyFilter, keyRefresh),
		moves:  gridMoves,
	}
}

func (f *fleet) close()       {}
func (f *fleet) busy() bool   { return false }
func (f *fleet) typing() bool { return f.filter.Focused() }
