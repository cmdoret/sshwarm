package swarm

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestTones(t *testing.T) {
	r := fixture(t)

	// A machine that's up but has problems: only the fields needing attention are colored.
	m := machineRow(r)
	if m.tone != inherit {
		t.Errorf("degraded machine row tone = %v", m.tone)
	}
	for i, want := range map[int]tone{1: warn, 2: warn, 6: warn, 7: bad, 8: bad, 3: inherit} {
		if m.cells[i].tone != want {
			t.Errorf("machine cell %d %q: tone %v, want %v", i, m.cells[i].text, m.cells[i].tone, want)
		}
	}
	if down := machineRow(Report{Host: Host{Name: "x"}, Err: errTest}); down.tone != bad {
		t.Error("down machine not red")
	}

	units := map[string]Unit{}
	for _, u := range r.Units {
		units[u.Name] = u
	}
	if u := unitRow(r, units["broken.service"]); u.tone != bad || u.cells[3].tone != bad {
		t.Errorf("failed unit: %+v", u)
	}
	if u := unitRow(r, units["glances.service"]); u.tone != inherit || u.cells[0].tone != good {
		t.Errorf("running unit: %+v", u)
	}
	if u := unitRow(r, Unit{Name: "x", Active: "activating"}); u.tone != inherit || u.cells[0].tone != warn {
		t.Errorf("starting unit: %+v", u)
	}
	if u := unitRow(r, Unit{Name: "x", Active: "inactive", Result: "success"}); u.tone != dim {
		t.Errorf("inactive unit: %+v", u)
	}

	stacks := map[string]row{}
	for _, s := range r.Stacks {
		stacks[s.Name] = stackRow(s)
	}
	if s := stacks["immich"]; s.tone != bad || s.cells[0].tone != bad {
		t.Errorf("unhealthy stack: %+v", s)
	}
	if s := stacks["utils"]; s.tone != inherit || s.cells[0].tone != good {
		t.Errorf("healthy stack (init container done): %+v", s)
	}
	starting := Stack{Name: "s", Containers: []Container{{State: "running", Status: "Up 3 seconds (health: starting)"}}}
	if s := stackRow(starting); s.tone != inherit || s.cells[0].tone != warn {
		t.Errorf("starting stack: %+v", s)
	}
}

// Colored cells and the selection bar keep every line the same width.
func TestGridRender(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	r := fixture(t)
	g := newGrid(machineTitles...)
	g.widths = []int{18, 10, 13, 8, 4, 6, 6, 6, 6, 30}
	g.height = 6
	g.SetRows([]row{machineRow(r), machineRow(Report{Host: Host{Name: "garage-node-neatus"}, Err: errTest})})
	g.Update(tea.KeyMsg{Type: tea.KeyDown})

	v := g.View()
	if !strings.Contains(v, "\x1b[31m") || !strings.Contains(v, "\x1b[33m") || !strings.Contains(v, "\x1b[7") {
		t.Errorf("expected red, yellow and the reversed selection in:\n%q", v)
	}
	lines := strings.Split(v, "\n")
	want := lipgloss.Width(lines[0])
	for i, l := range lines[:4] {
		if w := lipgloss.Width(l); w != want {
			t.Errorf("line %d is %d wide, header %d: %q", i, w, want, l)
		}
	}
}
