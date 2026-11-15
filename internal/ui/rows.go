package ui

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// The kinds' rows, from what the probe found: a cell per column, colored by how
// things are, and sorting on what they show.

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
	line := row{key: u.Name, cells: []cell{
		dot(state, u.Name),
		{text: u.Active, tone: state, rank: float64(state)},
		{text: u.Sub},
		result,
		restartsCell(u.Restarts, u.RecentRestart), age,
		{text: u.Description},
	}}

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

func stackRow(r swarm.Report, s swarm.Stack) row {
	worst := toneOf(s.Condition())

	var problems []string

	for _, c := range s.Containers {
		if t := c.Trouble(); t != "" {
			problems = append(problems, c.Name+" "+t)
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
		dot(t, c.Name),
		{text: stack},
		{text: c.State, tone: t, rank: float64(t)},
		restartsCell(c.Restarts, c.RecentRestart), ageCell(r, c.Since),
		{text: c.Status},
	}}

	if t == bad {
		line.tone = bad
	}

	return line
}

// restartsCell counts automatic restarts, sorting on them: a warning while the last
// is recent, as it stays all through a crash loop.
func restartsCell(n int, recent bool) cell {
	c := cell{text: strconv.Itoa(n), rank: float64(n)}
	if recent {
		c.tone = warn
	}

	return c
}

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
