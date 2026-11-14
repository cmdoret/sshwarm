package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cmdoret/sshwarm/internal/testutil"
)

// sshwarm runs sshwarm with args, in the environment env.
func sshwarm(t *testing.T, env map[string]string, args ...string) (code int, stdout, stderr string) {
	t.Helper()

	var out, errs strings.Builder
	code = run(t.Context(), args, &out, &errs, func(k string) string { return env[k] })

	return code, out.String(), errs.String()
}

// The help of sshwarm and of each command goes to stdout, and isn't an error.
func TestHelp(t *testing.T) {
	for args, want := range map[string]string{
		"--help":        "Usage: sshwarm [flags] [command]",
		"status --help": "Usage: sshwarm [flags] status",
		"probe -h":      "Usage: sshwarm [flags] probe",
	} {
		if code, out, errs := sshwarm(t, nil, strings.Fields(args)...); code != 0 || !strings.HasPrefix(out, want) || errs != "" {
			t.Errorf("sshwarm %s: exit %d, stdout %.40q, stderr %q", args, code, out, errs)
		}
	}
}

// Every flag is in its help, which is written by hand.
func TestHelpListsFlags(t *testing.T) {
	var global strings.Builder
	if err := usage(&global); err != nil {
		t.Fatal(err)
	}

	status, _ := statusFlags()

	for help, fs := range map[string]*flag.FlagSet{global.String(): globalFlags(&options{}, os.Getenv), statusHelp: status} {
		fs.VisitAll(func(f *flag.Flag) {
			if !strings.Contains(help, "--"+f.Name+" ") {
				t.Errorf("--%s isn't in the help of %s", f.Name, fs.Name())
			}
		})
	}
}

// A mistake says what's wrong on stderr, and exits 1.
func TestErrors(t *testing.T) {
	for args, want := range map[string]string{
		"nope":                 `unknown command "nope"`,
		"--bogus":              "flag provided but not defined: -bogus",
		"status --format yaml": `unknown format "yaml"`,
		"status":               "no hosts provided",
	} {
		if code, out, errs := sshwarm(t, nil, strings.Fields(args)...); code != 1 || out != "" || !strings.Contains(errs, want) {
			t.Errorf("sshwarm %s: exit %d, stdout %q, stderr %q", args, code, out, errs)
		}
	}
}

// status prints the table or JSON of the machines in the hosts file, and exits 1 when
// one has a problem, without saying more than its output.
func TestStatus(t *testing.T) {
	probe, err := filepath.Abs("internal/swarm/testdata/argiope.probe") // a machine with problems
	if err != nil {
		t.Fatal(err)
	}

	testutil.FakeBin(t, "ssh", `cat >/dev/null; cat "`+probe+`"`)

	hosts := filepath.Join(t.TempDir(), "hosts.toml")
	if err := os.WriteFile(hosts, []byte("[[host]]\nname = \"web\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	env := map[string]string{"SSHWARM_HOSTS": hosts}

	if code, out, errs := sshwarm(t, env, "status"); code != 1 || !strings.Contains(out, "degraded") || !strings.Contains(out, "web disk 93% full") || errs != "" {
		t.Errorf("status: exit %d, stderr %q, stdout:\n%s", code, errs, out)
	}

	code, out, errs := sshwarm(t, env, "status", "--format", "json")

	var machines []struct{ Name, State string }
	if err := json.Unmarshal([]byte(out), &machines); err != nil || code != 1 || errs != "" {
		t.Fatalf("status --format json: exit %d, %v, stderr %q, stdout:\n%s", code, err, errs, out)
	}

	if len(machines) != 1 || machines[0].Name != "web" || machines[0].State != "degraded" {
		t.Errorf("status --format json: %+v", machines)
	}
}

// The version line has what the build stamped, before what Go records.
func TestVersion(t *testing.T) {
	saved := [2]string{version, commit}
	t.Cleanup(func() { version, commit = saved[0], saved[1] })
	version, commit = "v1.2.0", "abc1234"

	if code, out, _ := sshwarm(t, nil, "--version"); code != 0 || out != "sshwarm 1.2.0 (abc1234)\n" {
		t.Errorf("--version: exit %d, %q", code, out)
	}
}

// SSHWARM_READ_ONLY is on when set, but to a false value: a typo errs on the safe side.
func TestEnvOn(t *testing.T) {
	for v, want := range map[string]bool{"": false, "0": false, "false": false, "1": true, "true": true, "yes": true} {
		if got := envOn(v); got != want {
			t.Errorf("SSHWARM_READ_ONLY=%q: on %v, want %v", v, got, want)
		}
	}
}
