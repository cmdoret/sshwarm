package ui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// kind is a resource the tables list: its names, columns and rows, and what enter,
// l, d and s do on a row, found by its key.
type kind struct {
	names  []string // the first is the tab's name; all of them work in the command bar
	help   string   // its own keys, for the help line
	titles []string
	rows   func(m *model, host int, stack string) []row
	enter  func(m *model, host int, key string) tea.Cmd         // the next level; nil: the logs
	logs   func(r swarm.Report, key string) (title, cmd string) // nil: no logs

	describe func(r swarm.Report, key string) (title, cmd string) // nil: nothing to describe
	shell    func(r swarm.Report, key string) (remote string)     // "": a login shell; nil: no shell
}

// kinds are the command bar's resources.
var kinds = []*kind{machines, services, stacks, containers}

// kindNamed is the kind a command bar name means, or nil.
func kindNamed(name string) *kind {
	i := slices.IndexFunc(kinds, func(k *kind) bool { return slices.Contains(k.names, name) })
	if i < 0 {
		return nil
	}

	return kinds[i]
}

// tabsOf lists a machine's kinds, in tab order: stacks and containers only with docker.
func tabsOf(r swarm.Report) []*kind {
	if r.Docker != "" {
		return []*kind{services, stacks, containers}
	}

	return []*kind{services}
}

// --- machines ---------------------------------------------------------------

// machines is the bottom table; a machine's own kinds are its tabs (tabsOf).
var machines = &kind{
	names:  []string{"machines"},
	help:   "enter open · d describe · s ssh",
	titles: machineTitles,
	rows: func(m *model, _ int, _ string) []row {
		var rows []row
		for _, r := range m.reports {
			rows = append(rows, machineRow(r))
		}

		return rows
	},
	enter:    func(m *model, host int, _ string) tea.Cmd { return m.push(newTable(services, host, "")) },
	shell:    func(swarm.Report, string) string { return "" },
	describe: func(swarm.Report, string) (string, string) { return "describe", swarm.DescribeMachine() },
}

// machineTitles head the machines table: `sshwarm status` prints all but PROBLEMS.
var machineTitles = []string{"MACHINE", "STATE", "DEPLOY", "UP", "REBOOT", "UNITS", "STACKS", "PROBLEMS"}

// machineRow is a machine's row, for both the TUI and `sshwarm status`. Only the
// fields needing attention are colored, unless the machine is down.
func machineRow(r swarm.Report) row {
	if r.Err != nil {
		cells := slices.Repeat([]cell{{text: "-"}}, len(machineTitles))
		cells[0], cells[1], cells[len(cells)-1] = cell{text: r.Host.Name}, dot(bad, "down"), cell{text: r.Err.Error()}

		return row{tone: bad, key: r.Host.Name, cells: cells}
	}

	problems := r.Problems()

	state := dot(good, "ok")
	if len(problems) > 0 {
		state = dot(warn, "degraded")
	}

	deployed := cell{text: "-"}
	if !r.Deployed.IsZero() {
		deployed.text = short(r.Age(r.Deployed))
	}

	reboot := cell{text: "no"}
	if r.RebootPending {
		reboot = cell{text: "yes", tone: warn}
	}

	// Units as stacks: how many of those the services tab lists are fine.
	watched := 0
	for _, u := range r.Units {
		if u.Interesting() {
			watched++
		}
	}

	failed := len(r.Failed())

	units := cell{text: fmt.Sprintf("%d/%d", watched-failed, watched)}
	if failed > 0 {
		units.tone = bad
	}

	stackCell := cell{text: "-"}
	if r.Docker != "" {
		up := r.StacksUp()

		stackCell.text = fmt.Sprintf("%d/%d", up, len(r.Stacks))
		if up < len(r.Stacks) {
			stackCell.tone = bad
		}
	}

	return row{key: r.Host.Name, cells: []cell{
		{text: r.Host.Name}, state, deployed, {text: short(r.Uptime)}, reboot, units, stackCell,
		{text: strings.Join(problems, "; "), tone: bad},
	}}
}

// --- services ---------------------------------------------------------------

var services = &kind{
	names:  []string{"services", "svc"},
	help:   "enter logs · d describe",
	titles: []string{"UNIT", "ACTIVE", "SUB", "RESULT", "AGE", "DESCRIPTION"},
	rows:   serviceRows,
	logs:   func(_ swarm.Report, unit string) (string, string) { return unit, swarm.UnitLogs(unit) },
	describe: func(_ swarm.Report, unit string) (string, string) {
		return "describe " + unit, swarm.DescribeUnit(unit)
	},
}

// serviceRows lists a machine's interesting units, failed first; with m.all, every
// unit.
func serviceRows(m *model, host int, _ string) []row {
	r := m.reports[host]

	var units []swarm.Unit

	for _, u := range r.Units {
		if m.all || u.Interesting() {
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

func unitRow(r swarm.Report, u swarm.Unit) row {
	age := "-"
	if !u.Since.IsZero() {
		age = short(r.Age(u.Since))
	}

	result := cell{text: u.Result}
	if u.Result != "" && u.Result != "success" {
		result.tone = bad
	}

	cells := func(state tone) []cell {
		return []cell{dot(state, u.Name), {text: u.Active, tone: state}, {text: u.Sub}, result, {text: age}, {text: u.Description}}
	}

	switch u.Active {
	case "failed":
		return row{tone: bad, cells: cells(bad), key: u.Name}
	case "activating", "deactivating", "reloading", "refreshing":
		return row{cells: cells(warn), key: u.Name}
	case "active":
		return row{cells: cells(good), key: u.Name}
	}

	return row{tone: dim, cells: cells(dim), key: u.Name} // inactive: there, but nothing to see
}

// --- stacks and containers --------------------------------------------------

var stacks = &kind{
	names:  []string{"stacks", "st"},
	help:   "enter containers · l logs · d describe",
	titles: []string{"STACK", "OK", "PROBLEMS"},
	rows: func(m *model, host int, _ string) []row {
		var rows []row
		for _, s := range m.reports[host].Stacks {
			rows = append(rows, stackRow(s))
		}

		return rows
	},
	enter: func(m *model, host int, stack string) tea.Cmd { return m.push(newTable(containers, host, stack)) },
	logs: func(r swarm.Report, stack string) (string, string) {
		return "stack " + stack, swarm.StackLogs(r.Docker, stack)
	},
	describe: func(r swarm.Report, stack string) (string, string) {
		return "describe stack " + stack, swarm.DescribeStack(r.Docker, stack)
	},
}

// containers lists a machine's compose containers, or one stack's.
var containers = &kind{
	names:  []string{"containers", "ctr"},
	help:   "enter logs · d describe · s shell",
	titles: []string{"CONTAINER", "STACK", "STATE", "STATUS"},
	rows: func(m *model, host int, stack string) []row {
		var rows []row

		for _, s := range m.reports[host].Stacks {
			if stack != "" && s.Name != stack {
				continue
			}

			for _, c := range s.Containers {
				rows = append(rows, containerRow(s.Name, c))
			}
		}

		return rows
	},
	logs: func(r swarm.Report, name string) (string, string) {
		return "container " + name, swarm.ContainerLogs(r.Docker, name)
	},
	describe: func(r swarm.Report, name string) (string, string) {
		return "describe container " + name, swarm.DescribeContainer(r.Docker, name)
	},
	shell: func(r swarm.Report, name string) string { return swarm.ContainerShell(r.Docker, name) },
}

func stackRow(s swarm.Stack) row {
	worst := good

	var problems []string

	for _, c := range s.Containers {
		t := containerTone(c)
		worst = max(worst, t)

		if t != good {
			problems = append(problems, c.Name+" "+c.Status)
		}
	}

	ok := fmt.Sprintf("%d/%d", len(s.Containers)-len(s.Bad()), len(s.Containers))
	r := row{key: s.Name, cells: []cell{dot(worst, s.Name), {text: ok, tone: worst}, {text: strings.Join(problems, "; ")}}}

	if worst == bad {
		r.tone = bad
	}

	return r
}

func containerRow(stack string, c swarm.Container) row {
	t := containerTone(c)
	r := row{key: c.Name, cells: []cell{dot(t, c.Name), {text: stack}, {text: c.State, tone: t}, {text: c.Status}}}

	if t == bad {
		r.tone = bad
	}

	return r
}

// containerTone rates c: restarting is a crash loop, so bad; starting is fine for now.
func containerTone(c swarm.Container) tone {
	switch {
	case !c.OK():
		return bad
	case strings.Contains(c.Status, "health: starting"), c.State == "created", c.State == "paused":
		return warn
	}

	return good
}

// --- formats ----------------------------------------------------------------

// short formats a duration as its largest unit: 45s, 12m, 5h, 3d.
func short(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	}

	return strconv.Itoa(int(d.Hours()/24)) + "d"
}

// shortRev keeps 7 characters of a commit, and its -dirty suffix.
func shortRev(rev string) string {
	rev, dirty := strings.CutSuffix(rev, "-dirty")
	rev = rev[:min(len(rev), 7)]

	if dirty {
		rev += "-dirty"
	}

	return rev
}
