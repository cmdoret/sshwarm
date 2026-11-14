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
	"strings"
	"time"

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
	hosts, flake     string
	timeout, refresh time.Duration
	version          bool
}

func main() {
	opts, args, err := parseGlobal(os.Args[1:])
	if err == nil {
		err = run(opts, args)
	}

	if err == nil {
		return
	}

	if errors.Is(err, flag.ErrHelp) {
		usage()

		return
	}

	if !errors.Is(err, errReported) {
		fmt.Fprintf(os.Stderr, "sshwarm: %v\n", err)
	}

	os.Exit(1)
}

// parseGlobal parses the flags before the command, and returns the arguments after them.
func parseGlobal(args []string) (options, []string, error) {
	var opts options

	fs := newFlagSet("sshwarm")
	fs.StringVar(&opts.hosts, "hosts", os.Getenv("SSHWARM_HOSTS"), "")
	fs.StringVar(&opts.flake, "flake", cmp.Or(os.Getenv("SSHWARM_FLAKE"), "."), "")
	fs.DurationVar(&opts.timeout, "timeout", defaultTimeout, "")
	fs.DurationVar(&opts.refresh, "refresh", defaultRefresh, "")
	fs.BoolVar(&opts.version, "version", false, "")

	err := fs.Parse(args)

	return opts, fs.Args(), err
}

// run runs the command first in args, with its own flags and arguments after it.
// Without a command, it opens the browser.
func run(opts options, args []string) error {
	if opts.version {
		fmt.Println(versionLine())

		return nil
	}

	if len(args) == 0 {
		hosts, err := loadHosts(context.Background(), opts)
		if err != nil {
			return err
		}

		return ui.Run(hosts, opts.timeout, opts.refresh)
	}

	switch args[0] {
	case "status":
		return statusCmd(opts, args[1:])
	case "probe":
		return probeCmd(opts, args[1:])
	case "help":
		return flag.ErrHelp // main prints the usage
	default:
		return fmt.Errorf("unknown command %q, see sshwarm help", args[0])
	}
}

// statusCmd prints the machines table, then their problems, if any.
func statusCmd(opts options, args []string) error {
	fs := newFlagSet("status")
	if err := fs.Parse(args); err != nil {
		return err
	}

	ctx := context.Background()

	hosts, err := loadHosts(ctx, opts)
	if err != nil {
		return err
	}

	hosts = swarm.Select(hosts, fs.Args())
	if len(hosts) == 0 {
		return fmt.Errorf("no machine matches %v", fs.Args())
	}

	if !ui.PrintStatus(os.Stdout, swarm.ProbeAll(ctx, hosts, opts.timeout)) {
		return errReported
	}

	return nil
}

// probeCmd prints one machine's raw probe output, for debugging and test fixtures.
func probeCmd(opts options, args []string) error {
	fs := newFlagSet("probe")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if fs.NArg() != 1 {
		return errors.New("probe takes one machine")
	}

	hosts, err := loadHosts(context.Background(), opts)
	if err != nil {
		return err
	}

	i, err := swarm.Find(hosts, fs.Arg(0))
	if err != nil {
		return err
	}

	out, err := swarm.RunProbe(context.Background(), hosts[i], opts.timeout)
	if err != nil {
		return err
	}

	fmt.Print(out)

	return nil
}

// newFlagSet returns a flag set that prints nothing: main prints the error, or
// the usage for -h/--help. Flags take one or two dashes: --flake or -flake.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	return fs
}

// loadHosts loads the machines, from the hosts file or else the flake.
func loadHosts(ctx context.Context, opts options) ([]swarm.Host, error) {
	if opts.hosts != "" {
		return swarm.LoadFile(opts.hosts)
	}

	return swarm.LoadFlake(ctx, opts.flake)
}

func usage() {
	fmt.Printf(`Usage: sshwarm [flags] [command] [machine...]

  (no command)          browse the machines, their services, stacks and logs
  status [machine...]   print the machines table; exit 1 on any problem
  probe <machine>       print a machine's raw probe output (debugging, test fixtures)
  help                  print this message

Machine can be referenced by substrings of their name.

Flags, before the command:

  --hosts <file>         the TOML hosts file, see below (default: $SSHWARM_HOSTS)
  --flake <dir>          without --hosts, the flake defining deploy.nodes (default: $SSHWARM_FLAKE, or .)
  --timeout <duration>   probe timeout, per machine (default: %v)
  --refresh <duration>   how often the browser refreshes (default: %v)
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
