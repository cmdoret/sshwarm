package ui

import (
	"errors"
	"os"
	"path/filepath"
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

// fakeBin puts an executable script called name first on PATH.
func fakeBin(t *testing.T, name, script string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

var errTest = errors.New("connection refused")
