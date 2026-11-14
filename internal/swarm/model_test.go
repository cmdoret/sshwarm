package swarm

import "testing"

// A container is OK running and not unhealthy, or exited with code 0, as init
// containers do; any other state isn't.
func TestContainerOK(t *testing.T) {
	for _, tt := range []struct {
		state, status string
		ok            bool
	}{
		{"running", "Up 2 hours", true},
		{"running", "Up 2 hours (healthy)", true},
		{"running", "Up 5 seconds (health: starting)", true},
		{"running", "Up 2 hours (unhealthy)", false},
		{"exited", "Exited (0) 2 hours ago", true},
		{"exited", "Exited (137) 5 minutes ago", false},
		{"restarting", "Restarting (1) 3 seconds ago", false},
		{"created", "Created", false},
		{"paused", "Up 2 hours (Paused)", false},
	} {
		if got := (Container{State: tt.state, Status: tt.status}).OK(); got != tt.ok {
			t.Errorf("%s, %q: OK %v, want %v", tt.state, tt.status, got, tt.ok)
		}
	}
}
