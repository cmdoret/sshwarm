package swarm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T) Report {
	t.Helper()

	raw, err := os.ReadFile("testdata/argiope.probe")
	if err != nil {
		t.Fatal(err)
	}

	r, err := Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}

	r.Host = Host{Name: "app-node-argiope", Address: "argiope"}

	return r
}

// fakeBin puts an executable script called name first on PATH.
func fakeBin(t *testing.T, name, script string) {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestParse(t *testing.T) {
	r := fixture(t)
	if r.OS != "NixOS 26.05 (Yarara)" || !r.RebootPending || r.Docker != "sudo -n docker" || len(r.Errors) != 0 {
		t.Errorf("meta: %+v", r)
	}

	if r.Deployed != time.Unix(1794292800, 0) || r.Uptime != 90061520*time.Millisecond {
		t.Errorf("deployed %v, uptime %v", r.Deployed, r.Uptime)
	}

	if r.CPU != 12.5 || r.Memory != 74.2 || r.Disk != 93.1 {
		t.Errorf("cpu %v, memory %v, disk %v", r.CPU, r.Memory, r.Disk)
	}

	if r.Revision != "0123456789abcdef0123456789abcdef01234567-dirty" {
		t.Errorf("rev = %s", r.Revision)
	}

	want := Unit{
		Name: "compose-immich.service", Active: "active", Sub: "exited", Description: "compose stack immich",
		Result: "success", UnitFileState: "enabled", Since: time.Unix(1794296400, 0),
	}
	if len(r.Units) != 3 || r.Units[0] != want {
		t.Errorf("units: %+v", r.Units)
	}

	if f := r.Failed(); len(f) != 1 || f[0].Name != "broken.service" {
		t.Errorf("failed: %+v", f)
	}

	// immich: unhealthy server; utils: init container exited 0 is fine; cook: exited 1.
	if len(r.Stacks) != 3 || r.StacksUp() != 1 {
		t.Errorf("stacks: %d, up %d: %+v", len(r.Stacks), r.StacksUp(), r.Stacks)
	}
}

// Elsewhere than NixOS, there is no deploy time or revision, and nothing's wrong.
func TestParseNotNixOS(t *testing.T) {
	raw, err := os.ReadFile("testdata/debian.probe")
	if err != nil {
		t.Fatal(err)
	}

	r, err := Parse(string(raw))
	if err != nil {
		t.Fatal(err)
	}

	if r.OS != "Debian GNU/Linux 12 (bookworm)" || !r.RebootPending || r.Docker != "" || len(r.Units) != 2 {
		t.Errorf("%+v", r)
	}

	if !r.Deployed.IsZero() || r.Revision != "" || len(r.Problems()) != 0 {
		t.Errorf("deployed %v, revision %q, problems %q", r.Deployed, r.Revision, r.Problems())
	}
}

// A command that fails on the host is a problem, not a silent gap.
func TestProbeErrors(t *testing.T) {
	out := "### meta\nnow=1\ndocker=sudo -n docker\n" +
		"### error containers\nCannot connect to the Docker daemon\n" +
		"### error units\nFailed to connect\n  to bus\n"

	r, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}

	if r.Docker == "" {
		t.Error("docker that fails must still count as present")
	}

	if r.CPU != -1 || r.Memory != -1 || r.Disk != -1 {
		t.Errorf("usage not measured must be unknown, not 0%%: cpu %v, memory %v, disk %v", r.CPU, r.Memory, r.Disk)
	}

	want := []string{"containers: Cannot connect to the Docker daemon", "units: Failed to connect to bus"}
	if !slices.Equal(r.Problems(), want) {
		t.Errorf("problems = %q, want %q", r.Problems(), want)
	}

	if _, err := Parse("nothing ran"); err == nil {
		t.Error("output without a meta section must be an error")
	}
}

// TestDockerCmd runs the probe here, against a fake docker that answers one way only:
// the probe must find that way, and keep sudo's error when there's none.
func TestDockerCmd(t *testing.T) {
	fakeBin(t, "sudo", `shift; SUDO=1 exec "$@"`) // sudo -n docker …: docker, as root

	tests := []struct{ answers, want string }{
		{`exit 0`, "docker"},
		{`[ "$1" = -H ]`, fmt.Sprintf("docker -H unix:///run/user/%d/docker.sock", os.Getuid())},
		{`[ -n "$SUDO" ]`, "sudo -n docker"},
		{`exit 1`, "sudo -n docker"},
	}

	for _, tt := range tests {
		fakeBin(t, "docker", tt.answers)

		cmd := exec.CommandContext(t.Context(), "bash", "-s")
		cmd.Stdin = strings.NewReader(probeScript)

		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}

		r, err := Parse(string(out))
		if err != nil {
			t.Fatal(err)
		}

		if r.Docker != tt.want {
			t.Errorf("docker answering %s: got %q, want %q", tt.answers, r.Docker, tt.want)
		}
	}
}

func TestSSHCmd(t *testing.T) {
	args := sshCmd(t.Context(), Host{Address: "-oProxyCommand=x", Opts: []string{"-p", "2222"}}, true, "uptime").Args
	n := len(args)

	if !slices.Equal(args[n-3:], []string{"--", "-oProxyCommand=x", "uptime"}) {
		t.Errorf("want -- destination command at the end: %q", args)
	}

	if i := slices.Index(args, "-p"); i < 0 || args[i+1] != "2222" || !slices.Contains(args[:n-3], "-tt") {
		t.Errorf("host options and the terminal go before --: %q", args)
	}

	// No remote command: a login shell.
	if args := sshCmd(t.Context(), Host{Address: "h"}, true, "").Args; args[len(args)-1] != "h" {
		t.Errorf("want the destination last: %q", args)
	}
}

// TestProbeAll runs the ssh path against a fake ssh: one good host, one refusing,
// one hanging, one whose probe stopped half-way.
func TestProbeAll(t *testing.T) {
	// Like real ssh: destination right after --, then the remote command.
	fakeBin(t, "ssh", `
while [ $# -gt 0 ] && [ "$1" != -- ]; do shift; done
case "$3" in "timeout "*" bash -s") ;; *) echo "ssh: bad destination or command: $2 $3" >&2; exit 255 ;; esac
cat >/dev/null
case "$2" in
  slow) sleep 5 ;;
  down) printf 'ssh: connect to host down port 22:\n Connection refused\n' >&2; exit 255 ;;
  half) printf '### meta\nnow=1\nreboot=true\n### error containers\nno \033]52;c;aGk=\007docker\n'; exit 124 ;;
esac
cat testdata/argiope.probe
`)

	start := time.Now()
	hosts := []Host{{Address: "ok"}, {Address: "down"}, {Address: "slow"}, {Address: "half"}}
	r := ProbeAll(t.Context(), hosts, time.Second)

	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("a hanging host blocked for %s", took)
	}

	if r[0].Err != nil || len(r[0].Units) != 3 || r[0].Host.Address != "ok" {
		t.Errorf("ok host: %v %+v", r[0].Err, r[0])
	}

	if r[1].Err == nil || r[1].Err.Error() != "ssh: connect to host down port 22: Connection refused" {
		t.Errorf("down host: %v", r[1].Err)
	}

	if r[2].Err == nil || r[2].Err.Error() != "timed out" {
		t.Errorf("slow host: %v", r[2].Err)
	}

	// What the host sends is stripped of control characters (here, a clipboard write).
	want := []string{"containers: no docker", "probe: timed out on the host"}
	if r[3].Err != nil || !r[3].RebootPending || !slices.Equal(r[3].Errors, want) {
		t.Errorf("half host: %v %q", r[3].Err, r[3].Errors)
	}
}

func TestCleanLogLines(t *testing.T) {
	fakeBin(t, "ssh", `printf 'user \033]52;c;aGk=\007logged in\r\n\tindented\n\033[32m[Nest]\033[39m LOG\n'`)

	var got []string
	for line := range streamLines(t.Context(), Host{Address: "h"}, false, "cmd") {
		got = append(got, line)
	}

	// No escape sequence, colors included, and no stray carriage return.
	if want := []string{"user logged in", "\tindented", "[Nest] LOG"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
