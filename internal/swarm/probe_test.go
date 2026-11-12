package swarm

import (
	"os"
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
	if r.Generation != 42 || !r.RebootPending || len(r.Errors) != 0 {
		t.Errorf("meta: %+v", r)
	}
	if r.Deployed != time.Unix(1794292800, 0) || r.Uptime != 90061520*time.Millisecond {
		t.Errorf("deployed %v, uptime %v", r.Deployed, r.Uptime)
	}
	if r.Revision != "0123456789abcdef0123456789abcdef01234567-dirty" {
		t.Errorf("rev = %s", r.Revision)
	}
	if len(r.Units) != 3 || r.Units[0].Result != "success" || r.Units[0].Since != time.Unix(1794296400, 0) {
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

// A command that fails on the host is a problem, not a silent gap.
func TestProbeErrors(t *testing.T) {
	out := "### meta\nnow=1\ndocker=true\n### units\n[]\n" +
		"### error containers\nCannot connect to the Docker daemon\n" +
		"### error version\nnixos-version:\n  not found\n"
	r, err := Parse(out)
	if err != nil {
		t.Fatal(err)
	}
	if !r.HasDocker {
		t.Error("docker that fails must still count as present")
	}
	want := []string{"containers: Cannot connect to the Docker daemon", "version: nixos-version: not found"}
	if !slices.Equal(r.Problems(), want) {
		t.Errorf("problems = %q, want %q", r.Problems(), want)
	}

	// Units that don't parse: the rest of the report still shows.
	if r, err = Parse("### meta\nnow=1\ngeneration=3\n### units\nnot json\n"); err != nil {
		t.Fatal(err)
	}
	if r.Generation != 3 || len(r.Errors) != 1 || !strings.HasPrefix(r.Errors[0], "units: ") {
		t.Errorf("%+v", r)
	}
	if r.HasDocker {
		t.Error("no docker on the host, yet a stacks column")
	}
}

func TestSSHCmd(t *testing.T) {
	args := sshCmd(t.Context(), Host{Address: "-oProxyCommand=x", Opts: []string{"-p", "2222"}}, false, true, "uptime").Args
	n := len(args)
	if !slices.Equal(args[n-3:], []string{"--", "-oProxyCommand=x", "uptime"}) {
		t.Errorf("want -- destination command at the end: %q", args)
	}
	if i := slices.Index(args, "-p"); i < 0 || args[i+1] != "2222" || !slices.Contains(args[:n-3], "-tt") {
		t.Errorf("node opts and the terminal go before --: %q", args)
	}
	if slices.Contains(args, "ControlMaster=auto") {
		t.Error("one-shot runs must not leave a ControlPersist master behind")
	}
	if !slices.Contains(sshCmd(t.Context(), Host{}, true, false, "").Args, "ControlMaster=auto") {
		t.Error("the TUI shares connections")
	}
}

func TestLoadHosts(t *testing.T) {
	// The fake nix echoes the flake reference it was given into the hostname.
	fakeBin(t, "nix", `echo "{\"b-node\":{\"hostname\":\"$3\",\"sshUser\":\"\",\"sshOpts\":[\"-p\",\"2\"]},\"a-node\":{\"hostname\":\"h\",\"sshUser\":\"u\",\"sshOpts\":[]}}"`)
	hosts, err := LoadFlake(t.Context(), "nix")
	if err != nil {
		t.Fatal(err)
	}
	if hosts[0].Address != "u@h" || hosts[1].Name != "b-node" || !slices.Equal(hosts[1].Opts, []string{"-p", "2"}) {
		t.Errorf("%+v", hosts)
	}
	if abs, _ := filepath.Abs("nix"); hosts[1].Address != abs+"#deploy" {
		t.Errorf("the flake must be an absolute path, not a registry name: %q", hosts[1].Address)
	}

	fakeBin(t, "nix", `echo "{}"`)
	if _, err := LoadFlake(t.Context(), "."); err == nil || !strings.Contains(err.Error(), "no machines") {
		t.Errorf("empty deploy.nodes: %v", err)
	}
}

// TestProbeAll runs the ssh path against a fake ssh: one good host, one refusing,
// one hanging, one whose probe stopped half-way.
func TestProbeAll(t *testing.T) {
	// Like real ssh: destination right after --, then the remote command.
	fakeBin(t, "ssh", `
while [ $# -gt 0 ] && [ "$1" != -- ]; do shift; done
case "$3" in "timeout "*" bash -s -- 'sudo -n docker'") ;; *) echo "ssh: bad destination or command: $2 $3" >&2; exit 255 ;; esac
cat >/dev/null
case "$2" in
  slow) sleep 5 ;;
  down) printf 'ssh: connect to host down port 22:\n Connection refused\n' >&2; exit 255 ;;
  half) printf '### meta\nnow=1\ngeneration=7\n### error containers\nno \033]52;c;aGk=\007docker\n'; exit 124 ;;
esac
cat testdata/argiope.probe
`)
	start := time.Now()
	hosts := []Host{{Address: "ok"}, {Address: "down"}, {Address: "slow"}, {Address: "half"}}
	r := ProbeAll(t.Context(), hosts, time.Second, false)
	if took := time.Since(start); took > 4*time.Second {
		t.Errorf("a hanging host blocked for %s", took)
	}
	if r[0].Err != nil || r[0].Generation != 42 {
		t.Errorf("ok host: %v %+v", r[0].Err, r[0])
	}
	if r[1].Err == nil || r[1].Err.Error() != "ssh: connect to host down port 22: Connection refused" {
		t.Errorf("down host: %v", r[1].Err)
	}
	if r[2].Err == nil || r[2].Err.Error() != "timed out" {
		t.Errorf("slow host: %v", r[2].Err)
	}
	// What the host sends is stripped of control characters (here, a clipboard write).
	if want := []string{"containers: no ]52;c;aGk=docker", "probe: timed out on the host"}; r[3].Err != nil || r[3].Generation != 7 || !slices.Equal(r[3].Errors, want) {
		t.Errorf("half host: %v %q", r[3].Err, r[3].Errors)
	}
}

func TestCleanLogLines(t *testing.T) {
	fakeBin(t, "ssh", `printf 'user \033]52;c;aGk=\007logged in\r\n\tindented\n'`)
	var got []string
	for line := range StreamLines(t.Context(), Host{Address: "h"}, "cmd", false) {
		got = append(got, line)
	}
	if want := []string{"user ]52;c;aGk=logged in", "\tindented"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Any probe output saved in testdata must parse cleanly, e.g. one recorded with
// `go run . --flake <dir> probe <machine> > internal/swarm/testdata/<machine>.probe`.
func TestRecordedProbes(t *testing.T) {
	files, err := filepath.Glob("testdata/*.probe")
	if err != nil || len(files) == 0 {
		t.Fatalf("no recorded probes: %v", err)
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		r, err := Parse(string(raw))
		if err != nil || len(r.Errors) > 0 || len(r.Units) == 0 || r.Now.IsZero() {
			t.Errorf("%s: err %v, errors %q, %d units", f, err, r.Errors, len(r.Units))
		}
	}
}
