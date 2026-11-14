package swarm

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/remote"
)

// Docker is the command that reaches docker on a host, as the probe found it, e.g.
// "sudo -n docker".
type Docker string

// Report is what one probe found on one host. Err is set when the probe failed;
// Errors lists the probe's commands that failed while the rest worked.
type Report struct {
	Host          inventory.Host
	Err           error
	Errors        []string
	OS            string    // e.g. Debian GNU/Linux 12 (bookworm)
	Now           time.Time // the host's clock: ages are computed against it
	Uptime        time.Duration
	Deployed      time.Time // NixOS: the last deploy; zero elsewhere
	RebootPending bool
	Load          *float64 // percent, nil if unknown: the 1-minute load per CPU, past 100 when tasks wait
	Memory        *float64 // percent in use (not available to programs), nil if unknown
	Disk          *float64 // percent used of the fullest local disk, nil if unknown
	Revision      string   // NixOS: the git commit the system was built from, if set
	Units         []Unit
	Docker        Docker // "" without docker
	Stacks        []Stack
}

// Unit is a systemd service on a host.
type Unit struct {
	Name, Active, Sub, Description string
	Result, UnitFileState          string
	Since                          time.Time // last state change; zero if never
}

// Container is one container of a compose stack.
type Container struct {
	Name, State string    // State: running, exited, created...
	Status      string    // as docker words it, e.g. "Up 2 hours (healthy)"; "" if unknown
	Health      string    // its healthcheck's: healthy, unhealthy or starting; "" without one
	ExitCode    int       // its last exit's
	Since       time.Time // when it last started or stopped; zero if never, or unknown
}

// Stack is a compose project and its containers.
type Stack struct {
	Name       string
	Containers []Container
}

// Condition is how something on a machine is doing, from idle to broken.
type Condition int

// The conditions, from the best to the worst but for Idle, which is neither.
const (
	Idle    Condition = iota // there, but doing nothing: an inactive unit
	Fine                     // running, or done well
	Warning                  // on its way, or held: starting, paused; a machine with problems
	Broken                   // failed, crashed, unhealthy; a machine that's down
)

// Condition rates u by its active state: failed is broken, changing a warning,
// active fine, and anything else idle.
func (u Unit) Condition() Condition {
	switch u.Active {
	case "failed":
		return Broken
	case "activating", "deactivating", "reloading", "refreshing":
		return Warning
	case "active":
		return Fine
	}

	return Idle
}

// Interesting says whether u is worth watching: broken, or enabled to run. The rest
// are static or inactive units: hundreds of them, rarely of interest.
func (u Unit) Interesting() bool { return u.Condition() == Broken || u.UnitFileState == "enabled" }

// Failed returns the units in the failed state.
func (r Report) Failed() []Unit {
	var out []Unit

	for _, u := range r.Units {
		if u.Condition() == Broken {
			out = append(out, u)
		}
	}

	return out
}

// Watched counts the units worth watching (Unit.Interesting): those the services
// tab lists.
func (r Report) Watched() int {
	n := 0

	for _, u := range r.Units {
		if u.Interesting() {
			n++
		}
	}

	return n
}

// StacksUp counts the stacks with no broken container.
func (r Report) StacksUp() int {
	n := 0

	for _, s := range r.Stacks {
		if len(s.Bad()) == 0 {
			n++
		}
	}

	return n
}

// Condition rates c: fine running and not unhealthy, or done with exit code 0, as
// init containers are; a warning on its way or held: health starting, created,
// paused, being removed; else broken: crashed, restarting (a crash loop), dead.
func (c Container) Condition() Condition {
	switch c.State {
	case "running":
		switch c.Health {
		case "unhealthy":
			return Broken
		case "starting":
			return Warning
		}

		return Fine
	case "exited":
		if c.ExitCode == 0 {
			return Fine
		}
	case "created", "paused", "removing":
		return Warning
	}

	return Broken
}

// Condition is the worst of s's containers' conditions.
func (s Stack) Condition() Condition {
	worst := Fine
	for _, c := range s.Containers {
		worst = max(worst, c.Condition())
	}

	return worst
}

// Since is when anything in s last started or stopped: its youngest container's
// Since, or the zero time.
func (s Stack) Since() time.Time {
	var last time.Time

	for _, c := range s.Containers {
		if c.Since.After(last) {
			last = c.Since
		}
	}

	return last
}

// Bad returns the containers that are broken.
func (s Stack) Bad() []Container {
	var out []Container

	for _, c := range s.Containers {
		if c.Condition() == Broken {
			out = append(out, c)
		}
	}

	return out
}

// FullDisk is the use, in percent, from which the fullest disk is a problem.
const FullDisk = 90

// Problems lists what's wrong: the host is down, or failed commands, a disk nearly
// full, failed units, broken containers.
func (r Report) Problems() []string {
	if r.Err != nil {
		return []string{"down: " + r.Err.Error()}
	}

	out := slices.Clone(r.Errors)

	if r.Disk != nil && *r.Disk >= FullDisk {
		out = append(out, fmt.Sprintf("disk %.0f%% full", *r.Disk))
	}

	if failed := r.Failed(); len(failed) > 0 {
		var names []string
		for _, u := range failed {
			names = append(names, u.Name)
		}

		out = append(out, "failed: "+strings.Join(names, ", "))
	}

	for _, s := range r.Stacks {
		for _, c := range s.Bad() {
			out = append(out, "stack "+s.Name+": "+c.Name+" "+c.State+" ("+c.Status+")")
		}
	}

	return out
}

// Condition rates r's machine: broken when it's down, a warning when it has
// problems, else fine.
func (r Report) Condition() Condition {
	switch {
	case r.Err != nil:
		return Broken
	case len(r.Problems()) > 0:
		return Warning
	}

	return Fine
}

// Age is the time since t, on the host's clock.
func (r Report) Age(t time.Time) time.Duration { return r.Now.Sub(t) }

// oneLine flattens remote text for an error message or a table cell.
func oneLine(s string) string { return remote.Clean(strings.Join(strings.Fields(s), " ")) }
