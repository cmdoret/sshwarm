package ui

import (
	"errors"
	"os"
	"testing"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// fixture is swarm's test machine: a failed unit, an unhealthy stack, a pending reboot.
func fixture(t *testing.T) swarm.Report {
	t.Helper()
	raw, err := os.ReadFile("../swarm/testdata/argiope.probe")
	if err != nil {
		t.Fatal(err)
	}
	r, err := swarm.Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	r.Host = swarm.Host{Name: "app-node-argiope", Address: "argiope"}
	return r
}

var errTest = errors.New("connection refused")
