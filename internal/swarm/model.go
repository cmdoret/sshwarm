package swarm

import (
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/x/ansi"
)

// Host is a machine sshwarm watches: its name, and how to reach it over ssh.
type Host struct {
	Name    string   // as the flake or hosts file names it, e.g. web-1
	Address string   // ssh destination, user@hostname
	Opts    []string // extra ssh options
}

// Report is what one probe found on one host. Err is set when the probe failed;
// Errors lists the probe's commands that failed while the rest worked.
type Report struct {
	Host          Host
	Err           error
	Errors        []string
	OS            string    // e.g. Debian GNU/Linux 12 (bookworm)
	Now           time.Time // the host's clock: ages are computed against it
	Uptime        time.Duration
	Deployed      time.Time // NixOS: the last deploy; zero elsewhere
	RebootPending bool
	Load          float64 // percent, -1 if unknown: the 1-minute load per CPU, past 100 when tasks wait
	Memory        float64 // percent in use (not available to programs), -1 if unknown
	Disk          float64 // percent used of the fullest local disk, -1 if unknown
	Revision      string  // NixOS: the git commit the system was built from, if set
	Units         []Unit
	Docker        string // the command that reaches docker there, e.g. "sudo -n docker"; "" without docker
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
	Name, State, Status string
	Since               time.Time // when it last started or stopped; zero if never, or unknown
}

// Stack is a compose project and its containers.
type Stack struct {
	Name       string
	Containers []Container
}

// Interesting says whether u is worth watching: failed, or enabled to run. The rest
// are static or inactive units: hundreds of them, rarely of interest.
func (u Unit) Interesting() bool { return u.Active == "failed" || u.UnitFileState == "enabled" }

// Failed returns the units in the failed state.
func (r Report) Failed() []Unit {
	var out []Unit

	for _, u := range r.Units {
		if u.Active == "failed" {
			out = append(out, u)
		}
	}

	return out
}

// StacksUp counts the stacks whose containers are all OK.
func (r Report) StacksUp() int {
	n := 0

	for _, s := range r.Stacks {
		if len(s.Bad()) == 0 {
			n++
		}
	}

	return n
}

// OK reports whether c is running and not unhealthy, or done with exit code 0
// (init containers). Status is docker's text, e.g. "Up 2 hours (healthy)" or
// "Exited (0) 3 hours ago".
func (c Container) OK() bool {
	switch c.State {
	case "running":
		return !strings.Contains(c.Status, "(unhealthy)")
	case "exited":
		return strings.HasPrefix(c.Status, "Exited (0)")
	}

	return false
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

// Bad returns the containers that aren't OK.
func (s Stack) Bad() []Container {
	var out []Container

	for _, c := range s.Containers {
		if !c.OK() {
			out = append(out, c)
		}
	}

	return out
}

// FullDisk is the use, in percent, from which the fullest disk is a problem.
const FullDisk = 90

// Problems lists what's wrong: the host is down, or failed commands, a disk nearly
// full, failed units, containers not OK.
func (r Report) Problems() []string {
	if r.Err != nil {
		return []string{"down: " + r.Err.Error()}
	}

	out := slices.Clone(r.Errors)

	if r.Disk >= FullDisk {
		out = append(out, fmt.Sprintf("disk %.0f%% full", r.Disk))
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

// Age is the time since t, on the host's clock.
func (r Report) Age(t time.Time) time.Duration { return r.Now.Sub(t) }

// clean makes remote text plain: it drops escape sequences, then any other
// control character but tabs and newlines.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || !unicode.IsControl(r) {
			return r
		}

		return -1
	}, ansi.Strip(s))
}

// oneLine flattens remote text for an error message or a table cell.
func oneLine(s string) string { return clean(strings.Join(strings.Fields(s), " ")) }
