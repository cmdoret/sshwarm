package ui

import (
	"slices"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// kind is a resource the tables list: its columns, its rows, and what enter does
// on one of them, found by its row's key.
type kind struct {
	name   string // the tab's name
	titles []string
	rows   func(m *model, host int) []row
	enter  func(m *model, host int, key string) tea.Cmd
}

// machines is the bottom table; a machine's own kinds are its tabs (tabsOf).
var machines = &kind{
	titles: machineTitles,
	rows: func(m *model, _ int) []row {
		var rows []row
		for _, r := range m.reports {
			rows = append(rows, machineRow(r))
		}

		return rows
	},
	enter: func(m *model, _ int, name string) tea.Cmd {
		i := slices.IndexFunc(m.reports, func(r swarm.Report) bool { return r.Host.Name == name })
		if i < 0 || m.reports[i].Err != nil {
			return nil // nothing to open on a machine that's down
		}

		return m.push(newTable(services, i))
	},
}

var services = &kind{
	name:   "services",
	titles: []string{"UNIT", "ACTIVE", "SUB", "RESULT", "AGE", "DESCRIPTION"},
	rows:   serviceRows,
	enter: func(m *model, host int, unit string) tea.Cmd {
		h := m.reports[host].Host

		return m.push(newText(h.Name+" · "+unit, h, swarm.UnitLogs(unit)))
	},
}

var stacks = &kind{
	name:   "stacks",
	titles: []string{"STACK", "OK", "PROBLEMS"},
	rows: func(m *model, host int) []row {
		var rows []row
		for _, s := range m.reports[host].Stacks {
			rows = append(rows, stackRow(s))
		}

		return rows
	},
	enter: func(m *model, host int, stack string) tea.Cmd {
		h := m.reports[host].Host

		return m.push(newText(h.Name+" · stack "+stack, h, swarm.StackLogs(stack)))
	},
}

// tabsOf lists a machine's kinds, in tab order: stacks only with docker.
func tabsOf(r swarm.Report) []*kind {
	if r.HasDocker {
		return []*kind{services, stacks}
	}

	return []*kind{services}
}

// serviceRows lists a machine's failed and enabled units, failed first; with m.all,
// every unit. The rest are static or inactive units: hundreds of them, rarely of
// interest.
func serviceRows(m *model, host int) []row {
	r := m.reports[host]

	var units []swarm.Unit

	for _, u := range r.Units {
		if m.all || u.Active == "failed" || u.UnitFileState == "enabled" {
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

	var rows []row
	for _, u := range units {
		rows = append(rows, unitRow(r, u))
	}

	return rows
}
