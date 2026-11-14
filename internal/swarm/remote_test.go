package swarm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cmdoret/sshwarm/internal/testutil"
)

// Every command passes a name on as one argument, whatever it holds: quotes,
// spaces, command substitutions.
func TestCommandQuoting(t *testing.T) {
	const name = `it's "a" $(touch pwned); x`

	for _, bin := range []string{"docker", "journalctl", "systemctl", "sudo"} {
		testutil.FakeBin(t, bin, `printf '[%s]\n' "$@"`) // an argument per line, bracketed
	}

	testutil.FakeBin(t, "id", "echo 0") // as root: no sudo

	for what, cmd := range map[string]string{
		"UnitLogs":          UnitLogs(name),
		"StackLogs":         StackLogs("docker", name),
		"ContainerLogs":     ContainerLogs("docker", name),
		"ContainerShell":    ContainerShell("docker", name),
		"UnitAction":        UnitAction("restart", name),
		"StackAction":       StackAction("docker", "stop", name),
		"ContainerAction":   ContainerAction("docker", "start", name),
		"DescribeUnit":      DescribeUnit(name),
		"DescribeStack":     DescribeStack("docker", name),
		"DescribeContainer": DescribeContainer("docker", name),
	} {
		dir := t.TempDir()

		c := exec.CommandContext(t.Context(), "sh", "-c", cmd)
		c.Dir = dir

		out, _ := c.CombinedOutput() // describe's sections may fail on fakes: only the arguments matter
		if !strings.Contains(string(out), "["+name+"]") && !strings.Contains(string(out), "label=com.docker.compose.project="+name+"]") {
			t.Errorf("%s: the name isn't one argument:\n%s\nran: %s", what, out, cmd)
		}

		if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
			t.Errorf("%s: the name ran a command", what)
		}
	}
}

// Run returns a command's output, stdout and stderr, and its failure. Stream runs a
// one-shot command under a time limit, and follows one with -f and a terminal; a
// failure ends the stream with a line saying so.
func TestRunStream(t *testing.T) {
	testutil.FakeBin(t, "ssh", `for a; do last=$a; done
case "$*" in *-tt*) echo tty ;; esac
echo "ran: $last"; echo oops >&2
case "$last" in *fail*) exit 3 ;; esac`)

	h := Host{Address: "h"}

	if out, err := Run(t.Context(), h, "ok"); err != nil || out != "ran: ok\noops\n" {
		t.Errorf("Run: %q, %v", out, err)
	}

	if out, err := Run(t.Context(), h, "fail"); err == nil || !strings.Contains(out, "oops") {
		t.Errorf("Run of a failing command: %q, %v", out, err)
	}

	lines := func(follow bool, cmd string) string {
		var got []string
		for l := range Stream(t.Context(), h, cmd, follow) {
			got = append(got, l)
		}

		return strings.Join(got, "|")
	}

	for _, tt := range []struct {
		follow    bool
		cmd, want string
	}{
		{false, "logs", "ran: timeout 30 sh -c 'logs'|oops"},
		{true, "logs", "tty|ran: logs -f|oops"},
		{false, "fail", "ran: timeout 30 sh -c 'fail'|oops|[exit status 3]"},
	} {
		if got := lines(tt.follow, tt.cmd); got != tt.want {
			t.Errorf("Stream %q, follow %v:\n got %s\nwant %s", tt.cmd, tt.follow, got, tt.want)
		}
	}
}
