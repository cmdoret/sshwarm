package swarm

import (
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// described reads a describe command's recorded output, testdata/<name>.describe.
func described(t *testing.T, name string) string {
	t.Helper()

	raw, err := os.ReadFile("testdata/" + name + ".describe")
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

// What the TUI shows when ssh fails: not a description, so an error.
const sshError = "ssh: connect to host x port 22: Connection refused"

func TestParseMachine(t *testing.T) {
	m, err := ParseMachine(described(t, "machine"))
	if err != nil {
		t.Fatal(err)
	}

	if m.OS != "NixOS 26.05 (Yarara)" || m.Virt != "kvm" || m.CPUs != 4 || m.Load != [3]float64{0.42, 0.35, 0.30} || m.Uptime < 25*time.Hour {
		t.Errorf("system: %+v", m)
	}

	if m.MemTotal != 8029916*1024 || m.MemAvailable != 1572864*1024 || m.SwapTotal != m.SwapFree {
		t.Errorf("memory: %+v", m)
	}

	// df's error about a mount it can't read is skipped; a mount point keeps its spaces.
	if len(m.Disks) != 3 || m.Disks[2].Mount != "/mnt/media files" || m.Disks[0].Used != 56108032*1024 {
		t.Errorf("disks: %+v", m.Disks)
	}

	if fmt.Sprintf("%.1f", m.CPUBusy) != "22.1" || len(m.Processes) != 5 || m.Processes[0] != (Process{PID: "2817", User: "root", Command: "immich", CPU: 61, Mem: 15.3}) {
		t.Errorf("top: %.1f %+v", m.CPUBusy, m.Processes)
	}

	if _, err := ParseMachine(sshError); err == nil {
		t.Error("ssh's error parsed as a machine")
	}

	// An idle machine's 100.0 touches the comma before it: still idle, not busy.
	if busy, _ := parseTop("%Cpu(s):  0.0 us,  0.0 sy,  0.0 ni,100.0 id,  0.0 wa,  0.0 hi,  0.0 si,  0.0 st"); busy != 0 {
		t.Errorf("an idle machine is %.0f%% busy", busy)
	}
}

// DescribeMachine runs here as on a machine, and parses. Only what any Linux has is
// required: the nix build's sandbox has no top or lscpu.
func TestDescribeMachineHere(t *testing.T) {
	out, err := exec.CommandContext(t.Context(), "sh", "-c", DescribeMachine()).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	m, err := ParseMachine(string(out))
	if err != nil || m.CPUs == 0 || m.MemTotal == 0 {
		t.Errorf("%v %+v in:\n%s", err, m, out)
	}
}

func TestParseUnit(t *testing.T) {
	u, err := ParseUnit(described(t, "unit"))
	if err != nil {
		t.Fatal(err)
	}

	if u.Name != "backup.service" || u.Active != "failed" || u.File != "/etc/systemd/system/backup.service" || u.TriggeredBy != "backup.timer" {
		t.Errorf("unit: %+v", u)
	}

	if u.MainPID != 0 || u.ExitStatus != 2 || u.Restarts != 3 || u.Restart != "on-failure" {
		t.Errorf("process: %+v", u)
	}

	// "[not set]" reads as nothing; the inactive cache isn't memory used.
	if u.Memory != 48234496-10485760 || u.MemoryMax != 536870912 || u.CPUTime != 1234567890 || u.Tasks != 0 {
		t.Errorf("usage: %+v", u)
	}

	if len(u.Journal) != 4 || !strings.Contains(u.Journal[1], "connection refused") {
		t.Errorf("journal: %q", u.Journal)
	}

	if _, err := ParseUnit(sshError); err == nil {
		t.Error("ssh's error parsed as a unit")
	}
}

func TestParseStack(t *testing.T) {
	s, err := ParseStack(described(t, "stack"))
	if err != nil {
		t.Fatal(err)
	}

	if s.CPUs != 8 || len(s.Containers) != 2 {
		t.Fatalf("stack: %+v", s)
	}

	// Only the running container has stats.
	job, web := s.Containers[0], s.Containers[1]
	if job.Name != "demo-job-1" || job.Service != "job" || job.State != "exited" || job.Stats != nil {
		t.Errorf("exited container: %+v", job)
	}

	if web.Ports != "127.0.0.1:18080->80/tcp" || web.Stats == nil || web.Stats.MemUsage != "1.73MiB / 15.24GiB" || web.Stats.CPU != 6.86 {
		t.Errorf("running container: %+v %+v", web, web.Stats)
	}

	if _, err := ParseStack(sshError); err == nil {
		t.Error("ssh's error parsed as a stack")
	}
}

func TestParseContainer(t *testing.T) {
	c, err := ParseContainer(described(t, "container"))
	if err != nil {
		t.Fatal(err)
	}

	if c.Name != "demo-web-1" || c.Image != "alpine:latest" || c.Project != "demo" || c.Service != "web" || c.Command != "sleep 300" {
		t.Errorf("container: %+v", c)
	}

	if c.State != "running" || !c.Running || c.StartedAt.IsZero() || c.RestartPolicy != "unless-stopped" || c.CPUs != 8 {
		t.Errorf("state: %+v", c)
	}

	if c.Health != "unhealthy" || c.HealthStreak < 3 || c.HealthLast != "probe failed" {
		t.Errorf("health: %q %d %q", c.Health, c.HealthStreak, c.HealthLast)
	}

	if !slices.Equal(c.Ports, []string{"127.0.0.1:18080 → 80/tcp"}) || !slices.Equal(c.Mounts, []string{"/srv/demo/data → /data (rw)"}) || len(c.Networks) != 1 {
		t.Errorf("ports %q, mounts %q, networks %q", c.Ports, c.Mounts, c.Networks)
	}

	if c.Stats == nil || c.Stats.PIDs != "1" || c.Stats.NetIO != "1.09kB / 126B" {
		t.Errorf("stats: %+v", c.Stats)
	}

	// The environment's names, never its values: they're often secrets.
	if !slices.Contains(c.EnvNames, "DB_PASSWORD") || strings.Contains(fmt.Sprintf("%+v", c), "hunter2") {
		t.Errorf("environment: %q in %+v", c.EnvNames, c)
	}

	if _, err := ParseContainer("Error: No such object: demo-web-1"); err == nil {
		t.Error("docker's error parsed as a container")
	}
}
