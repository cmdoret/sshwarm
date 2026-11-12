// sshwarm shows the state of the machines deployed from the flake: deployed commit,
// uptime, failed units, compose stacks, and their logs. Read-only.
package main

import (
	"cmp"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

const (
	defaultTimeout = 20 * time.Second
	defaultRefresh = 30 * time.Second
)

// errReported means the output already says what's wrong (status's problems):
// sshwarm exits 1 without repeating it.
var errReported = errors.New("reported")

// options are the global flags, given before the command.
type options struct {
	flake            string
	timeout, refresh time.Duration
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
	fs.StringVar(&opts.flake, "flake", cmp.Or(os.Getenv("SSHWARM_FLAKE"), "."), "")
	fs.DurationVar(&opts.timeout, "timeout", defaultTimeout, "")
	fs.DurationVar(&opts.refresh, "refresh", defaultRefresh, "")

	err := fs.Parse(args)

	return opts, fs.Args(), err
}

// run runs the command first in args, with its own flags and arguments after it.
// Without a command, it opens the browser.
func run(opts options, args []string) error {
	if len(args) == 0 {
		return swarm.RunTUI(opts.flake, opts.timeout, opts.refresh)
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

	hosts, err := machines(ctx, opts.flake, fs.Args())
	if err != nil {
		return err
	}

	if !swarm.PrintStatus(os.Stdout, swarm.ProbeAll(ctx, hosts, opts.timeout, false)) {
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

	hosts, err := machines(context.Background(), opts.flake, fs.Args())
	if err != nil {
		return err
	}

	if len(hosts) > 1 {
		return fmt.Errorf("%q matches %d machines", fs.Arg(0), len(hosts))
	}

	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	out, err := swarm.RunProbe(ctx, hosts[0], false)
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

// machines loads the flake's machines and keeps those named, or all without names.
func machines(ctx context.Context, flake string, names []string) ([]swarm.Host, error) {
	hosts, err := swarm.LoadHosts(ctx, flake)
	if err != nil {
		return nil, err
	}

	hosts = selectHosts(hosts, names)
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no machine matches %v", names)
	}

	return hosts, nil
}

// selectHosts keeps the machines whose name contains one of names (argiope, garage).
func selectHosts(hosts []swarm.Host, names []string) []swarm.Host {
	if len(names) == 0 {
		return hosts
	}

	var out []swarm.Host

	for _, h := range hosts {
		if slices.ContainsFunc(names, func(n string) bool { return strings.Contains(h.Name, n) }) {
			out = append(out, h)
		}
	}

	return out
}

func usage() {
	fmt.Printf(`Usage: sshwarm [flags] [command] [machine...]

  (no command)          browse the machines, their services, stacks and logs
  status [machine...]   print the machines table; exit 1 on any problem
  probe <machine>       print a machine's raw probe output (debugging, test fixtures)
  help                  print this message

A machine is named by any part of its name: web-1, or web for all web machines.

Flags, before the command:

  --flake <dir>          the flake defining deploy.nodes (default: $SSHWARM_FLAKE, or .)
  --timeout <duration>   probe timeout, per machine (default: %v)
  --refresh <duration>   how often the browser refreshes (default: %v)
`, defaultTimeout, defaultRefresh)
}
