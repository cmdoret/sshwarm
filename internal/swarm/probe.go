// Package swarm probes machines over ssh, read-only, and shows what it finds:
// a status table, and a TUI with their services, stacks and logs.
package swarm

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed probe.sh
var probeScript string

// sshCmd runs remote on h. It never prompts and gives up on dead hosts and dead
// connections quickly. mux shares one connection per host (the TUI); tty gives the
// remote command a terminal, so it's hung up when ssh ends (needed for -f).
func sshCmd(ctx context.Context, h Host, mux, tty bool, remote string) *exec.Cmd {
	args := []string{
		"-o", "BatchMode=yes",
		"-o", "ConnectTimeout=5",
		"-o", "ServerAliveInterval=10",
		"-o", "ServerAliveCountMax=2",
	}
	if mux {
		args = append(args, "-o", "ControlMaster=auto", "-o", "ControlPath=~/.ssh/sshwarm-%C", "-o", "ControlPersist=60s")
	}
	if tty {
		args = append(args, "-tt", "-q")
	}
	cmd := exec.CommandContext(ctx, "ssh", slices.Concat(args, h.Opts, []string{"--", h.Address, remote})...)
	// A new ControlPersist master keeps ssh's output open in the background: don't
	// wait for it once ssh itself has exited. Wait then returns exec.ErrWaitDelay.
	cmd.WaitDelay = time.Second
	return cmd
}

// LoadHosts reads deploy.nodes from the flake in flakeDir, with deploy-rs's
// defaults for sshUser and sshOpts.
func LoadHosts(ctx context.Context, flakeDir string) ([]Host, error) {
	// Absolute, or nix reads a bare "nix" as the registry's github:NixOS/nix.
	dir, err := filepath.Abs(flakeDir)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, "nix", "eval", "--json", dir+"#deploy", "--apply", `d: builtins.mapAttrs (_: n: {
		inherit (n) hostname;
		sshUser = n.sshUser or d.sshUser or "";
		sshOpts = n.sshOpts or d.sshOpts or [ ];
	}) d.nodes`)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("nix eval: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var nodes map[string]struct {
		Hostname, SSHUser string
		SSHOpts           []string
	}
	if err := json.Unmarshal(out, &nodes); err != nil {
		return nil, fmt.Errorf("nix eval output: %w", err)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("no machines in %s#deploy.nodes", dir)
	}
	var hosts []Host
	for _, name := range slices.Sorted(maps.Keys(nodes)) {
		n := nodes[name]
		addr := n.Hostname
		if n.SSHUser != "" {
			addr = n.SSHUser + "@" + addr
		}
		hosts = append(hosts, Host{Name: name, Address: addr, Opts: n.SSHOpts})
	}
	return hosts, nil
}

// ProbeAll probes the hosts in parallel; reports keep the hosts' order.
func ProbeAll(ctx context.Context, hosts []Host, timeout time.Duration, mux bool) []Report {
	reports := make([]Report, len(hosts))
	var wg sync.WaitGroup
	for i, h := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			reports[i] = probe(ctx, h, mux)
		}()
	}
	wg.Wait()
	return reports
}

func probe(ctx context.Context, h Host, mux bool) Report {
	r := Report{Host: h}
	out, err := RunProbe(ctx, h, mux)
	if err == nil {
		err = parse(&r, out)
	}
	r.Err = err
	return r
}

// RunProbe returns the probe's raw output, without control characters.
func RunProbe(ctx context.Context, h Host, mux bool) (string, error) {
	// The remote side stops on its own, a bit before us: without a terminal, killing
	// ssh here wouldn't stop a command stuck there (a hung docker ps).
	secs := 60
	if d, ok := ctx.Deadline(); ok {
		secs = max(int(time.Until(d).Seconds())-2, 1)
	}
	cmd := sshCmd(ctx, h, mux, false, fmt.Sprintf("timeout %d bash -s", secs))
	cmd.Stdin = strings.NewReader(probeScript)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	raw, err := cmd.Output()
	out := clean(string(raw))

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", errors.New("timed out")
	}

	if err == nil || errors.Is(err, exec.ErrWaitDelay) {
		return out, nil
	}

	if !strings.Contains(out, "### meta\n") {
		// Nothing ran. ssh exits 255 on connection errors; its message is the useful part.
		if msg := oneLine(stderr.String()); msg != "" {
			return "", errors.New(msg)
		}

		return "", err
	}

	// Part of the probe ran: keep what it printed, and say why it stopped.
	why := err.Error()

	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 124 {
		why = "timed out on the host"
	}

	return out + "\n### error probe\n" + why + "\n", nil
}

// sections splits the probe output on its "### <name>" lines.
func sections(out string) map[string]string {
	s := map[string]string{}
	for _, part := range strings.Split("\n"+out, "\n### ")[1:] {
		name, body, _ := strings.Cut(part, "\n")
		s[name] = body
	}
	return s
}

// parse fills r from the probe output. Only a missing meta section is fatal:
// a command that failed, or output that doesn't parse, becomes one of r.Errors.
func parse(r *Report, out string) error {
	sec := sections(out)
	meta, ok := sec["meta"]
	if !ok {
		return errors.New("no probe output")
	}
	kv := keyValues(meta)
	r.Now = unix(kv["now"])
	if up, err := strconv.ParseFloat(kv["uptime"], 64); err == nil {
		r.Uptime = time.Duration(up * float64(time.Second))
	}
	r.Generation, _ = strconv.Atoi(kv["generation"])
	r.Deployed = unix(kv["deployed"])
	r.RebootPending = kv["reboot"] == "true"
	r.HasDocker = kv["docker"] == "true"
	r.Stacks = parseStacks(sec["containers"])

	for name, text := range sec {
		if cmd, ok := strings.CutPrefix(name, "error "); ok {
			r.Errors = append(r.Errors, cmd+": "+oneLine(text))
		}
	}
	slices.Sort(r.Errors)
	if raw, ok := sec["version"]; ok {
		var v struct{ NixosVersion, ConfigurationRevision string }
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			r.Errors = append(r.Errors, "version: "+err.Error())
		}
		r.Version, r.Revision = v.NixosVersion, v.ConfigurationRevision
	}
	raw, ok := sec["units"]
	if !ok {
		return nil
	}
	var units []struct{ Unit, Active, Sub, Description string }
	if err := json.Unmarshal([]byte(raw), &units); err != nil {
		r.Errors = append(r.Errors, "units: "+err.Error())
		return nil
	}
	props := map[string]map[string]string{}
	for _, block := range strings.Split(sec["show"], "\n\n") {
		p := keyValues(block)
		props[p["Id"]] = p
	}
	for _, u := range units {
		p := props[u.Unit]
		r.Units = append(r.Units, Unit{
			Name: u.Unit, Active: u.Active, Sub: u.Sub, Description: u.Description,
			Result: p["Result"], UnitFileState: p["UnitFileState"],
			Since: unix(strings.TrimPrefix(p["StateChangeTimestamp"], "@")),
		})
	}
	return nil
}

// parseStacks groups the containers (project, name, state, status per line) by
// compose project.
func parseStacks(raw string) []Stack {
	byName := map[string][]Container{}
	for _, line := range strings.Split(raw, "\n") {
		if f := strings.Split(line, "\t"); len(f) == 4 && f[0] != "" {
			byName[f[0]] = append(byName[f[0]], Container{Name: f[1], State: f[2], Status: f[3]})
		}
	}
	var out []Stack
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		out = append(out, Stack{Name: name, Containers: byName[name]})
	}
	return out
}

func keyValues(s string) map[string]string {
	kv := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			kv[k] = v
		}
	}
	return kv
}

func unix(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n == 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}
