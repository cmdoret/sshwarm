// sshwarm shows the state of Linux machines over ssh: uptime, pending reboots,
// CPU, memory and disk use, failed units, compose stacks, and their logs.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/swarm"
	"github.com/cmdoret/sshwarm/internal/ui"
)

const (
	defaultTimeout = 20 * time.Second
	defaultRefresh = 30 * time.Second
)

// errReported means the output already says what's wrong (status's problems):
// sshwarm exits 1 without repeating it.
var errReported = errors.New("reported")

// version and commit are stamped by the Nix build.
// Without them, versionLine falls back on what Go records itself.
var version, commit string

// options are the global flags, given before the command.
type options struct {
	hosts, flake      string
	timeout, refresh  time.Duration
	version, readOnly bool
}

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, os.Getenv))
}

// run runs sshwarm with args, and returns its exit status. It writes to stdout, its
// errors to stderr, and reads the environment with getenv.
func run(ctx context.Context, args []string, stdout, stderr io.Writer, getenv func(string) string) int {
	err := dispatch(ctx, args, stdout, getenv)
	if errors.Is(err, flag.ErrHelp) { // sshwarm -h
		err = usage(stdout)
	}

	switch {
	case err == nil:
		return 0
	case !errors.Is(err, errReported): // else the output already says what's wrong
		_, _ = fmt.Fprintf(stderr, "sshwarm: %v\n", err) // nowhere left to say it failed
	}

	return 1
}

// dispatch parses the global flags, then runs the command first in the arguments
// after them, with its own flags and arguments. Without a command, it opens the
// browser.
func dispatch(ctx context.Context, args []string, stdout io.Writer, getenv func(string) string) error {
	var opts options

	fs := globalFlags(&opts, getenv)
	if err := fs.Parse(args); err != nil {
		return err
	}

	args = fs.Args()

	if opts.version {
		_, err := fmt.Fprintln(stdout, versionLine())

		return err
	}

	if len(args) == 0 {
		hosts, err := loadHosts(ctx, opts)
		if err != nil {
			return err
		}

		return ui.Run(ctx, hosts, opts.timeout, opts.refresh, opts.readOnly)
	}

	switch args[0] {
	case "status":
		return statusCmd(ctx, opts, args[1:], stdout)
	case "probe":
		return probeCmd(ctx, opts, args[1:], stdout)
	}

	return fmt.Errorf("unknown command %q, see sshwarm --help", args[0])
}

// globalFlags is the flag set of the flags before the command, set into opts; their
// defaults come from the environment, read with getenv.
func globalFlags(opts *options, getenv func(string) string) *flag.FlagSet {
	fs := newFlagSet("sshwarm")
	fs.StringVar(&opts.hosts, "hosts", getenv("SSHWARM_HOSTS"), "")
	fs.StringVar(&opts.flake, "flake", getenv("SSHWARM_FLAKE"), "")
	fs.DurationVar(&opts.timeout, "timeout", defaultTimeout, "")
	fs.DurationVar(&opts.refresh, "refresh", defaultRefresh, "")
	fs.BoolVar(&opts.version, "version", false, "")
	fs.BoolVar(&opts.readOnly, "read-only", envOn(getenv("SSHWARM_READ_ONLY")), "")

	return fs
}

// envOn says whether an environment variable's value v turns something on: set, but
// not to a false value like 0 or false. Any other value is on, so that a typo errs
// on the safe side.
func envOn(v string) bool {
	on, err := strconv.ParseBool(v)

	return v != "" && (on || err != nil)
}

const statusHelp = `Usage: sshwarm [flags] status [--format text|json] [machine...]

Print the machines table and exit 1 on any problem. Machines can
be selected by substrings of their name. All machines by default.

  --format text|json   output format. Text by default.
`

// statusFlags is the status command's flag set, and its format flag.
func statusFlags() (*flag.FlagSet, *string) {
	fs := newFlagSet("status")

	return fs, fs.String("format", "text", "")
}

// statusCmd prints the machines table, then their problems, if any.
func statusCmd(ctx context.Context, opts options, args []string, stdout io.Writer) error {
	fs, format := statusFlags()
	if err := fs.Parse(args); err != nil {
		return helpOr(err, statusHelp, stdout)
	}

	if *format != "text" && *format != "json" {
		return fmt.Errorf("unknown format %q: text or json", *format)
	}

	hosts, err := loadHosts(ctx, opts)
	if err != nil {
		return err
	}

	hosts = inventory.Select(hosts, fs.Args())
	if len(hosts) == 0 {
		return fmt.Errorf("no machine matches %v", fs.Args())
	}

	reports := swarm.ProbeAll(ctx, hosts, opts.timeout)

	var ok bool

	switch *format {
	case "text":
		ok, err = ui.PrintStatus(stdout, reports)
	case "json":
		ok, err = ui.PrintStatusJSON(stdout, reports)
	}

	if err != nil {
		return err
	}

	if !ok {
		return errReported
	}

	return nil
}

const probeHelp = `Usage: sshwarm [flags] probe <machine>

Print a machine's raw probe output, mainly for debugging. The machine
can be referenced by a substring of its name.
`

// probeCmd prints one machine's raw probe output, for debugging and test fixtures.
func probeCmd(ctx context.Context, opts options, args []string, stdout io.Writer) error {
	fs := newFlagSet("probe")
	if err := fs.Parse(args); err != nil {
		return helpOr(err, probeHelp, stdout)
	}

	if fs.NArg() != 1 {
		return errors.New("probe takes one machine")
	}

	hosts, err := loadHosts(ctx, opts)
	if err != nil {
		return err
	}

	i, err := inventory.Find(hosts, fs.Arg(0))
	if err != nil {
		return err
	}

	out, err := swarm.RunProbe(ctx, hosts[i], opts.timeout)
	if err != nil {
		return err
	}

	_, err = io.WriteString(stdout, out)

	return err
}

// helpOr writes a command's help to w if err asks for it (-h, --help), and then
// there's nothing more to do; else it's err.
func helpOr(err error, help string, w io.Writer) error {
	if !errors.Is(err, flag.ErrHelp) {
		return err
	}

	_, err = io.WriteString(w, help)

	return err
}

// newFlagSet returns a flag set that prints nothing: main prints the error, or
// the usage for -h/--help. Flags take one or two dashes: --flake or -flake.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	return fs
}

// loadHosts loads the machines, from the hosts file or else the flake.
func loadHosts(ctx context.Context, opts options) ([]inventory.Host, error) {
	switch {
	case opts.hosts != "":
		return inventory.LoadFile(opts.hosts)
	case opts.flake != "":
		return inventory.LoadFlake(ctx, opts.flake)
	}

	return nil, errors.New("no hosts provided: use --hosts <file> or --flake <dir>, or set SSHWARM_HOSTS or SSHWARM_FLAKE")
}

// usage writes sshwarm's help to w.
func usage(w io.Writer) error {
	_, err := fmt.Fprintf(w, `Usage: sshwarm [flags] [command] [machine...]

  (no command)          browse the machines, their services, stacks and logs
  status [machine...]   print the machines table; exit 1 on any problem
  probe <machine>       print a machine's raw probe output (debugging)

Machine can be selected by substrings of their name. See a command's own flags
with sshwarm <command> --help.

Flags, before the command:

  --hosts <file>         the TOML hosts file, see below (default: $SSHWARM_HOSTS)
  --flake <dir>          without --hosts, the flake defining deploy.nodes (default: $SSHWARM_FLAKE)
  --timeout <duration>   probe timeout, per machine (default: %v)
  --refresh <duration>   how often the browser refreshes (default: %v)
  --read-only            don't start, stop, restart or reboot anything, nor open shells (default: $SSHWARM_READ_ONLY set, but to 0 or false)
  --version              print the version and the commit it was built from

Hosts file, one [[host]] per machine. ssh looks up its host, or else its name, in
~/.ssh/config for anything not set here:

  [[host]]
  name = "db"                  # ssh db

  [[host]]
  name = "web-1"
  host = "web-1.example.org"   # ssh -i ~/.ssh/web deploy@web-1.example.org
  user = "deploy"
  key  = "~/.ssh/web"
`, defaultTimeout, defaultRefresh)

	return err
}

// versionLine is "sshwarm x.y.z (<commit>)".
func versionLine() string {
	v, c := version, commit

	if info, ok := debug.ReadBuildInfo(); ok {
		v = cmp.Or(v, info.Main.Version)

		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				c = cmp.Or(c, s.Value)
			}
		}
	}

	line := "sshwarm " + strings.TrimPrefix(cmp.Or(v, "(devel)"), "v")
	if c != "" {
		line += " (" + c + ")"
	}

	return line
}
