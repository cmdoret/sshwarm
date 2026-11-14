package ui

import (
	"strings"
	"testing"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// The table shows ages short, counts units as stacks, and lists the problems below it.
func TestPrintStatus(t *testing.T) {
	r := fixture(t)
	var b strings.Builder
	if PrintStatus(&b, []swarm.Report{r, {Host: swarm.Host{Name: "garage-node-neatus"}, Err: errTest}}) {
		t.Error("status reported all OK")
	}
	out := b.String()
	rows := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) > 0 && rows[f[0]] == "" { // the table, not the problems below
			rows[f[0]] = strings.Join(f, " ")
		}
	}
	for name, want := range map[string]string{
		"MACHINE":            "MACHINE STATE DEPLOY UP REBOOT UNITS STACKS LOAD MEM DISK",
		"app-node-argiope":   "app-node-argiope degraded 2h 25h yes 2/3 1/3 12% 74% 93%",
		"garage-node-neatus": "garage-node-neatus down - - - - - - - -",
	} {
		if rows[name] != want {
			t.Errorf("row %q:\n got %q\nwant %q", name, rows[name], want)
		}
	}
	for _, want := range []string{
		"app-node-argiope failed: broken.service",
		"app-node-argiope stack immich: immich_server running (Up 2 hours (unhealthy))",
		"app-node-argiope stack cook: cook-cookcli-1 exited (Exited (1) 5 minutes ago)",
		"garage-node-neatus down: connection refused",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
