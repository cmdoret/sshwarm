package ui

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
	s.resize(160)
	s.m, _ = s.m.Update(reportsMsg(reports))

	return s
}

// resize makes the window width columns wide, and 30 lines high.
func (s *session) resize(width int) {
	s.m, _ = s.m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
}

// keys types each key: "enter", "esc", "tab", "shift+tab", or else text.
func (s *session) keys(keys ...string) {
	for _, k := range keys {
		s.m, _ = s.m.Update(keyMsg(k))
	}
}

// logs types a key that opens or restarts a log stream, and reads the stream to its end.
func (s *session) logs(key string) {
	var cmd tea.Cmd

	s.m, cmd = s.m.Update(keyMsg(key))
	s.read(cmd)
}

// read runs cmd, a stream's, until its end.
func (s *session) read(cmd tea.Cmd) {
	for cmd != nil {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			msg = streamOf(batch)
		}

		s.m, cmd = s.m.Update(msg)

		if l, ok := msg.(logLinesMsg); ok && l.done {
			break
		}
	}
}

// expect checks that the screen shows each of want, and fills the window exactly:
// the frame's bottom edge sits right above the help line.
func (s *session) expect(want ...string) {
	s.t.Helper()

	v := s.m.View()
	for _, w := range want {
		if !strings.Contains(v, w) {
			s.t.Errorf("missing %q in:\n%s", w, v)
		}
	}

	if n := strings.Count(v, "\n") + 1; n != 30 {
		s.t.Errorf("view is %d lines, not the window's 30:\n%s", n, v)
	}

	if w := lipgloss.Width(v); w > s.m.(model).width {
		s.t.Errorf("view is %d columns, wider than the window:\n%s", w, v)
	}
}

// streamOf runs a batch's commands until one brings a stream's lines: a wait's first
// batch also holds the spinner's tick, which is left out.
func streamOf(batch tea.BatchMsg) tea.Msg {
	for _, c := range batch {
		if c == nil {
			continue
		}

		if msg, ok := c().(logLinesMsg); ok {
			return msg
		}
	}

	return nil
}

// ran checks that the text screen on top runs a command with each of want. Unlike
// the screen, the command doesn't wrap with the window.
func (s *session) ran(want ...string) {
	s.t.Helper()

	cmd := s.top().(*text).cmd
	for _, w := range want {
		if !strings.Contains(cmd, w) {
			s.t.Errorf("missing %q in the command: %s", w, cmd)
		}
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
printf 'ran: %s\n' "$remote"
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

	// The spinner turns while probing, and stops with the probe.
	if _, cmd := m.Update(spinner.TickMsg{}); cmd == nil {
		t.Error("no spinner while probing")
	}

	s := start(t, r)
	s.expect("sshwarm", "machines 1", "app-node-argiope", "2/3", "failed: broken.service")

	if _, cmd := s.m.Update(spinner.TickMsg{}); cmd != nil {
		t.Error("the spinner turns after the probe")
	}

	// Machine view: the revision in the title, the failed unit first.
	s.keys("enter")
	s.expect("app-node-argiope · NixOS 26.05 (Yarara) · rev 0123456-dirty · up 25h", "services 3", "stacks 3", "containers 5", "broken.service", "compose-immich.service")

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
	s.expect("app-node-argiope › broken.service", `journalctl -u '\''broken.service'\'' -n 300`, "line-two", "s follow")

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
	s.expect("stack cook", `timeout 30 sh -c 'sudo -n docker compose -p '\''cook'\'' logs --tail 300`)

	// The machine goes down while we look at it: say so instead of showing nothing.
	s.keys("esc")
	s.m, _ = s.m.Update(reportsMsg{{Host: r.Host, Err: errTest}})
	s.expect("down: connection refused")

	// Back down, to the machines.
	s.keys("esc")
	s.expect("machines 1", "enter open")
}

// Stacks open on their containers, the command bar goes to any machine's table,
// and the logs view wraps, goes fullscreen and copies.
func TestNavigation(t *testing.T) {
	r := fixture(t)
	r.Docker = "docker" // the logs use the docker command the probe found
	fakeBin(t, "ssh", `eval remote=\${$#}; printf 'ran: %s\n' "$remote"; echo "`+strings.Repeat("y", 150)+` ENDOFLONGLINE"`)

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
	s.expect(`no "x" here: try machines, services/svc, stacks/st, containers/ctr`)

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
	s.expect("app-node-argiope › stack cook", "containers 1", "cook-cookcli-1")
	s.logs("l")
	s.expect("app-node-argiope › container cook-cookcli-1", `ran: timeout 30 sh -c 'docker logs --tail 300 '\''cook-cookcli-1'\'''`)

	// Wrapped, the long line's end shows; unwrapped, it's off the screen.
	s.expect("ENDOFLONGLINE", "w wrap • f fullscreen • c copy")
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
	s.expect("machines 1", "enter open")
}

// d describes a machine, a unit, a stack or a container, and s opens a shell on a
// machine or in a container.
func TestActions(t *testing.T) {
	r := fixture(t)
	fakeBin(t, "ssh", `eval remote=\${$#}; printf 'ran: %s\n' "$remote"`)

	var shell []string

	saved := execProcess
	t.Cleanup(func() { execProcess = saved })
	execProcess = func(c *exec.Cmd, done tea.ExecCallback) tea.Cmd {
		shell = c.Args

		return func() tea.Msg { return done(errors.New("exit status 127")) }
	}

	s := start(t, r)

	// s on a machine: ssh with a terminal, and no command, so a login shell.
	s.keys("s")

	if n := len(shell); n < 2 || shell[n-2] != "--" || shell[n-1] != "argiope" || !slices.Contains(shell, "-tt") {
		t.Errorf("machine shell: %q", shell)
	}

	// d on a machine: what it is, its CPUs and what uses them, memory, disks. This
	// fake ssh's echo is no description: it shows as it came.
	s.logs("d")
	s.expect("app-node-argiope › describe")
	s.ran("lscpu", "XDG_CONFIG_HOME=/nonexistent", "/proc/meminfo")
	s.keys("esc")

	// d on the failed unit: its status, nothing to follow.
	s.keys("enter")
	s.logs("d")
	s.expect("app-node-argiope › describe broken.service")
	s.ran("systemctl show --timestamp=unix -p Id,Description,ActiveState", "journalctl -u 'broken.service' -n 10")

	if strings.Contains(s.m.View(), "s follow") {
		t.Error("a description offers to follow")
	}

	s.keys("s")

	if s.top().(*text).follow {
		t.Error("s followed a description")
	}

	// A stack, then a container: their commands run in one shell, under one timeout.
	s.keys("esc", "tab")
	s.logs("d")
	s.expect("describe stack cook", "ran: timeout 30 sh -c 'echo")
	s.ran("echo '### cpus'; getconf _NPROCESSORS_ONLN", "sudo -n docker ps -a --filter 'label=com.docker.compose.project=cook'", "sudo -n docker stats --no-stream")

	s.keys("esc", "enter")
	s.logs("d")
	s.expect("describe container cook-cookcli-1")
	s.ran("sudo -n docker inspect 'cook-cookcli-1'")

	// s in a container: docker exec, then the TUI says how the shell ended.
	s.keys("esc")

	var cmd tea.Cmd
	s.m, cmd = s.m.Update(keyMsg("s"))

	want := "sudo -n docker exec -it 'cook-cookcli-1' sh -c 'command -v bash >/dev/null && exec bash || exec sh'"
	if shell[len(shell)-1] != want {
		t.Errorf("container shell: %q", shell)
	}

	s.m, _ = s.m.Update(cmd())
	s.expect("shell ended: exit status 127")

	// In a narrow window, lines are cut instead of wrapped: the help line here, and the
	// machines table's rows.
	s.resize(60)
	s.keys("j") // clears the message
	s.expect("enter logs • d describe • s shell", "…")
	s.keys(":", "machines", "enter")
	s.expect("MACHINE")
}

// A description shows as a card once all its output is in, opening at its top; v
// shows the output as it came, and r describes again.
func TestCard(t *testing.T) {
	r := fixture(t)
	runs := t.TempDir() + "/runs"
	t.Setenv("RUNS", runs)
	fakeBin(t, "ssh", `echo >> "$RUNS"; cat ../swarm/testdata/container.describe`)

	s := start(t, r)
	s.keys("enter", "shift+tab") // the containers tab

	var cmd tea.Cmd
	s.m, cmd = s.m.Update(keyMsg("d"))
	s.expect("loading…")
	s.read(cmd)

	s.expect("Container", "demo-web-1", "running · health unhealthy", "v raw")

	if v := s.m.View(); !strings.Contains(v, "  0%") {
		t.Errorf("a card doesn't open at its top:\n%s", v)
	}

	s.keys("G")
	s.expect("Environment (values hidden)", "DB_PASSWORD")

	if strings.Contains(s.m.View(), "hunter2") {
		t.Error("a card shows an environment value")
	}

	s.keys("v")
	s.expect(`"Id": "044c2818`, "v card")

	s.keys("v")
	s.logs("r")
	s.expect("running · health unhealthy", "r refresh", "refreshed ")

	if n, _ := os.ReadFile(runs); len(n) != 2 {
		t.Errorf("described %d times, want 2: r didn't run it again", len(n))
	}

	// In a while, it describes again by itself, the last card showing meanwhile.
	card := s.top().(*text)
	s.m, cmd = s.m.Update(redescribeMsg{card, card.runs})
	s.expect("running · health unhealthy", s.m.(model).spin.View()+" refreshing")

	if _, tick := s.m.Update(spinner.TickMsg{}); tick == nil {
		t.Error("no spinner while describing")
	}

	s.read(cmd)
	s.expect("refreshed ")

	if _, tick := s.m.Update(spinner.TickMsg{}); tick != nil {
		t.Error("the spinner turns after describing")
	}

	if n, _ := os.ReadFile(runs); len(n) != 3 {
		t.Errorf("described %d times, want 3: it didn't refresh", len(n))
	}

	// A refresh due before r, or for a closed card, doesn't run.
	if _, cmd := s.m.Update(redescribeMsg{card, card.runs - 1}); cmd != nil {
		t.Error("a stale refresh ran")
	}

	s.keys("esc")

	if _, cmd := s.m.Update(redescribeMsg{card, card.runs}); cmd != nil {
		t.Error("a closed card refreshed")
	}
}

// In a narrow window, a card fits it, cutting long lines; w makes it as wide as its
// content, to scroll left and right.
func TestCardScroll(t *testing.T) {
	r := fixture(t)
	fakeBin(t, "ssh", "cat ../swarm/testdata/unit.describe")

	s := start(t, r)
	s.resize(60)
	s.keys("enter")
	s.logs("d")

	// The title gives way to the status, rather than the status to the window's edge.
	s.expect("Recent logs", "w full width", "refreshed ", "─╮")

	if strings.Contains(s.m.View(), "INVALIDARGUMENT") {
		t.Error("a long line fits a narrow window")
	}

	s.keys("w", "l", "l", "l", "l", "l", "l", "l", "l")
	s.expect("status=2/INVALIDARGUMENT", "w fit")

	// Back to fitting the window: from its left edge again.
	s.keys("w")
	s.expect("  Name          backup.service")
}
