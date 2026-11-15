package ui

import (
	"slices"

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

// on names what, and r's machine: "stack immich on web-1".
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

var services = &kind{
	names:  []string{"services", "svc"},
	keys:   []key.Binding{as(keyEnter, "logs"), keyDescribe},
	titles: []string{"UNIT", "ACTIVE", "SUB", "RESULT", "RESTARTS", "AGE", "DESCRIPTION"},
	status: 1,
	age:    5,
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
	titles: []string{"CONTAINER", "STACK", "STATE", "RESTARTS", "AGE", "STATUS"},
	status: 2,
	age:    4, // the time since it last started or stopped, as services' AGE
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
