// Package swarm talks to the machines over ssh: it lists them, probes them, streams
// their logs and descriptions, and opens shells.
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
)

//go:embed probe.sh
var probeScript string

// ProbeAll probes the hosts in parallel, each within timeout. The reports keep the
// hosts' order.
func ProbeAll(ctx context.Context, hosts []Host, timeout time.Duration) []Report {
	reports := make([]Report, len(hosts))

	var wg sync.WaitGroup

	for i, h := range hosts {
		wg.Add(1)

		go func() {
			defer wg.Done()

			out, err := RunProbe(ctx, h, timeout)
			if err == nil {
				reports[i], err = Parse(out)
			}

			reports[i].Host, reports[i].Err = h, err
		}()
	}

	wg.Wait()

	return reports
}

// RunProbe returns the probe's raw output, without control characters. It gives
// up after timeout.
func RunProbe(ctx context.Context, h Host, timeout time.Duration) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// The probe stops on its own on the host, a bit before we give up: without a
	// terminal, killing ssh wouldn't stop a command stuck there (a hung docker ps).
	onHost := max(int(timeout.Seconds())-2, 1)
	cmd := sshCmd(ctx, h, false, fmt.Sprintf("timeout %d bash -s", onHost))
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
	r := Report{
		OS:            kv["os"],
		Revision:      kv["revision"],
		Now:           unix(kv["now"]),
		Deployed:      unix(kv["deployed"]),
		RebootPending: kv["reboot"] == "true",
		Load:          usage(kv["load"]),
		Memory:        usage(kv["memory"]),
		Disk:          usage(kv["disk"]),
		Docker:        kv["docker"],
		Units:         parseUnits(sec["units"]),
		Stacks:        parseStacks(sec["containers"]),
	}

	if up, err := strconv.ParseFloat(kv["uptime"], 64); err == nil {
		r.Uptime = time.Duration(up * float64(time.Second))
	}

	for name, text := range sec {
		if cmd, ok := strings.CutPrefix(name, "error "); ok {
			r.Errors = append(r.Errors, cmd+": "+oneLine(text))
		}
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

// parseUnits reads systemctl show's output: a block of properties per unit.
func parseUnits(raw string) []Unit {
	var units []Unit

	for _, block := range strings.Split(raw, "\n\n") {
		p := keyValues(block)
		if p["Id"] == "" {
			continue
		}

		units = append(units, Unit{
			Name: p["Id"], Active: p["ActiveState"], Sub: p["SubState"], Description: p["Description"],
			Result: p["Result"], UnitFileState: p["UnitFileState"],
			Since: unix(strings.TrimPrefix(p["StateChangeTimestamp"], "@")),
		})
	}

	return units
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

// keyValues reads key=value lines.
func keyValues(s string) map[string]string {
	kv := map[string]string{}

	for _, line := range strings.Split(s, "\n") {
		if k, v, ok := strings.Cut(line, "="); ok {
			kv[k] = v
		}
	}

	return kv
}

// usage reads a percentage the probe computed; -1 when it couldn't, as 0% would
// pass for a fine machine. Some awks print inf or nan for a division by zero.
func usage(s string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return -1
	}

	return n
}

// unix reads seconds since the epoch; zero or anything else is the zero time.
func unix(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n == 0 {
		return time.Time{}
	}

	return time.Unix(n, 0)
}
