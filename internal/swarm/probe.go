// Package swarm talks to the machines over ssh: it lists them, probes them, streams
// their logs and descriptions, opens shells and runs actions.
package swarm

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"maps"
	"math"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/remote"
)

//go:embed probe.sh
var probeScript string

// ProbeAll probes the hosts in parallel, each within timeout. The reports keep the
// hosts' order.
func ProbeAll(ctx context.Context, hosts []inventory.Host, timeout time.Duration) []Report {
	reports := make([]Report, len(hosts))

	var wg sync.WaitGroup

	for i, h := range hosts {
		wg.Go(func() {
			out, err := RunProbe(ctx, h, timeout)
			if err == nil {
				reports[i], err = Parse(out)
			}

			reports[i].Host, reports[i].Err = h, err
		})
	}

	wg.Wait()

	return reports
}

// RunProbe returns the probe's raw output, without control characters. It gives
// up after timeout.
func RunProbe(ctx context.Context, h inventory.Host, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The probe stops on its own on the host, a bit before we give up: without a
	// terminal, killing ssh wouldn't stop a command stuck there.
	onHost := max(int(timeout.Seconds())-2, 1)
	cmd := remote.Command(ctx, h, false, fmt.Sprintf("timeout %d bash -s", onHost))
	cmd.Stdin = strings.NewReader(probeScript)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	raw, err := cmd.Output()
	out := remote.Clean(string(raw))

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "", fmt.Errorf("timed out after %v", timeout)
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

// Parse reads a probe's output, as RunProbe returns it. Only a missing meta section
// is an error: a command that failed, or output that doesn't parse, becomes one of
// Report.Errors.
func Parse(out string) (Report, error) {
	sec := sections(out)

	meta, ok := sec["meta"]
	if !ok {
		return Report{}, errors.New("no probe output")
	}

	kv := keyValues(meta)
	bad := misread{}
	now := unix(kv["now"])
	r := Report{
		OS:            kv["os"],
		Revision:      kv["revision"],
		Now:           now,
		Deployed:      unix(kv["deployed"]),
		RebootPending: kv["reboot"] == "true",
		Load:          usage(kv["load"]),
		Memory:        usage(kv["memory"]),
		Disk:          usage(kv["disk"]),
		Docker:        Docker(kv["docker"]),
		Units:         withUnloaded(parseUnits(sec["units"], now, bad), sec["unit-files"], bad),
		Stacks:        parseStacks(sec["containers"], parseStates(sec["states"], now, bad), bad),
	}

	if up, err := strconv.ParseFloat(kv["uptime"], 64); err == nil {
		r.Uptime = time.Duration(up * float64(time.Second))
	}

	for name, text := range sec {
		if cmd, ok := strings.CutPrefix(name, "error "); ok {
			r.Errors = append(r.Errors, cmd+": "+oneLine(text))
		}
	}

	for name, n := range bad {
		r.Errors = append(r.Errors, fmt.Sprintf("%s: %d unreadable lines", name, n))
	}

	slices.Sort(r.Errors)

	return r, nil
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

// misread counts, per section of the probe's output, the lines that don't read: a
// sign that the output of docker or systemd changed, where skipping them would
// pass for having nothing to show.
type misread map[string]int

// note counts line, of section, as misread, unless it's blank.
func (m misread) note(section, line string) {
	if strings.TrimSpace(line) != "" {
		m[section]++
	}
}

// parseUnits reads systemctl show's output: a block of properties per unit. now is
// the host's clock, which tells a recent restart.
func parseUnits(raw string, now time.Time, bad misread) []Unit {
	var units []Unit

	for block := range strings.SplitSeq(raw, "\n\n") {
		p := keyValues(block)
		if p["Id"] == "" {
			bad.note("units", block)

			continue
		}

		// A reload changes the state, not when it last became active: a restart does.
		restarts := int(count(p["NRestarts"]))
		started := unix(strings.TrimPrefix(p["ActiveEnterTimestamp"], "@"))

		units = append(units, Unit{
			Name: p["Id"], Active: p["ActiveState"], Sub: p["SubState"], Description: p["Description"],
			Result: p["Result"], UnitFileState: p["UnitFileState"],
			Since:    unix(strings.TrimPrefix(p["StateChangeTimestamp"], "@")),
			Restarts: restarts, RecentRestart: restartedRecently(restarts, started, now),
		})
	}

	return units
}

// withUnloaded adds to units the installed units systemd hasn't loaded, from
// systemctl list-unit-files (name, then state, per line): inactive. Templates
// (foo@.service) and aliases aren't units of their own.
func withUnloaded(units []Unit, raw string, bad misread) []Unit {
	loaded := map[string]bool{}
	for _, u := range units {
		loaded[u.Name] = true
	}

	for line := range strings.SplitSeq(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			bad.note("unit-files", line)

			continue
		}

		if loaded[f[0]] || strings.HasSuffix(f[0], "@.service") || f[1] == "alias" {
			continue
		}

		units = append(units, Unit{Name: f[0], Active: "inactive", Sub: "dead", UnitFileState: f[1]})
	}

	return units
}

// parseStacks groups the containers (project, name, state, status per line) by
// compose project, with what docker inspect says of each (parseStates).
func parseStacks(raw string, states map[string]Container, bad misread) []Stack {
	byName := map[string][]Container{}

	for line := range strings.SplitSeq(raw, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 4 || f[0] == "" {
			bad.note("containers", line)

			continue
		}

		c := states[f[1]]
		c.Name, c.State, c.Status = f[1], f[2], f[3]
		byName[f[0]] = append(byName[f[0]], c)
	}

	var out []Stack
	for _, name := range slices.Sorted(maps.Keys(byName)) {
		out = append(out, Stack{Name: name, Containers: byName[name]})
	}

	return out
}

// stateFormat is what docker inspect says of a container, for parseStates: its
// name, when it last started and stopped, its last exit code, its restarts, and its
// health, "-" without a healthcheck. probe.sh has it too.
const stateFormat = `{{.Name}} {{.State.StartedAt}} {{.State.FinishedAt}} {{.State.ExitCode}} {{.RestartCount}} {{if .State.Health}}{{.State.Health.Status}}{{else}}-{{end}}`

// parseStates reads docker inspect's lines in stateFormat, into containers by name:
// their health, last exit code, restarts, and since when they're in their state,
// the later of the two times. A container that never ran has docker's zero time
// for both, the zero time. now is the host's clock, which tells a recent restart.
func parseStates(raw string, now time.Time, bad misread) map[string]Container {
	states := map[string]Container{}

	for line := range strings.SplitSeq(raw, "\n") {
		f := strings.Fields(line)
		if len(f) != 6 {
			bad.note("states", line)

			continue
		}

		started, err1 := time.Parse(time.RFC3339Nano, f[1])
		stopped, err2 := time.Parse(time.RFC3339Nano, f[2])
		code, err3 := strconv.Atoi(f[3])
		restarts, err4 := strconv.Atoi(f[4])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			bad.note("states", line)

			continue
		}

		c := Container{
			Name: strings.TrimPrefix(f[0], "/"), ExitCode: code, Since: started,
			Restarts: restarts, RecentRestart: restartedRecently(restarts, started, now),
		}
		if stopped.After(started) { // exited, or waiting to restart
			c.Since = stopped
		}

		if f[5] != "-" {
			c.Health = f[5]
		}

		states[c.Name] = c
	}

	return states
}

// keyValues reads key=value lines.
func keyValues(s string) map[string]string {
	kv := map[string]string{}

	for line := range strings.SplitSeq(s, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			kv[k] = v
		}
	}

	return kv
}

// usage reads a percentage the probe computed; nil when it couldn't, as 0% would
// pass for a fine machine. Some awks print inf or nan for a division by zero.
func usage(s string) *float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return nil
	}

	return &n
}

// unix reads seconds since the epoch; zero or anything else is the zero time.
func unix(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n == 0 {
		return time.Time{}
	}

	return time.Unix(n, 0)
}
