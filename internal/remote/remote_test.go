package remote

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/testutil"
)

func TestCommand(t *testing.T) {
	args := Command(t.Context(), inventory.Host{Address: "-oProxyCommand=x", Opts: []string{"-p", "2222"}}, true, "uptime").Args
	n := len(args)

	if !slices.Equal(args[n-3:], []string{"--", "-oProxyCommand=x", "uptime"}) {
		t.Errorf("want -- destination command at the end: %q", args)
	}

	if i := slices.Index(args, "-p"); i < 0 || args[i+1] != "2222" || !slices.Contains(args[:n-3], "-tt") {
		t.Errorf("host options and the terminal go before --: %q", args)
	}

	// No remote command: a login shell.
	if args := Command(t.Context(), inventory.Host{Address: "h"}, true, "").Args; args[len(args)-1] != "h" {
		t.Errorf("want the destination last: %q", args)
	}
}

// Share has ssh share connections, with their sockets in ~/.ssh, but to a host
// whose ssh config already sets how, as ssh -G tells, or if ssh -G fails; and none
// without a ~/.ssh to keep the sockets in, or with one too long for their paths.
func TestShare(t *testing.T) {
	testutil.FakeBin(t, "ssh", `case "$*" in
  *own*) printf 'user me\ncontrolmaster auto\ncontrolpath /home/me/.ssh/cm-x\n' ;;
  *broken*) exit 255 ;;
  *) printf 'user me\ncontrolmaster false\ncontrolpersist no\n' ;;
esac`)

	// A home short enough for a socket's path, which t.TempDir's isn't under a long
	// TMPDIR, as nix develop's or macOS's.
	home, err := os.MkdirTemp("/tmp", "h")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.RemoveAll(home) })
	t.Setenv("HOME", home)

	hosts := []inventory.Host{{Address: "plain", Opts: []string{"-p", "2222"}}, {Address: "own"}, {Address: "broken"}}

	if got := Share(t.Context(), hosts); got[0].Opts[0] != "-p" {
		t.Errorf("shared without a ~/.ssh: %q", got[0].Opts)
	}

	long := filepath.Join(home, strings.Repeat("x", 40))
	for _, h := range []string{home, long} {
		if err := os.MkdirAll(filepath.Join(h, ".ssh"), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("HOME", long)

	if got := Share(t.Context(), hosts); got[0].Opts[0] != "-p" {
		t.Errorf("shared with a ~/.ssh too long for a socket's path: %q", got[0].Opts)
	}

	t.Setenv("HOME", home)

	got := Share(t.Context(), hosts)

	if i := slices.Index(got[0].Opts, `ControlPath="`+home+`/.ssh/sshwarm-%C"`); i < 0 || got[0].Opts[len(got[0].Opts)-2] != "-p" {
		t.Errorf("plain host: %q, want the socket in ~/.ssh, before its own options", got[0].Opts)
	}

	if got[1].Opts != nil || got[2].Opts != nil {
		t.Errorf("own config, or no ssh -G: %q, %q, want them left alone", got[1].Opts, got[2].Opts)
	}

	if hosts[0].Opts[0] != "-p" {
		t.Errorf("Share changed the hosts it was given: %q", hosts[0].Opts)
	}
}

// Run returns a command's output, stdout and stderr, and its failure.
func TestRun(t *testing.T) {
	testutil.FakeBin(t, "ssh", `for a; do last=$a; done
echo "ran: $last"; echo oops >&2
case "$last" in *fail*) exit 3 ;; esac`)

	h := inventory.Host{Address: "h"}

	if out, err := Run(t.Context(), h, "ok"); err != nil || out != "ran: ok\noops\n" {
		t.Errorf("Run: %q, %v", out, err)
	}

	if out, err := Run(t.Context(), h, "fail"); err == nil || !strings.Contains(out, "oops") {
		t.Errorf("Run of a failing command: %q, %v", out, err)
	}
}

// A line too long to read ends the stream with a line saying so.
func TestStreamLongLine(t *testing.T) {
	testutil.FakeBin(t, "ssh", `echo before; head -c 1100000 /dev/zero | tr '\\0' x; echo`)

	var got []string
	for line := range Stream(t.Context(), inventory.Host{Address: "h"}, "cmd", false) {
		got = append(got, line)
	}

	if want := []string{"before", "[bufio.Scanner: token too long]"}; !slices.Equal(got, want) {
		t.Errorf("got %d lines, %.60q…, want %q", len(got), got, want)
	}
}

// Stream sends clean lines: no escape sequence, colors included, and no stray
// carriage return; a failure ends them with a line saying so.
func TestStream(t *testing.T) {
	testutil.FakeBin(t, "ssh", `printf 'user \033]52;c;aGk=\007logged in\r\n\tindented\n\033[32m[Nest]\033[39m LOG\n'; exit 3`)

	var got []string
	for line := range Stream(t.Context(), inventory.Host{Address: "h"}, "cmd", false) {
		got = append(got, line)
	}

	if want := []string{"user logged in", "\tindented", "[Nest] LOG", "[exit status 3]"}; !slices.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}
