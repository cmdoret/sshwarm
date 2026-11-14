package ui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/swarm"
)

// kind is a resource the lists show: its names, columns and rows, and what enter, l,
// d, s and the action keys do on a row, found by its key.
type kind struct {
	names  []string      // the first is the tab's name; all of them work in the command bar
	keys   []key.Binding // its own keys, for the help line
	titles []string

	rows  func(r swarm.Report, s scope) []row                  // a machine's; nil for the machines: the fleet lists them
	drill *kind                                                // what enter opens on a row, of that row only; nil: its logs
	logs  func(r swarm.Report, key string) (title, cmd string) // nil: no logs

	status, age int // the columns S and A sort on; 0 (the name) for none

	describe func(r swarm.Report, key string) (title, cmd string) // nil: nothing to describe
	card     cardFunc                                             // draws describe's output
	shell    func(r swarm.Report, key string) (cmd string)        // "": a login shell; nil: no shell

	// act fills in what a, from actions, does to the row about key: what it acts on,
	// and the command. It's not ok if the kind has no such action.
	act        func(r swarm.Report, a action, key string) (action, bool)
	actionKeys []key.Binding // act's and the shell's, for the help line unless read-only
}

// scope is which of a machine's rows a list shows: every unit or the interesting
// ones; one stack's containers, or every container.
type scope struct {
	all   bool
	stack string
}

// action is a start, stop or restart (or reboot) of what a row is about: asked for
// with its key, then confirmed, then run.
type action struct {
	verb      swarm.Verb // what it does; on a machine, "reboot"
	done      string     // what's done: "restarted"
	what, cmd string     // once asked: "stack immich on web-1", and the command that does it
	host      inventory.Host
}

// actions are the keys that start, stop and restart what's under the cursor, if its
// kind can (act).
var actions = []struct {
	key key.Binding
	action
}{
	{keyStart, action{verb: swarm.Start, done: "started"}},
	{keyStop, action{verb: swarm.Stop, done: "stopped"}}, // k9s's kill
	{keyRestart, action{verb: swarm.Restart, done: "restarted"}},
}

// rowActionKeys are the action keys of what runs on a machine, for the help line.
var rowActionKeys = []key.Binding{keyStart, keyStop, keyRestart}

// on says where what is: on r's machine.
func on(r swarm.Report, what string) string { return what + " on " + r.Host.Name }

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
// some.
func tabsOf(r swarm.Report) []*kind {
	if len(r.Stacks) > 0 {
		return []*kind{services, stacks, containers}
	}

	return []*kind{services}
}

// --- machines ---------------------------------------------------------------

// machines are what the fleet lists; a machine's own kinds are its tabs (tabsOf).
var machines = &kind{
	names:    []string{"machines"},
	keys:     []key.Binding{as(keyEnter, "open"), keyDescribe},
	titles:   machineTitles,
	status:   1,
	age:      2, // DEPLOY: what changed last, as services' AGE
	shell:    func(swarm.Report, string) string { return "" },
	describe: func(swarm.Report, string) (string, string) { return "describe", swarm.DescribeMachine() },
	card:     machineCard,
	act: func(r swarm.Report, a action, _ string) (action, bool) {
		if a.verb != swarm.Restart { // can't be started over ssh, nor started again once stopped
			return action{}, false
		}

		return action{verb: "reboot", done: "rebooting", what: r.Host.Name, cmd: swarm.Reboot()}, true
	},
	actionKeys: []key.Binding{as(keyShell, "ssh"), as(keyRestart, "reboot")},
}

// machineTitles head the machines table: `sshwarm status` prints all but PROBLEMS.
var machineTitles = []string{"MACHINE", "STATE", "DEPLOY", "UP", "REBOOT", "UNITS", "STACKS", "LOAD", "MEM", "DISK", "PROBLEMS"}

// machineRow is a machine's row, for both the TUI and `sshwarm status`. Only the
// fields needing attention are colored, unless the machine is down.
func machineRow(r swarm.Report) row {
	if r.Err != nil {
		cells := slices.Repeat([]cell{{text: "-", rank: -1}}, len(machineTitles)) // unknown: last, biggest first
		cells[0], cells[1], cells[len(cells)-1] = cell{text: r.Host.Name}, stateCell(machineState(r)), cell{text: r.Err.Error()}

		return row{tone: bad, key: r.Host.Name, cells: cells}
	}

	problems := r.Problems()
	state := stateCell(machineState(r))
	deployed := ageCell(r, r.Deployed)

	reboot := cell{text: "no"}
	if r.RebootPending {
		reboot = cell{text: "yes", tone: warn, rank: 1}
	}

	unitCount, stackCount := counts(r)
	units, stacks := countCell(unitCount), cell{text: "-"}

	if stackCount != nil {
		stacks = countCell(*stackCount)
	}

	return row{key: r.Host.Name, cells: []cell{
		{text: r.Host.Name},
		state, deployed,
		{text: short(r.Uptime), rank: r.Uptime.Seconds()},
		reboot, units, stacks,
		usageCell(r.Load), usageCell(r.Memory), usageCell(r.Disk),
		{text: strings.Join(problems, "; "), tone: bad},
	}}
}

// --- services ---------------------------------------------------------------

var services = &kind{
	names:  []string{"services", "svc"},
	keys:   []key.Binding{as(keyEnter, "logs"), keyDescribe},
	titles: []string{"UNIT", "ACTIVE", "SUB", "RESULT", "AGE", "DESCRIPTION"},
	status: 1,
	age:    4,
	rows:   func(r swarm.Report, s scope) []row { return serviceRows(r, s.all) },
	logs:   func(_ swarm.Report, unit string) (string, string) { return unit, swarm.UnitLogs(unit) },
	describe: func(_ swarm.Report, unit string) (string, string) {
		return "describe " + unit, swarm.DescribeUnit(unit)
	},
	card: unitCard,
	act: func(r swarm.Report, a action, unit string) (action, bool) {
		a.what, a.cmd = on(r, unit), swarm.UnitAction(a.verb, unit)

		return a, true
	},
	actionKeys: rowActionKeys,
}

// serviceRows lists r's interesting units, failed first, then by name ignoring
// case, as column sorts do; with all, every unit.
func serviceRows(r swarm.Report, all bool) []row {
	var units []swarm.Unit

	for _, u := range r.Units {
		if all || u.Interesting() {
			units = append(units, u)
		}
	}

	sort.SliceStable(units, func(i, j int) bool {
		fi, fj := units[i].Condition() == swarm.Broken, units[j].Condition() == swarm.Broken
		if fi != fj {
			return fi
		}

		return strings.ToLower(units[i].Name) < strings.ToLower(units[j].Name)
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

	state := toneOf(u.Condition())
	line := row{key: u.Name, cells: []cell{dot(state, u.Name), {text: u.Active, tone: state, rank: float64(state)}, {text: u.Sub}, result, age, {text: u.Description}}}

	if state == bad || state == dim { // the whole row: failed stands out, inactive fades
		line.tone = state
	}

	return line
}

// toneOf colors a condition: idle fades, the others say how things are.
func toneOf(c swarm.Condition) tone {
	switch c {
	case swarm.Idle:
		return dim
	case swarm.Warning:
		return warn
	case swarm.Broken:
		return bad
	}

	return good
}

// --- stacks and containers --------------------------------------------------

var stacks = &kind{
	names:  []string{"stacks", "st"},
	keys:   []key.Binding{as(keyEnter, "containers"), keyLogs, keyDescribe},
	titles: []string{"STACK", "OK", "AGE", "PROBLEMS"},
	status: 1,
	age:    2, // its youngest container's: the time since anything in it started or stopped
	rows: func(r swarm.Report, _ scope) []row {
		var rows []row
		for _, s := range r.Stacks {
			rows = append(rows, stackRow(r, s))
		}

		return rows
	},
	drill: containers,
	logs: func(r swarm.Report, stack string) (string, string) {
		return "stack " + stack, r.Docker.StackLogs(stack)
	},
	describe: func(r swarm.Report, stack string) (string, string) {
		return "describe stack " + stack, r.Docker.DescribeStack(stack)
	},
	card: stackCard,
	act: func(r swarm.Report, a action, stack string) (action, bool) {
		a.what, a.cmd = on(r, "stack "+stack), r.Docker.StackAction(a.verb, stack)

		return a, true
	},
	actionKeys: rowActionKeys,
}

// containers lists a machine's compose containers, or one stack's.
var containers = &kind{
	names:  []string{"containers", "ctr"},
	keys:   []key.Binding{as(keyEnter, "logs"), keyDescribe, keyOwner},
	titles: []string{"CONTAINER", "STACK", "STATE", "AGE", "STATUS"},
	status: 2,
	age:    3, // the time since it last started or stopped, as services' AGE
	rows: func(r swarm.Report, sc scope) []row {
		var rows []row

		for _, s := range r.Stacks {
			if sc.stack != "" && s.Name != sc.stack {
				continue
			}

			for _, c := range s.Containers {
				rows = append(rows, containerRow(r, s.Name, c))
			}
		}

		return rows
	},
	logs: func(r swarm.Report, name string) (string, string) {
		return "container " + name, r.Docker.ContainerLogs(name)
	},
	describe: func(r swarm.Report, name string) (string, string) {
		return "describe container " + name, r.Docker.DescribeContainer(name)
	},
	card:  containerCard,
	shell: func(r swarm.Report, name string) string { return r.Docker.ContainerShell(name) },
	act: func(r swarm.Report, a action, name string) (action, bool) {
		a.what, a.cmd = on(r, "container "+name), r.Docker.ContainerAction(a.verb, name)

		return a, true
	},
	actionKeys: append([]key.Binding{keyShell}, rowActionKeys...),
}

func stackRow(r swarm.Report, s swarm.Stack) row {
	worst := toneOf(s.Condition())

	var problems []string

	for _, c := range s.Containers {
		if c.Condition() != swarm.Fine {
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
	t := toneOf(c.Condition())
	line := row{key: c.Name, owner: stack, cells: []cell{
		dot(t, c.Name), {text: stack}, {text: c.State, tone: t, rank: float64(t)}, ageCell(r, c.Since), {text: c.Status},
	}}

	if t == bad {
		line.tone = bad
	}

	return line
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

// count is how many of something are fine, of how many.
type count struct {
	OK    int `json:"ok"`
	Total int `json:"total"`
}

// counts says how many of r's units, those the services tab lists, and of its
// stacks are fine; no stacks without docker. The machines table and status's JSON
// show them.
func counts(r swarm.Report) (units count, stacks *count) {
	watched := r.Watched()
	units = count{OK: watched - len(r.Failed()), Total: watched}

	if r.Docker != "" {
		stacks = &count{OK: r.StacksUp(), Total: len(r.Stacks)}
	}

	return units, stacks
}

// countCell shows c as "fine/all": bad unless all are, sorting on those that aren't.
func countCell(c count) cell {
	cl := cell{text: fmt.Sprintf("%d/%d", c.OK, c.Total), rank: float64(c.Total - c.OK)}
	if c.OK < c.Total {
		cl.tone = bad
	}

	return cl
}

// machineState is a machine's condition, and how the STATE column words it.
func machineState(r swarm.Report) (tone, string) {
	c := r.Condition()

	switch c {
	case swarm.Broken:
		return toneOf(c), "down"
	case swarm.Warning:
		return toneOf(c), "degraded"
	}

	return toneOf(c), "ok"
}

// stateCell is a machine's state: it sorts on its tone, worst first.
func stateCell(t tone, text string) cell {
	c := dot(t, text)
	c.rank = float64(t)

	return c
}

// usageCell shows a percentage, in the describe card's colors (usageTone) but only
// once it needs attention, as the table's other fields; "-" if unknown.
func usageCell(p *float64) cell {
	if p == nil {
		return cell{text: "-", rank: -1}
	}

	percent := *p

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
