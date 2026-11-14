package ui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/swarm"
)

// prober probes the machines, one probe at a time, and again every interval.
type prober struct {
	ctx               context.Context
	hosts             []inventory.Host
	timeout, interval time.Duration

	running bool      // a probe is
	again   bool      // a refresh was asked for while it ran: probe once it's in
	gen     int       // of the tick that probes next: a later one supersedes it
	last    time.Time // when the last probe ended
}

// probe probes the machines: reportsMsg brings what it found.
func (p *prober) probe() tea.Cmd {
	ctx, hosts, timeout := p.ctx, p.hosts, p.timeout

	return func() tea.Msg { return reportsMsg(swarm.ProbeAll(ctx, hosts, timeout)) }
}

// refresh probes the machines again. If a probe is running, it probes once that one
// is in, as that one may predate what asked for this one.
func (p *prober) refresh() tea.Cmd {
	if p.running {
		p.again = true

		return nil
	}

	p.running = true

	return p.probe()
}

// done takes note that a probe ended: the next tick probes after interval, unless
// another was asked for meanwhile, which probes now.
func (p *prober) done() tea.Cmd {
	p.running, p.last = false, time.Now()
	p.gen++
	gen := p.gen
	tick := tea.Tick(p.interval, func(time.Time) tea.Msg { return tickMsg(gen) })

	if !p.again {
		return tick
	}

	p.again = false

	return tea.Batch(tick, p.refresh())
}

// tick probes on the latest tick, unless a probe is running: its end sets the next.
func (p *prober) tick(gen int) tea.Cmd {
	if gen != p.gen || p.running {
		return nil
	}

	return p.refresh()
}
