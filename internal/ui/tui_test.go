package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// session drives the TUI like a user: it types keys and checks the screens.
type session struct {
	t *testing.T
	m tea.Model
}

// start opens the TUI on reports, in a 160×30 window.
func start(t *testing.T, reports ...swarm.Report) *session {
	t.Helper()

	var hosts []swarm.Host
	for _, r := range reports {
		hosts = append(hosts, r.Host)
	}

	s := &session{t: t, m: newModel(t.Context(), hosts, time.Second, time.Hour)}
	s.m, _ = s.m.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	s.m, _ = s.m.Update(reportsMsg(reports))

	return s
}

// keys types each key: "enter", "esc", "tab", "shift+tab", or else text.
func (s *session) keys(keys ...string) {
	for _, k := range keys {
		s.m, _ = s.m.Update(keyMsg(k))
	}
}

// logs types a key that opens or restarts a log stream, and reads the stream to its end.
func (s *session) logs(key string) {
	m, cmd := s.m.Update(keyMsg(key))

	for cmd != nil {
		msg := cmd()
		m, cmd = m.Update(msg)

		if l, ok := msg.(logLinesMsg); ok && l.done {
			break
		}
	}

	s.m = m
}

// expect checks that the screen shows each of want, and fits the window.
func (s *session) expect(want ...string) {
	s.t.Helper()

	v := s.m.View()
	for _, w := range want {
		if !strings.Contains(v, w) {
			s.t.Errorf("missing %q in:\n%s", w, v)
		}
	}

	if n := strings.Count(v, "\n") + 1; n > 30 {
		s.t.Errorf("view is %d lines, taller than the window:\n%s", n, v)
	}
}

func (s *session) top() screen { return s.m.(model).top() }

func keyMsg(k string) tea.KeyMsg {
	switch k {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	}

	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func TestTUI(t *testing.T) {
	r := fixture(t)
	// The logs' ssh: echo what it was asked to run.
	fakeBin(t, "ssh", `
case "$*" in *-tt*) echo "with a terminal" ;; esac
eval remote=\${$#}
echo "ran: $remote"
echo line-two
`)

	// Before the first probe, there is nothing to open: keys but q do nothing.
	m := newModel(t.Context(), []swarm.Host{r.Host}, time.Second, time.Hour)
	if v := m.View(); !strings.Contains(v, "probing 1 machines") {
		t.Errorf("before the first probe: %q", v)
	}

	if _, cmd := m.Update(keyMsg(":")); cmd != nil {
		t.Error("a key acted before the first probe")
	}

	s := start(t, r)
	s.expect("sshwarm", "app-node-argiope", "0123456-dirty", "failed: broken.service")

	// Machine view: the failed unit comes first.
	s.keys("enter")
	s.expect("services 3", "stacks 3", "containers 5", "broken.service", "compose-immich.service")

	if v := s.m.View(); strings.Index(v, "broken.service") > strings.Index(v, "compose-immich.service") {
		t.Error("failed unit not first")
	}

	// Filter, then clear it with esc.
	s.keys("/", "glan")
	s.expect("services 1", "glances.service")
	s.keys("esc")
	s.expect("services 3")

	// a shows every unit, then only the interesting ones again.
	s.keys("a")
	s.expect("a interesting units")
	s.keys("a")
	s.expect("a all units")

	// Logs of the selected (failed) unit.
	s.logs("enter")
	s.expect("app-node-argiope · broken.service", "journalctl -u 'broken.service' -n 300", "line-two", "s follow")

	// Following adds -f and a terminal, so the remote side is hung up when we leave.
	s.logs("s")
	s.expect("with a terminal", "-n 300 -o short-iso --no-pager -f", "s stop following")

	// A long line wraps instead of being cut off.
	stream := s.top().(*text).ch
	s.m, _ = s.m.Update(logLinesMsg{ch: stream, lines: []string{strings.Repeat("x", 150) + " ENDOFLONGLINE"}})
	s.expect("ENDOFLONGLINE")

	// Back, then the stacks tab: cook and immich have problems, utils is fine.
	s.keys("esc", "tab")
	s.expect("cook", "0/1", "immich", "1/2", "Up 2 hours (unhealthy)", "utils", "2/2")
	s.logs("l")
	s.expect("stack cook", "timeout 30 sudo -n docker compose -p 'cook' logs --tail 300")

	// The machine goes down while we look at it: say so instead of showing nothing.
	s.keys("esc")
	s.m, _ = s.m.Update(reportsMsg{{Host: r.Host, Err: errTest}})
	s.expect("down: connection refused")

	// Back down, to the machines.
	s.keys("esc")
	s.expect("1 machines", "enter open")
}

// Stacks open on their containers, the command bar goes to any machine's table,
// and the logs view wraps, goes fullscreen and copies.
func TestNavigation(t *testing.T) {
	r := fixture(t)
	fakeBin(t, "ssh", `eval remote=\${$#}; echo "ran: $remote"; echo "`+strings.Repeat("y", 150)+` ENDOFLONGLINE"`)

	var copied string

	saved := copyText
	t.Cleanup(func() { copyText = saved })
	copyText = func(s string) { copied = s }

	s := start(t, r)

	// The command bar: without a machine on screen, it asks which one.
	s.keys(":", "ctr", "enter")
	s.expect("which machine?")
	s.keys(":", "ctr nope", "enter")
	s.expect(`no machine matches "nope"`)
	s.keys(":", "x", "enter")
	s.expect(`no "x" here: try machines, services/svc, stacks/compose, containers/ctr`)

	// :ctr arg: every compose container of argiope.
	s.keys(":", "ctr arg", "enter")
	s.expect("containers 5", "immich_server", "vert", "cook-cookcli-1")

	// Back, into the machine; shift+tab goes round the tabs the other way.
	s.keys("esc", "enter", "shift+tab")
	s.expect("CONTAINER")
	s.keys("shift+tab")
	s.expect("STACK", "PROBLEMS")

	// A stack: enter opens its containers, and l their logs.
	s.keys("enter")
	s.expect("app-node-argiope · stack cook", "containers 1", "cook-cookcli-1")
	s.logs("l")
	s.expect("app-node-argiope · container cook-cookcli-1", "ran: timeout 30 sudo -n docker logs --tail 300 'cook-cookcli-1'")

	// Wrapped, the long line's end shows; unwrapped, it's off the screen.
	s.expect("ENDOFLONGLINE", "w wrap · f fullscreen · c copy")
	s.keys("w")

	if strings.Contains(s.m.View(), "ENDOFLONGLINE") {
		t.Error("an unwrapped long line must not show its end")
	}

	// Fullscreen: no title or help line, still within the window.
	s.keys("f")
	s.expect()

	if v := s.m.View(); strings.Contains(v, "container cook-cookcli-1") || strings.Contains(v, "esc back") {
		t.Errorf("fullscreen still has its title or help:\n%s", v)
	}

	s.keys("f")

	s.keys("c")
	s.expect("copied 2 lines")

	if !strings.HasPrefix(copied, "ran: timeout 30") || !strings.HasSuffix(copied, "ENDOFLONGLINE") {
		t.Errorf("copied %q", copied)
	}

	// :svc from the logs closes them, their stream with them, and starts over from
	// the machines, so esc goes back there.
	logs := s.top().(*text)
	s.keys(":", "svc", "enter")
	s.expect("services 3")

	if logs.cancel != nil || logs.ch != nil {
		t.Error("the logs' stream outlived their screen")
	}

	if n := len(s.m.(model).screens); n != 2 {
		t.Errorf("%d screens, want the machines and the services", n)
	}

	// :machines goes back to the bottom.
	s.keys(":", "machines", "enter")
	s.expect("1 machines", "enter open")
}
