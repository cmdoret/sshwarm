package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

func TestTones(t *testing.T) {
	r := fixture(t)

	// A machine that's up but has problems: only the fields needing attention are colored.
	m := machineRow(r)
	if m.tone != inherit {
		t.Errorf("degraded machine row tone = %v", m.tone)
	}
	for i, want := range map[int]tone{1: warn, 2: inherit, 3: inherit, 4: warn, 5: bad, 6: bad, 7: inherit, 8: warn, 9: bad, 10: bad} {
		if m.cells[i].tone != want {
			t.Errorf("machine cell %d %q: tone %v, want %v", i, m.cells[i].text, m.cells[i].tone, want)
		}
	}
	if down := machineRow(swarm.Report{Host: swarm.Host{Name: "x"}, Err: errTest}); down.tone != bad {
		t.Error("down machine not red")
	}
	if state := machineRow(swarm.Report{Errors: []string{"version: not found"}}).cells[1].text; state != "degraded" {
		t.Errorf("a failed probe command: state %q", state)
	}

	units := map[string]swarm.Unit{}
	for _, u := range r.Units {
		units[u.Name] = u
	}
	if u := unitRow(r, units["broken.service"]); u.tone != bad || u.cells[3].tone != bad {
		t.Errorf("failed unit: %+v", u)
	}
	if u := unitRow(r, units["glances.service"]); u.tone != inherit || u.cells[0].tone != good {
		t.Errorf("running unit: %+v", u)
	}
	if u := unitRow(r, swarm.Unit{Name: "x", Active: "activating"}); u.tone != inherit || u.cells[0].tone != warn {
		t.Errorf("starting unit: %+v", u)
	}
	if u := unitRow(r, swarm.Unit{Name: "x", Active: "inactive", Result: "success"}); u.tone != dim {
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
	starting := swarm.Stack{Name: "s", Containers: []swarm.Container{{State: "running", Status: "Up 3 seconds (health: starting)"}}}
	if s := stackRow(starting); s.tone != inherit || s.cells[0].tone != warn {
		t.Errorf("starting stack: %+v", s)
	}
}

// Colored cells and the selection bar keep every line the same width.
func TestGridRender(t *testing.T) {
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	r := fixture(t)
	g := grid{titles: machineTitles}
	g.width, g.height = 120, 6
	g.setRows([]row{machineRow(r), machineRow(swarm.Report{Host: swarm.Host{Name: "garage-node-neatus"}, Err: errTest})})
	g.key(tea.KeyMsg{Type: tea.KeyDown})

	v := g.view()
	if !strings.Contains(v, "\x1b[31m") || !strings.Contains(v, "\x1b[33m") || !strings.Contains(v, "▌") {
		t.Errorf("expected red, yellow and the selection's marker in:\n%q", v)
	}
	lines := strings.Split(v, "\n")
	want := lipgloss.Width(lines[0])
	for i, l := range lines[:3] { // the header, then the rows: no rule between them
		if w := lipgloss.Width(l); w != want {
			t.Errorf("line %d is %d wide, header %d: %q", i, w, want, l)
		}
	}
}

// A narrow grid scrolls whole columns under the first, every line as wide as the
// grid, with "…" where more follows; a wide one doesn't scroll.
func TestGridScroll(t *testing.T) {
	r := fixture(t)
	g := grid{titles: machineTitles}
	g.width, g.height = 60, 2 // the header and the row
	g.setRows([]row{machineRow(r)})

	for range len(machineTitles) { // past the end, which stops it
		head, line, _ := strings.Cut(g.view(), "\n")
		if lipgloss.Width(head) != g.width || lipgloss.Width(line) != g.width || !strings.Contains(line, "app-node-argiope") {
			t.Fatalf("scrolled %d: lines not %d wide, or the first column lost:\n%s\n%s", g.scroll, g.width, head, line)
		}

		// The first column shown after MACHINE is whole: its title starts it.
		if shown := strings.TrimLeft(strings.TrimPrefix(head, " MACHINE"), " "); !strings.HasPrefix(shown, machineTitles[1+g.scroll]) {
			t.Fatalf("scrolled %d, the header shows %q", g.scroll, shown)
		}

		if g.scroll < g.hidden() && !strings.HasSuffix(head, "…") {
			t.Fatalf("scrolled %d of %d, no … for the columns hidden: %q", g.scroll, g.hidden(), head)
		}

		g.key(tea.KeyMsg{Type: tea.KeyRight})
	}

	if g.scroll != len(machineTitles)-2 { // the problems are long: they end up alone after MACHINE
		t.Errorf("scrolled %d at the end", g.scroll)
	}

	for range len(machineTitles) {
		g.key(tea.KeyMsg{Type: tea.KeyLeft})
	}

	if g.scroll != 0 {
		t.Errorf("scrolled back to %d", g.scroll)
	}

	g.width = 1000
	if g.clamp(); g.hidden() != 0 || g.scroll != 0 {
		t.Errorf("a wide grid scrolls: %d columns hidden, %d scrolled", g.hidden(), g.scroll)
	}
}
