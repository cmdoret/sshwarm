package ui

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/swarm"
)

// The table shows ages short, counts units as stacks, and lists the problems below it.
func TestPrintStatus(t *testing.T) {
	r := fixture(t)
	var b strings.Builder
	if ok, err := PrintStatus(&b, []swarm.Report{r, {Host: inventory.Host{Name: "garage-node-neatus"}, Err: errTest}}); ok || err != nil {
		t.Errorf("all OK: %v, %v", ok, err)
	}
	out := b.String()
	rows := map[string]string{}
	for line := range strings.SplitSeq(out, "\n") {
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
		"app-node-argiope disk 93% full",
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

// As JSON: the table's values, not its text, and the problems; of a machine that's
// down, only what's known.
func TestPrintStatusJSON(t *testing.T) {
	var b strings.Builder

	ok, err := PrintStatusJSON(&b, []swarm.Report{fixture(t), {Host: inventory.Host{Name: "garage-node-neatus"}, Err: errTest}})
	if ok || err != nil {
		t.Errorf("all OK: %v, %v", ok, err)
	}

	var machines []map[string]any
	if err := json.Unmarshal([]byte(b.String()), &machines); err != nil {
		t.Fatalf("%v:\n%s", err, b.String())
	}

	up, down := machines[0], machines[1]

	got := fmt.Sprintf("%v %v %v %v %v %v %v %v %v", up["state"], up["deployed"], up["uptime_seconds"], up["reboot_pending"],
		up["units"], up["stacks"], up["load"], up["disk"], up["problems"].([]any)[0])
	if want := "degraded 2026-11-10T06:40:00Z 90061 true map[ok:2 total:3] map[ok:1 total:3] 12.5 93.1 disk 93% full"; got != want {
		t.Errorf("up:\n got %s\nwant %s", got, want)
	}

	if got := fmt.Sprint(down); got != "map[name:garage-node-neatus problems:[down: connection refused] state:down]" {
		t.Errorf("down: %s", got)
	}
}
