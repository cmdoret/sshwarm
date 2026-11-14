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

	status, age int // the columns S and A sort on; 0 (the name) for none

	describe func(r swarm.Report, key string) (title, cmd string)      // nil: nothing to describe
	card     cardFunc                                                  // draws describe's output
	shell    func(r swarm.Report, key string) (remote string)          // "": a login shell; nil: no shell
	act      func(r swarm.Report, verb, key string) (what, cmd string) // start, stop, restart; nil: none
}

// action is a start, stop or restart of what a row is about: asked for with its key,
// then confirmed, then run.
type action struct {
	verb, done string // "restart", "restarted"
	what, cmd  string // once asked: "stack immich", and the command that does it
	host       swarm.Host
}

// actions are the keys that start, stop and restart, on the kinds that can (act).
var actions = map[string]action{
	"u":      {verb: "start", done: "started"}, // up
	"ctrl+k": {verb: "stop", done: "stopped"},  // k9s's kill
	"R":      {verb: "restart", done: "restarted"},
}

// actionsHelp is the actions' keys, for the help line.
const actionsHelp = "u start · ^k stop · R restart"

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

// tabsOf lists a machine's kinds, in tab order: stacks and containers only if it has
// some, so that a machine with docker but nothing in it looks like one without.
func tabsOf(r swarm.Report) []*kind {
	if len(r.Stacks) > 0 {
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
	status: 1,
	age:    2, // DEPLOY: what changed last, as services' AGE
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
	card:     machineCard,
}

// machineTitles head the machines table: `sshwarm status` prints all but PROBLEMS.
var machineTitles = []string{"MACHINE", "STATE", "DEPLOY", "UP", "REBOOT", "UNITS", "STACKS", "LOAD", "MEM", "DISK", "PROBLEMS"}

// machineRow is a machine's row, for both the TUI and `sshwarm status`. Only the
// fields needing attention are colored, unless the machine is down.
func machineRow(r swarm.Report) row {
	if r.Err != nil {
		cells := slices.Repeat([]cell{{text: "-", rank: -1}}, len(machineTitles)) // unknown: last, biggest first
		cells[0], cells[1], cells[len(cells)-1] = cell{text: r.Host.Name}, stateCell(bad, "down"), cell{text: r.Err.Error()}

		return row{tone: bad, key: r.Host.Name, cells: cells}
	}

	problems := r.Problems()

	state := stateCell(good, "ok")
	if len(problems) > 0 {
		state = stateCell(warn, "degraded")
	}

	deployed := ageCell(r, r.Deployed)

	reboot := cell{text: "no"}
	if r.RebootPending {
		reboot = cell{text: "yes", tone: warn, rank: 1}
	}

	// Units as stacks: how many of those the services tab lists are fine.
	watched := 0
	for _, u := range r.Units {
		if u.Interesting() {
			watched++
		}
	}

	failed := len(r.Failed())

	units := cell{text: fmt.Sprintf("%d/%d", watched-failed, watched), rank: float64(failed)}
	if failed > 0 {
		units.tone = bad
	}

	stackCell := cell{text: "-"}
	if r.Docker != "" {
		up := r.StacksUp()

		stackCell = cell{text: fmt.Sprintf("%d/%d", up, len(r.Stacks)), rank: float64(len(r.Stacks) - up)}
		if up < len(r.Stacks) {
			stackCell.tone = bad
		}
	}

	return row{key: r.Host.Name, cells: []cell{
		{text: r.Host.Name}, state, deployed, {text: short(r.Uptime), rank: r.Uptime.Seconds()}, reboot, units, stackCell,
		usageCell(r.Load), usageCell(r.Memory), usageCell(r.Disk),
		{text: strings.Join(problems, "; "), tone: bad},
	}}
}

// --- services ---------------------------------------------------------------

var services = &kind{
	names:  []string{"services", "svc"},
	help:   "enter logs · d describe",
	titles: []string{"UNIT", "ACTIVE", "SUB", "RESULT", "AGE", "DESCRIPTION"},
	status: 1,
	age:    4,
	rows:   serviceRows,
	logs:   func(_ swarm.Report, unit string) (string, string) { return unit, swarm.UnitLogs(unit) },
	describe: func(_ swarm.Report, unit string) (string, string) {
		return "describe " + unit, swarm.DescribeUnit(unit)
	},
	card: unitCard,
	act: func(_ swarm.Report, verb, unit string) (string, string) {
		return unit, swarm.UnitAction(verb, unit)
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
	age := ageCell(r, u.Since)

	result := cell{text: u.Result}
	if u.Result != "" && u.Result != "success" {
		result.tone = bad
	}

	state := unitTone(u.Active)
	line := row{key: u.Name, cells: []cell{dot(state, u.Name), {text: u.Active, tone: state, rank: float64(state)}, {text: u.Sub}, result, age, {text: u.Description}}}

	if state == bad || state == dim { // the whole row: failed stands out, inactive fades
		line.tone = state
	}

	return line
}

// unitTone rates a unit by its active state: failed is bad, changing is a warning,
// inactive is there, but nothing to see.
func unitTone(active string) tone {
	switch active {
	case "failed":
		return bad
	case "activating", "deactivating", "reloading", "refreshing":
		return warn
	case "active":
		return good
	}

	return dim
}

// --- stacks and containers --------------------------------------------------

var stacks = &kind{
	names:  []string{"stacks", "st"},
	help:   "enter containers · l logs · d describe",
	titles: []string{"STACK", "OK", "AGE", "PROBLEMS"},
	status: 1,
	age:    2, // its youngest container's: the time since anything in it started or stopped
	rows: func(m *model, host int, _ string) []row {
		r := m.reports[host]

		var rows []row
		for _, s := range r.Stacks {
			rows = append(rows, stackRow(r, s))
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
	card: stackCard,
	act: func(r swarm.Report, verb, stack string) (string, string) {
		return "stack " + stack, swarm.StackAction(r.Docker, verb, stack)
	},
}

// containers lists a machine's compose containers, or one stack's.
var containers = &kind{
	names:  []string{"containers", "ctr"},
	help:   "enter logs · d describe · s shell · J stack",
	titles: []string{"CONTAINER", "STACK", "STATE", "AGE", "STATUS"},
	status: 2,
	age:    3, // the time since it last started or stopped, as services' AGE
	rows: func(m *model, host int, stack string) []row {
		r := m.reports[host]

		var rows []row

		for _, s := range r.Stacks {
			if stack != "" && s.Name != stack {
				continue
			}

			for _, c := range s.Containers {
				rows = append(rows, containerRow(r, s.Name, c))
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
	card:  containerCard,
	shell: func(r swarm.Report, name string) string { return swarm.ContainerShell(r.Docker, name) },
	act: func(r swarm.Report, verb, name string) (string, string) {
		return "container " + name, swarm.ContainerAction(r.Docker, verb, name)
	},
}

func stackRow(r swarm.Report, s swarm.Stack) row {
	worst := good

	var problems []string

	for _, c := range s.Containers {
		t := containerTone(c)
		worst = max(worst, t)

		if t != good {
			problems = append(problems, c.Name+" "+c.Status)
		}
	}

	broken := len(s.Bad())
	ok := cell{text: fmt.Sprintf("%d/%d", len(s.Containers)-broken, len(s.Containers)), tone: worst, rank: float64(broken)}
	line := row{key: s.Name, cells: []cell{dot(worst, s.Name), ok, ageCell(r, s.Since()), {text: strings.Join(problems, "; ")}}}

	if worst == bad {
		line.tone = bad
	}

	return line
}

func containerRow(r swarm.Report, stack string, c swarm.Container) row {
	t := containerTone(c)
	line := row{key: c.Name, owner: stack, cells: []cell{
		dot(t, c.Name), {text: stack}, {text: c.State, tone: t, rank: float64(t)}, ageCell(r, c.Since), {text: c.Status},
	}}

	if t == bad {
		line.tone = bad
	}

	return line
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

// ageCell shows the time since t, on the host's clock, short, and sorts on it; "-"
// for the zero time, sorting last.
func ageCell(r swarm.Report, t time.Time) cell {
	if t.IsZero() {
		return cell{text: "-", rank: -1}
	}

	d := r.Age(t)

	return cell{text: short(d), rank: d.Seconds()}
}

// stateCell is a machine's state: it sorts on its tone, worst first.
func stateCell(t tone, text string) cell {
	c := dot(t, text)
	c.rank = float64(t)

	return c
}

// usageCell shows a percentage, in the describe card's colors (usageTone) but only
// once it needs attention, as the table's other fields; "-" if unknown.
func usageCell(percent float64) cell {
	if percent < 0 {
		return cell{text: "-", rank: -1}
	}

	c := cell{text: fmt.Sprintf("%.0f%%", percent), rank: percent}
	if t := usageTone(percent / 100); t != good {
		c.tone = t
	}

	return c
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
