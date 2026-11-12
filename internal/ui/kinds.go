package ui

import (
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// kind is a resource the tables list: its columns, its rows, and what enter does
// on one of them, found by its row's key.
type kind struct {
	name   string // the tab's name
	titles []string
	widths func(width int) []int // every column's but the last, which takes the rest
	rows   func(m *model, host int) []row
	enter  func(m *model, host int, key string) tea.Cmd
}

// machines is the bottom table; a machine's own kinds are its tabs (tabsOf).
var machines = &kind{
	titles: machineTitles,
	widths: func(int) []int { return []int{22, 10, 13, 8, 4, 6, 6, 6, 6} },
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
	widths: func(w int) []int { return []int{max(w/3, 30), 8, 9, 10, 5} },
	rows:   serviceRows,
	enter: func(m *model, host int, unit string) tea.Cmd {
		h := m.reports[host].Host

		return m.push(newText(h.Name+" · "+unit, h, swarm.UnitLogs(h, unit)))
	},
}

var stacks = &kind{
	name:   "stacks",
	titles: []string{"STACK", "OK", "PROBLEMS"},
	widths: func(int) []int { return []int{18, 6} },
	rows: func(m *model, host int) []row {
		var rows []row
		for _, s := range m.reports[host].Stacks {
			rows = append(rows, stackRow(s))
		}

		return rows
	},
	enter: func(m *model, host int, stack string) tea.Cmd {
		h := m.reports[host].Host

		return m.push(newText(h.Name+" · stack "+stack, h, swarm.StackLogs(h, stack)))
	},
}

// tabsOf lists a machine's kinds, in tab order: stacks only with docker.
func tabsOf(r swarm.Report) []*kind {
	if r.HasDocker {
		return []*kind{services, stacks}
	}

	return []*kind{services}
}

// serviceRows lists a machine's interesting units, failed ones first: the failed,
// compose stacks and enabled services. With m.all, every unit.
func serviceRows(m *model, host int) []row {
	r := m.reports[host]

	var units []swarm.Unit

	for _, u := range r.Units {
		interesting := u.Active == "failed" || strings.HasPrefix(u.Name, "compose-") || u.UnitFileState == "enabled"
		if m.all || interesting {
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
