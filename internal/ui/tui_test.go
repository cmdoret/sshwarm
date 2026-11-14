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
	case "shift+left":
		return tea.KeyMsg{Type: tea.KeyShiftLeft}
	case "shift+right":
		return tea.KeyMsg{Type: tea.KeyShiftRight}
	case "ctrl+k":
		return tea.KeyMsg{Type: tea.KeyCtrlK}
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

	// Filter, then clear it with esc: the cursor stays on the unit found.
	s.keys("/", "glan")
	s.expect("services 1", "glances.service")
	s.keys("esc")
	s.expect("services 3")

	if g := s.top().(*table).grid; g.rows[g.cursor].key != "glances.service" {
		t.Errorf("the cursor left the unit found for %s", g.rows[g.cursor].key)
	}

	s.keys("g") // back to the failed unit

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
	s.expect("containers 5", "immich_server", "vert", "cook-cookcli-1", "J stack")

	// J: from immich_server to its stack, in the stacks tab, even filtered out.
	s.keys("/", "server", "enter", "J")

	if tb := s.top().(*table); tb.kind != stacks || tb.grid.rows[tb.grid.cursor].key != "immich" || tb.filter.Value() != "" {
		t.Errorf("J went to %s, on row %d", tb.kind.names[0], tb.grid.cursor)
	}

	// Back, into the machine; shift+tab goes round the tabs the other way.
	s.keys("esc", "enter", "shift+tab")
	s.expect("CONTAINER")
	s.keys("shift+tab")
	s.expect("STACK", "PROBLEMS")

	// A stack: enter opens its containers, J goes back to it, and l the containers' logs.
	s.keys("enter")
	s.expect("app-node-argiope › stack cook", "containers 1", "cook-cookcli-1")
	s.keys("J")

	if n := len(s.m.(model).screens); n != 2 {
		t.Errorf("J from a stack's containers: %d screens, want the machines and the stacks", n)
	}

	s.keys("enter")
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

// A machine running no stack has no stacks or containers tab, docker or not.
func TestTabs(t *testing.T) {
	r := fixture(t)
	r.Stacks = nil // docker, still

	s := start(t, r)
	s.keys(":", "st arg", "enter")
	s.expect("app-node-argiope has no stacks")
}

// The tables sort on a column: selected with shift and the arrows then O, or with
// k9s's keys. Numbers sort biggest first, text from A ignoring case, and the other
// way round on the same column again. The cursor stays on its row.
func TestSort(t *testing.T) {
	r := fixture(t)
	idle := r
	idle.Host = swarm.Host{Name: "Idle"}
	idle.Units, idle.Stacks, idle.Disk = nil, nil, 12

	down := swarm.Report{Host: swarm.Host{Name: "down"}, Err: errTest}

	s := start(t, idle, r, down)
	s.expect("⇧←→ column • O sort")

	order := func(want ...string) {
		t.Helper()

		var keys []string
		for _, row := range s.top().(*table).grid.rows {
			keys = append(keys, row.key)
		}

		if !slices.Equal(keys, want) {
			t.Errorf("rows %q, want %q", keys, want)
		}
	}

	on := func(want string) {
		t.Helper()

		if g := s.top().(*table).grid; g.rows[g.cursor].key != want {
			t.Errorf("the cursor is on %s, not %s", g.rows[g.cursor].key, want)
		}
	}

	// The worst state first, then the other way round.
	s.keys("S")
	order("down", "app-node-argiope", "Idle")
	s.expect("STATE ↓")
	s.keys("S")
	order("Idle", "app-node-argiope", "down")
	s.expect("STATE ↑")

	// From STATE to DISK: the fullest first, the unknown last.
	for range slices.Index(machineTitles, "DISK") - 1 {
		s.keys("shift+right")
	}

	s.keys("O")
	order("app-node-argiope", "Idle", "down")
	s.expect("DISK ↓")

	// The cursor stays on its machine through the sorts, and a refresh that
	// reorders the rows.
	idle.Disk = 99
	s.m, _ = s.m.Update(reportsMsg{idle, r, down})
	order("Idle", "app-node-argiope", "down")
	on("Idle")

	s.keys("N")
	order("app-node-argiope", "down", "Idle")
	on("Idle")

	// Containers sort on the time since they last started or stopped, to the second:
	// all "2h" but the one that exited 5 minutes ago.
	s.keys(":", "ctr arg", "enter", "A")
	s.expect("AGE ↓", "5m")
	order("immich_redis", "vert", "utils-init-volumes-1", "immich_server", "cook-cookcli-1")
}

// u, ctrl+k and R start, stop and restart a unit, a stack or a container, and R
// reboots a machine, once confirmed with y: in the background, saying how it went,
// then probing again. Any other key cancels. Read-only, there are no actions at all.
func TestStartStop(t *testing.T) {
	r := fixture(t)
	runs := t.TempDir() + "/runs"
	t.Setenv("RUNS", runs)
	// The actions' ssh: note what it ran. systemctl fails, as without sudo rights.
	fakeBin(t, "ssh", `eval remote=\${$#}; echo "$remote" >> "$RUNS"
case "$remote" in *systemctl*) echo "sudo: a password is required"; exit 1 ;; esac`)

	s := start(t, r)

	// act types key, confirms with y, and waits for the action's end.
	act := func(key string) {
		t.Helper()

		var cmd tea.Cmd

		s.keys(key)
		s.m, cmd = s.m.Update(keyMsg("y"))
		s.expect("1 running")

		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok { // the action, then the spinner's tick if it wasn't turning
			msg = batch[0]()
		}

		s.m, cmd = s.m.Update(msg)

		if m := s.m.(model); m.acting != 0 || cmd == nil && !m.stale {
			t.Errorf("%s: still running, or no refresh after it", key)
		}
	}

	// A machine only reboots.
	s.expect("s ssh • R reboot")
	s.keys("u", "ctrl+k")

	if s.m.(model).confirm != nil {
		t.Error("a machine started or stopped")
	}

	s.keys("R")
	s.expect("reboot app-node-argiope? y/N")
	s.keys("n")
	act("R")
	s.expect("reboot app-node-argiope failed: sudo: a password is required")

	// R on the failed unit asks first; another key cancels.
	s.keys("enter", "R")
	s.expect("restart broken.service on app-node-argiope? y/N")
	s.keys("n")
	s.expect("cancelled")

	// Confirmed, it fails: why shows.
	act("R")
	s.expect("restart broken.service on app-node-argiope failed: sudo: a password is required")

	// ctrl+k on a stack, u on a container.
	s.keys("tab")
	act("ctrl+k")
	s.expect("stopped stack cook on app-node-argiope")

	// The probe running when it ended may have looked before: another follows.
	s.m, _ = s.m.Update(reportsMsg{r})

	if m := s.m.(model); !m.refreshing || m.stale {
		t.Error("no probe after the one running during the action")
	}

	s.keys("tab")
	act("u")
	s.expect("started container cook-cookcli-1 on app-node-argiope")

	ran, _ := os.ReadFile(runs)
	if want := swarm.Reboot() + "\n" + swarm.UnitAction("restart", "broken.service") + "\nsudo -n docker compose -p 'cook' stop\nsudo -n docker start 'cook-cookcli-1'\n"; string(ran) != want {
		t.Errorf("ran:\n%s\nwant:\n%s", ran, want)
	}

	// Read-only: the keys only say so, and the help doesn't offer them.
	s.keys("j")
	s.expect("s shell • J stack • u start • ^k stop • R restart")

	ro := s.m.(model)
	ro.readOnly = true
	s.m = ro

	s.keys("R")
	s.expect("read-only: no actions")
	s.keys("j")

	if v := s.m.View(); strings.Contains(v, "R restart") || s.m.(model).confirm != nil {
		t.Errorf("read-only, actions are still offered:\n%s", v)
	}
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

// c copies what's on screen: a card, never the values it hides, even while the next
// description comes in under it; the output as it came, once asked for.
func TestCopy(t *testing.T) {
	var copied string

	saved := copyText
	t.Cleanup(func() { copyText = saved })
	copyText = func(s string) { copied = s }

	raw, err := os.ReadFile("../swarm/testdata/container.describe")
	if err != nil {
		t.Fatal(err)
	}

	c := newText("describe", swarm.Host{}, "", containerCard)
	c.resize(80, 30)
	c.lines = strings.Split(string(raw), "\n")
	c.setContent()

	if c.copyShown(); !strings.Contains(copied, "Environment (values hidden)") || strings.Contains(copied, "hunter2") {
		t.Errorf("copied, from the card:\n%s", copied)
	}

	c.lines, c.ch = []string{`"Env": ["DB_PASSWORD=hunter2"`}, make(chan string) // refreshing
	c.setContent()

	if c.copyShown(); strings.Contains(copied, "hunter2") || !strings.Contains(copied, "Environment") {
		t.Errorf("copied, from the card being refreshed:\n%s", copied)
	}

	c.raw = true
	c.setContent()

	if c.copyShown(); copied != c.lines[0] {
		t.Errorf("copied %q, not the raw output on screen", copied)
	}
}

// In a narrow window, a card is still drawn whole: its long lines scroll left and
// right, and there's nothing to wrap.
func TestCardScroll(t *testing.T) {
	r := fixture(t)
	fakeBin(t, "ssh", "cat ../swarm/testdata/unit.describe")

	s := start(t, r)
	s.resize(60)
	s.keys("enter")
	s.logs("d")

	// The title gives way to the status, rather than the status to the window's edge.
	s.expect("Recent logs", "refreshed ", "─╮")

	if v := s.m.View(); strings.Contains(v, "INVALIDARGUMENT") || strings.Contains(v, "w wrap") {
		t.Errorf("a long line fits a narrow window, or a card wraps:\n%s", v)
	}

	s.keys("l", "l", "l", "l", "l", "l", "l", "l")
	s.expect("status=2/INVALIDARGUMENT")
}
