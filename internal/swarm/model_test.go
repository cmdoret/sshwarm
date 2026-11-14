package swarm

import (
	"errors"
	"testing"
)

// A container is fine running and not unhealthy, or done with exit code 0, as init
// containers are; a warning on its way or held; else broken. Its status text, as
// docker words it, plays no part.
func TestContainerCondition(t *testing.T) {
	for _, tt := range []struct {
		state, health string
		code          int
		want          Condition
	}{
		{"running", "", 0, Fine},
		{"running", "healthy", 0, Fine},
		{"running", "starting", 0, Warning},
		{"running", "unhealthy", 0, Broken},
		{"exited", "", 0, Fine},
		{"exited", "", 137, Broken},
		{"restarting", "", 1, Broken},
		{"created", "", 0, Warning},
		{"paused", "healthy", 0, Warning},
		{"removing", "", 0, Warning},
		{"dead", "", 0, Broken},
	} {
		c := Container{State: tt.state, Health: tt.health, ExitCode: tt.code, Status: "Up 2 hours (unhealthy)"}
		if got := c.Condition(); got != tt.want {
			t.Errorf("%s, health %q, exit %d: %v, want %v", tt.state, tt.health, tt.code, got, tt.want)
		}
	}
}

// A unit is broken failed, a warning changing, fine active, and else idle; a
// machine is broken down, a warning with problems, else fine.
func TestCondition(t *testing.T) {
	for active, want := range map[string]Condition{"failed": Broken, "activating": Warning, "active": Fine, "inactive": Idle} {
		if got := (Unit{Active: active}).Condition(); got != want {
			t.Errorf("unit %s: %v, want %v", active, got, want)
		}
	}

	for r, want := range map[*Report]Condition{
		{Err: errors.New("refused")}:   Broken,
		{Errors: []string{"x: oops"}}:  Warning,
		{Disk: new(float64(FullDisk))}: Warning,
		{Disk: new(12.0)}:              Fine,
	} {
		if got := r.Condition(); got != want {
			t.Errorf("%+v: %v, want %v", *r, got, want)
		}
	}
}
