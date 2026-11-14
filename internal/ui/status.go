package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// PrintStatus writes the machines table, then what's wrong. It returns false if any
// machine is unreachable or has a problem, and the error of writing to w, if any.
func PrintStatus(w io.Writer, reports []swarm.Report) (bool, error) {
	columns := len(machineTitles) - 1 // PROBLEMS go below the table
	lines := []string{strings.Join(machineTitles[:columns], "\t")}

	var details []string

	for _, r := range reports {
		var cells []string
		for _, c := range machineRow(r).cells[:columns] {
			cells = append(cells, c.text)
		}

		lines = append(lines, strings.Join(cells, "\t"))

		for _, p := range r.Problems() {
			details = append(details, r.Host.Name+" "+p)
		}
	}

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := io.WriteString(tw, strings.Join(lines, "\n")+"\n"); err != nil {
		return false, err
	}

	if err := tw.Flush(); err != nil {
		return false, err
	}

	if len(details) > 0 {
		if _, err := fmt.Fprintf(w, "\n%s\n", strings.Join(details, "\n")); err != nil {
			return false, err
		}
	}

	return len(details) == 0, nil
}

// machineJSON is a machine in `sshwarm status --format json`: the table's columns as
// values, and the machine's problems. A machine that's down has only its name, state
// and problems.
type machineJSON struct {
	Name          string     `json:"name"`
	State         string     `json:"state"`              // ok, degraded or down
	Deployed      *time.Time `json:"deployed,omitempty"` // NixOS
	Uptime        *int64     `json:"uptime_seconds,omitempty"`
	RebootPending *bool      `json:"reboot_pending,omitempty"`
	Units         *count     `json:"units,omitempty"`  // as the services tab lists them
	Stacks        *count     `json:"stacks,omitempty"` // with docker
	Load          *float64   `json:"load,omitempty"`   // percent per CPU, past 100 when tasks wait
	Memory        *float64   `json:"memory,omitempty"` // percent
	Disk          *float64   `json:"disk,omitempty"`   // percent, of the fullest local disk
	Problems      []string   `json:"problems"`
}

// count is how many of something are fine, of how many.
type count struct {
	OK    int `json:"ok"`
	Total int `json:"total"`
}

// PrintStatusJSON writes the machines as a JSON array of machineJSON. Like
// PrintStatus, it returns false if any machine is unreachable or has a problem.
func PrintStatusJSON(w io.Writer, reports []swarm.Report) (bool, error) {
	ok := true

	var machines []machineJSON

	for _, r := range reports {
		m := statusOf(r)
		ok = ok && m.State == "ok"
		machines = append(machines, m)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	return ok, enc.Encode(machines)
}

// statusOf is r as machineJSON.
func statusOf(r swarm.Report) machineJSON {
	_, state := machineState(r)
	m := machineJSON{Name: r.Host.Name, State: state, Problems: append([]string{}, r.Problems()...)} // [] without any

	if r.Err != nil {
		return m
	}

	if !r.Deployed.IsZero() {
		m.Deployed = new(r.Deployed.UTC())
	}

	watched, failed := r.Watched(), len(r.Failed())
	m.Uptime, m.RebootPending = new(int64(r.Uptime.Seconds())), new(r.RebootPending)
	m.Units = &count{OK: watched - failed, Total: watched}

	if r.Docker != "" {
		m.Stacks = &count{OK: r.StacksUp(), Total: len(r.Stacks)}
	}

	m.Load, m.Memory, m.Disk = known(r.Load), known(r.Memory), known(r.Disk)

	return m
}

// known is a percentage the probe measured, or nil if it couldn't (-1).
func known(percent float64) *float64 {
	if percent < 0 {
		return nil
	}

	return &percent
}
