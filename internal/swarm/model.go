package swarm

import (
	"slices"
	"strings"
	"time"
	"unicode"
)

// Host is a machine sshwarm watches: its name, and how to reach it over ssh.
type Host struct {
	Name    string   // as the flake names it, e.g. web-1
	Address string   // ssh destination, user@hostname
	Opts    []string // extra ssh options
}

// Report is what one probe found on one host. Err is set when the probe failed;
// Errors lists the probe's commands that failed while the rest worked.
type Report struct {
	Host          Host
	Err           error
	Errors        []string
	Now           time.Time // the host's clock: ages are computed against it
	Uptime        time.Duration
	Deployed      time.Time
	RebootPending bool
	Version       string
	Revision      string // git commit the system was built from, "" if unknown
	Units         []Unit
	HasDocker     bool
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
}

// Stack is a compose project and its containers.
type Stack struct {
	Name       string
	Containers []Container
}

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

// Problems lists what's wrong: the host is down, or failed commands, failed
// units, containers not OK.
func (r Report) Problems() []string {
	if r.Err != nil {
		return []string{"down: " + r.Err.Error()}
	}

	out := slices.Clone(r.Errors)

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

// clean drops control characters from remote text, except tabs and newlines, so
// it can't send escape sequences (clipboard, title, links) to the terminal.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || !unicode.IsControl(r) {
			return r
		}

		return -1
	}, s)
}

// oneLine flattens remote text for an error message or a table cell.
func oneLine(s string) string { return clean(strings.Join(strings.Fields(s), " ")) }
