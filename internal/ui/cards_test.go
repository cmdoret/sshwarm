package ui

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Each card draws its kind's recorded description within the width, even a narrow
// one, and leaves the environment's values out.
func TestCards(t *testing.T) {
	for name, tc := range map[string]struct {
		card cardFunc
		want []string
	}{
		"machine": {machineCard, []string{"System", "NixOS 26.05 (Yarara)", "6.12.4 · x86_64 · kvm", "4 CPUs", "Usage", "22%", "/ 7.7 GiB", "Disks", "/mnt/media files", "97%", "Processes", "immich"}},
		"unit":    {unitCard, []string{"backup.service", "failed (failed)", "exit-code", "last exit status 2", "on-failure · 3 restarts", "36.0 MiB / 512.0 MiB", "Recent logs", "connection refused"}},
		"stack":   {stackCard, []string{"Containers, 0 of 2 fine", "demo-job-1", "exited", "demo-web-1", "6.9%", "1.73MiB", "127.0.0.1:18080->80/tcp"}},
		"container": {containerCard, []string{"demo-web-1", "alpine:latest", "demo · service web", "running · health unhealthy", "of a CPU, 8 here",
			"1.82MiB / 15.24GiB", "failed checks in a row", "probe failed", "127.0.0.1:18080 → 80/tcp", "/srv/demo/data → /data (rw)", "DB_PASSWORD", "values hidden"}},
	} {
		raw, err := os.ReadFile("../swarm/testdata/" + name + ".describe")
		if err != nil {
			t.Fatal(err)
		}

		out, err := tc.card(string(raw), 100)
		if err != nil {
			t.Errorf("%s: %v", name, err)

			continue
		}

		for _, w := range tc.want {
			if !strings.Contains(out, w) {
				t.Errorf("%s: missing %q in:\n%s", name, w, out)
			}
		}

		if w := lipgloss.Width(out); w > 100 {
			t.Errorf("%s: %d columns, wider than 100:\n%s", name, w, out)
		}

		if narrow, _ := tc.card(string(raw), 60); lipgloss.Width(narrow) > 60 {
			t.Errorf("%s: wider than 60:\n%s", name, narrow)
		}

		// Without a width, nothing is cut: lines wider than the window scroll.
		if full, _ := tc.card(string(raw), 0); strings.Contains(full, "…") {
			t.Errorf("%s: cut without a width:\n%s", name, full)
		}

		if strings.Contains(out, "hunter2") {
			t.Errorf("%s: an environment value shows:\n%s", name, out)
		}

		if _, err := tc.card("ssh: connect to host x port 22: Connection refused", 80); err == nil {
			t.Errorf("%s: drew ssh's error as a card", name)
		}
	}
}

func TestSize(t *testing.T) {
	for b, want := range map[uint64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 5 << 30: "5.0 GiB", 3 << 40: "3.0 TiB"} {
		if got := size(b); got != want {
			t.Errorf("size(%d) = %q, want %q", b, got, want)
		}
	}
}
