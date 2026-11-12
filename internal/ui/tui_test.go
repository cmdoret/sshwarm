package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// drain runs a log stream's commands until it ends, feeding each message back.
func drain(t *testing.T, m tea.Model, cmd tea.Cmd) tea.Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		m, cmd = m.Update(msg)
		if l, ok := msg.(logLinesMsg); ok && l.done {
			break
		}
	}
	return m
}

func TestTUI(t *testing.T) {
	r := fixture(t)
	// The logs view's ssh: echo what it was asked to run.
	fakeBin(t, "ssh", `
case "$*" in *-tt*) echo "with a terminal" ;; esac
eval remote=\${$#}
echo "ran: $remote"
echo line-two
`)

	var m tea.Model = newModel(t.Context(), "nix", time.Second, time.Hour)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	m, _ = m.Update(hostsMsg{hosts: []swarm.Host{r.Host}})
	m, _ = m.Update(reportsMsg{r})
	expect := func(want ...string) {
		t.Helper()
		v := m.View()
		for _, w := range want {
			if !strings.Contains(v, w) {
				t.Errorf("missing %q in:\n%s", w, v)
			}
		}
		if n := strings.Count(v, "\n") + 1; n > 30 {
			t.Errorf("view is %d lines, taller than the window", n)
		}
	}
	expect("sshwarm", "app-node-argiope", "0123456-dirty", "failed: broken.service")

	// Machine view: the failed unit comes first.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	expect("services 3", "stacks 3", "broken.service", "compose-immich.service")
	if v := m.View(); strings.Index(v, "broken.service") > strings.Index(v, "compose-immich.service") {
		t.Error("failed unit not first")
	}

	// Filter, then clear it with esc.
	m, _ = m.Update(runes("/"))
	for _, c := range "glan" {
		m, _ = m.Update(runes(string(c)))
	}
	expect("services 1", "glances.service")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	expect("services 3")

	// Logs of the selected (failed) unit.
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drain(t, m, cmd)
	expect("app-node-argiope · broken.service", "journalctl -u 'broken.service' -n 300", "line-two", "f follow")

	// Following adds -f and a terminal, so the remote side is hung up when we leave.
	m, cmd = m.Update(runes("f"))
	m = drain(t, m, cmd)
	expect("with a terminal", "-n 300 -o short-iso --no-pager -f", "f stop following")

	// A long line wraps instead of being cut off.
	stream := m.(model).top().(*text).ch
	m, _ = m.Update(logLinesMsg{ch: stream, lines: []string{strings.Repeat("x", 150) + " ENDOFLONGLINE"}})
	expect("ENDOFLONGLINE")

	// Back, then the stacks tab: cook and immich have problems, utils is fine.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	expect("cook", "0/1", "immich", "1/2", "Up 2 hours (unhealthy)", "utils", "2/2")
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = drain(t, m, cmd)
	expect("stack cook", "timeout 30 sudo -n docker compose -p 'cook' logs --tail 300")

	// The machine goes down while we look at it: say so instead of showing nothing.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = m.Update(reportsMsg{{Host: r.Host, Err: errTest}})
	expect("down: connection refused")

	// Back down the stack, to the machines.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	expect("1 machines", "enter open")
}
