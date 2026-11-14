package swarm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cmdoret/sshwarm/internal/inventory"
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
		"StackLogs":         Docker("docker").StackLogs(name),
		"ContainerLogs":     Docker("docker").ContainerLogs(name),
		"ContainerShell":    Docker("docker").ContainerShell(name),
		"UnitAction":        UnitAction(Restart, name),
		"StackAction":       Docker("docker").StackAction(Stop, name),
		"ContainerAction":   Docker("docker").ContainerAction(Start, name),
		"DescribeUnit":      DescribeUnit(name),
		"DescribeStack":     Docker("docker").DescribeStack(name),
		"DescribeContainer": Docker("docker").DescribeContainer(name),
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

// Stream runs a one-shot command under a time limit, and follows a logs command with
// -f and a terminal; a failure ends the stream with a line saying so.
func TestStream(t *testing.T) {
	testutil.FakeBin(t, "ssh", `for a; do last=$a; done
case "$*" in *-tt*) echo tty ;; esac
echo "ran: $last"; echo oops >&2
case "$last" in *fail*) exit 3 ;; esac`)

	h := inventory.Host{Address: "h"}

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

// A unit's action runs systemctl directly as root, else through sudo, and only once
// when it fails.
func TestUnitAction(t *testing.T) {
	testutil.FakeBin(t, "id", `echo "$FAKE_UID"`)
	testutil.FakeBin(t, "sudo", `shift; printf 'sudo '; exec "$@"`) // sudo -n …
	testutil.FakeBin(t, "systemctl", `echo "systemctl $*"; exit 1`)

	for uid, want := range map[string]string{"0": "systemctl --no-block restart it's", "1000": "sudo systemctl --no-block restart it's"} {
		t.Setenv("FAKE_UID", uid)

		out, err := exec.CommandContext(t.Context(), "bash", "-c", UnitAction(Restart, "it's")).CombinedOutput()
		if err == nil || strings.TrimSpace(string(out)) != want {
			t.Errorf("uid %s: %v, ran %q, want %q once", uid, err, out, want)
		}
	}
}

// A reboot is a timer's, a moment later, running systemctl by its full path.
func TestReboot(t *testing.T) {
	testutil.FakeBin(t, "id", "echo 0")
	testutil.FakeBin(t, "systemctl", "")
	testutil.FakeBin(t, "systemd-run", `echo "$@"`)

	out, err := exec.CommandContext(t.Context(), "bash", "-c", Reboot()).CombinedOutput()
	if f := strings.Fields(string(out)); err != nil || len(f) != 4 || f[0] != "--on-active=2" || !strings.HasPrefix(f[2], "/") || f[3] != "reboot" {
		t.Errorf("%v, ran systemd-run %s", err, out)
	}
}
