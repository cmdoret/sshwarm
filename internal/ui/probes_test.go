package ui

import (
	"testing"
	"time"
)

// The machines are probed one probe at a time: a refresh asked for during one
// probes once it's in, and neither a tick during a probe nor a stale tick probes.
func TestProber(t *testing.T) {
	p := prober{ctx: t.Context(), interval: time.Hour}

	if p.refresh() == nil || !p.running {
		t.Fatal("a refresh doesn't probe")
	}

	if p.refresh() != nil || !p.again {
		t.Error("a refresh during a probe probes, or isn't kept for after it")
	}

	if p.tick(p.gen) != nil {
		t.Error("a tick during a probe probes")
	}

	stale := p.gen

	if p.done() == nil || !p.running || p.again {
		t.Error("the refresh asked for meanwhile doesn't probe once the probe is in")
	}

	p.done()

	if p.tick(stale) != nil {
		t.Error("a superseded tick probes")
	}

	if p.tick(p.gen) == nil || !p.running {
		t.Error("the latest tick doesn't probe")
	}
}
