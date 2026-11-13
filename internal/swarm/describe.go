package swarm

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Describe commands print sections, as the probe does: a "### <name>" line, then a
// command's output, made to be parsed. Parse* read them back for the TUI's cards.
// Only a missing or broken main section is an error: the TUI then shows the output
// as it came, which tells why (ssh's error, "No such container"...). The other
// sections may be missing, and leave their fields zero.

// inSections runs commands one after the other, each output under a "### <name>"
// line, as sections reads them back.
func inSections(named ...[2]string) string {
	var cmds []string
	for _, nc := range named {
		cmds = append(cmds, "echo '### "+nc[0]+"'; "+nc[1])
	}

	return strings.Join(cmds, "; ")
}

// --- machine ----------------------------------------------------------------

// Machine is what DescribeMachine found on a machine.
type Machine struct {
	OS, Kernel, Arch, Virt, Model string // Virt: e.g. kvm, "none" on bare metal
	CPUs                          int
	Uptime                        time.Duration
	Load                          [3]float64 // over 1, 5 and 15 minutes
	CPUBusy                       float64    // percent of all CPUs busy over a second; -1 without top

	MemTotal, MemAvailable, SwapTotal, SwapFree uint64 // bytes

	Disks     []Disk
	Processes []Process // the busiest, from top
}

// Disk is a mounted filesystem, in bytes.
type Disk struct {
	Mount            string
	Size, Used, Free uint64
}

// Process is one of top's processes; CPU and Mem in percent.
type Process struct {
	PID, User, Command string
	CPU, Mem           float64
}

// DescribeMachine is the describe command of a machine, for ParseMachine. top's
// first frame averages since boot: the second covers the last second. A home that
// doesn't exist keeps the user's top settings (~/.toprc: a line per CPU, other
// columns...) out of the format parseTop reads. df leaves out
// pseudo filesystems, container layers and snaps. lscpu runs in the C locale, so
// that its field names are the ones sed looks for.
func DescribeMachine() string {
	return inSections(
		[2]string{"system", `echo "os=$(. /etc/os-release && echo "$PRETTY_NAME")"
echo "kernel=$(uname -r)"
echo "arch=$(uname -m)"
echo "virt=$(systemd-detect-virt 2>/dev/null)"
echo "model=$(LC_ALL=C lscpu 2>/dev/null | sed -n 's/^Model name: *//p')"
echo "cpus=$(nproc)"
echo "uptime=$(cut -d' ' -f1 /proc/uptime)"
echo "load=$(cut -d' ' -f1-3 /proc/loadavg)"`},
		[2]string{"memory", "grep -E '^(MemTotal|MemAvailable|SwapTotal|SwapFree):' /proc/meminfo"},
		[2]string{"disks", "df -P -k -x tmpfs -x devtmpfs -x overlay -x squashfs"},
		[2]string{"top", "HOME=/nonexistent XDG_CONFIG_HOME=/nonexistent LC_ALL=C top -b -n 2 -d 1 -w 512 | awk '/^top -/ {n++} n == 2' | head -n 12"},
	)
}

// ParseMachine reads DescribeMachine's output.
func ParseMachine(out string) (Machine, error) {
	sec := sections(out)

	sys := keyValues(sec["system"])

	cpus, err := strconv.Atoi(sys["cpus"])
	if err != nil {
		return Machine{}, errors.New("no system section")
	}

	m := Machine{OS: sys["os"], Kernel: sys["kernel"], Arch: sys["arch"], Virt: sys["virt"], Model: sys["model"], CPUs: cpus, CPUBusy: -1}

	if up, err := strconv.ParseFloat(sys["uptime"], 64); err == nil {
		m.Uptime = time.Duration(up * float64(time.Second))
	}

	for i, f := range strings.Fields(sys["load"]) {
		if i < len(m.Load) {
			m.Load[i] = number(f)
		}
	}

	for _, line := range strings.Split(sec["memory"], "\n") {
		name, value, _ := strings.Cut(line, ":")
		kb := count(strings.TrimSuffix(strings.TrimSpace(value), " kB"))

		switch name {
		case "MemTotal":
			m.MemTotal = kb * 1024
		case "MemAvailable":
			m.MemAvailable = kb * 1024
		case "SwapTotal":
			m.SwapTotal = kb * 1024
		case "SwapFree":
			m.SwapFree = kb * 1024
		}
	}

	m.Disks = parseDisks(sec["disks"])
	m.CPUBusy, m.Processes = parseTop(sec["top"])

	return m, nil
}

// parseDisks reads df -P -k: filesystem, size, used, available, capacity, then the
// mount point, which may have spaces. Anything else, like the header or df's errors
// about mounts it can't read, is skipped.
func parseDisks(raw string) []Disk {
	var disks []Disk

	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || !strings.HasSuffix(f[4], "%") {
			continue
		}

		size, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}

		disks = append(disks, Disk{Mount: strings.Join(f[5:], " "), Size: size * 1024, Used: count(f[2]) * 1024, Free: count(f[3]) * 1024})
	}

	return disks
}

// parseTop reads top's frame: the CPUs' busy percent, from the idle one on the
// %Cpu(s) line, and the processes under the PID header line. busy is -1 without it.
// That line's values are 5 wide, with no space of their own: a full one touches
// the comma before it ("ni,100.0 id"). It's split on commas, not spaces.
func parseTop(raw string) (busy float64, procs []Process) {
	busy = -1
	header := false

	for _, line := range strings.Split(raw, "\n") {
		f := strings.Fields(line)

		switch {
		case strings.HasPrefix(line, "%Cpu"):
			_, values, _ := strings.Cut(line, ":")

			for _, v := range strings.Split(values, ",") {
				if n, name, _ := strings.Cut(strings.TrimSpace(v), " "); name == "id" {
					busy = 100 - number(n)
				}
			}
		case len(f) > 0 && f[0] == "PID":
			header = true
		case header && len(f) >= 12: // PID USER PR NI VIRT RES SHR S %CPU %MEM TIME+ COMMAND
			procs = append(procs, Process{PID: f[0], User: f[1], CPU: number(f[8]), Mem: number(f[9]), Command: strings.Join(f[11:], " ")})
		}
	}

	return busy, procs
}

// --- unit -------------------------------------------------------------------

// UnitInfo is what DescribeUnit found about a systemd unit.
type UnitInfo struct {
	Unit

	File, Restart, TriggeredBy string
	MainPID, ExitStatus        int // the main process, 0 if none; its last exit status
	Restarts                   int // automatic restarts since the unit was started

	Memory, MemoryMax uint64 // bytes, as docker counts a container's; 0: unknown, or no limit
	CPUTime           time.Duration
	Tasks             uint64

	Journal []string // its last lines
}

// DescribeUnit is the describe command of a systemd unit, for ParseUnit.
func DescribeUnit(unit string) string {
	return inSections(
		[2]string{"show", "systemctl show --timestamp=unix -p " +
			"Id,Description,ActiveState,SubState,Result,UnitFileState,StateChangeTimestamp," +
			"FragmentPath,MainPID,ExecMainStatus,NRestarts,Restart,TriggeredBy," +
			"MemoryCurrent,MemoryMax,CPUUsageNSec,TasksCurrent " + quote(unit)},
		[2]string{"journal", "journalctl -u " + quote(unit) + " -n 10 -o short-iso --no-pager"},
		[2]string{"cache", "cg=$(systemctl show -P ControlGroup " + quote(unit) + `); [ -z "$cg" ] || grep '^inactive_file ' "/sys/fs/cgroup$cg/memory.stat"`},
	)
}

// ParseUnit reads DescribeUnit's output.
func ParseUnit(out string) (UnitInfo, error) {
	sec := sections(out)

	units := parseUnits(sec["show"])
	if len(units) == 0 {
		return UnitInfo{}, errors.New("no unit in systemctl show's output")
	}

	p := keyValues(sec["show"])
	u := UnitInfo{
		Unit: units[0], File: p["FragmentPath"], Restart: p["Restart"], TriggeredBy: p["TriggeredBy"],
		MainPID: int(count(p["MainPID"])), ExitStatus: int(count(p["ExecMainStatus"])), Restarts: int(count(p["NRestarts"])),
		Memory: count(p["MemoryCurrent"]), MemoryMax: count(p["MemoryMax"]),
		CPUTime: time.Duration(count(p["CPUUsageNSec"])), Tasks: count(p["TasksCurrent"]),
	}

	// systemd's MemoryCurrent counts the unit's page cache. Like docker, leave out its
	// inactive part, which the kernel drops first: what's left is what the unit uses.
	if f := strings.Fields(sec["cache"]); len(f) == 2 && f[0] == "inactive_file" {
		u.Memory -= min(count(f[1]), u.Memory)
	}

	for _, line := range strings.Split(strings.TrimSpace(sec["journal"]), "\n") {
		if line != "" {
			u.Journal = append(u.Journal, line)
		}
	}

	return u, nil
}

// --- stacks and containers --------------------------------------------------

// Stats is what docker stats says of a running container.
type Stats struct {
	CPU, Mem                       float64 // percent: CPU of one CPU, as docker counts; memory of its limit
	MemUsage, NetIO, BlockIO, PIDs string  // as docker prints them: "620MiB / 15.2GiB"
}

// StackInfo is what DescribeStack found about a compose stack.
type StackInfo struct {
	CPUs       int
	Containers []StackContainer
}

// StackContainer is one of a stack's containers; Stats is nil unless it runs.
type StackContainer struct {
	Container

	Service, Ports string
	Stats          *Stats
}

// projectFilter selects a compose stack's containers in docker ps.
func projectFilter(stack string) string {
	return "--filter " + quote("label=com.docker.compose.project="+stack)
}

// DescribeStack is the describe command of a compose stack, for ParseStack: its
// containers, then what the running ones use. docker: see StackLogs. Without any
// container to name, docker stats would list every container: they're named.
func DescribeStack(docker, stack string) string {
	return inSections(
		[2]string{"cpus", "nproc"},
		[2]string{"ps", docker + " ps -a " + projectFilter(stack) +
			` --format '{{.Names}}\t{{.Label "com.docker.compose.service"}}\t{{.State}}\t{{.Status}}\t{{.Ports}}'`},
		[2]string{"stats", "ids=$(" + docker + " ps -q " + projectFilter(stack) + `); [ -z "$ids" ] || ` +
			docker + ` stats --no-stream --format '{{.Name}}\t{{.CPUPerc}}\t{{.MemPerc}}\t{{.MemUsage}}' $ids`},
	)
}

// ParseStack reads DescribeStack's output.
func ParseStack(out string) (StackInfo, error) {
	sec := sections(out)

	ps, ok := sec["ps"]
	if !ok {
		return StackInfo{}, errors.New("no ps section")
	}

	stats := map[string]*Stats{}

	for _, line := range strings.Split(sec["stats"], "\n") {
		if f := strings.Split(line, "\t"); len(f) == 4 {
			stats[f[0]] = &Stats{CPU: percent(f[1]), Mem: percent(f[2]), MemUsage: f[3]}
		}
	}

	s := StackInfo{CPUs: int(count(sec["cpus"]))}

	for _, line := range strings.Split(ps, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			continue
		}

		c := Container{Name: f[0], State: f[2], Status: f[3]}
		s.Containers = append(s.Containers, StackContainer{Container: c, Service: f[1], Ports: f[4], Stats: stats[f[0]]})
	}

	if len(s.Containers) == 0 {
		return StackInfo{}, errors.New("no container in the stack")
	}

	return s, nil
}

// ContainerInfo is what DescribeContainer found about a container. The values of
// its environment are left out on purpose: they're often secrets.
type ContainerInfo struct {
	Name, Image, Command    string
	Project, Service        string // its compose stack and service, if any
	State                   string // created, running, exited...
	Running                 bool
	ExitCode                int
	StartedAt               time.Time
	Restarts                int
	RestartPolicy           string
	Health, HealthLast      string // its healthcheck's status, and last output; "" without one
	HealthStreak            int    // failed checks in a row
	Ports, Networks, Mounts []string
	EnvNames                []string
	CPUs                    int
	Stats                   *Stats // nil unless docker stats answered
}

// DescribeContainer is the describe command of a container, for ParseContainer:
// what it uses, then everything docker knows about it. docker: see StackLogs.
func DescribeContainer(docker, container string) string {
	return inSections(
		[2]string{"cpus", "nproc"},
		[2]string{"stats", docker + ` stats --no-stream --format '{{.CPUPerc}}\t{{.MemPerc}}\t{{.MemUsage}}\t{{.NetIO}}\t{{.BlockIO}}\t{{.PIDs}}' ` + quote(container)},
		[2]string{"inspect", docker + " inspect " + quote(container)},
	)
}

// inspected is the part of docker inspect's output that ContainerInfo shows.
type inspected struct {
	Name         string
	RestartCount int
	State        struct {
		Status    string
		Running   bool
		ExitCode  int
		StartedAt time.Time
		Health    *struct {
			Status        string
			FailingStreak int
			Log           []struct{ Output string }
		}
	}
	Config struct {
		Image           string
		Env             []string
		Entrypoint, Cmd []string
		Labels          map[string]string
	}
	HostConfig struct {
		RestartPolicy struct{ Name string }
	}
	NetworkSettings struct {
		Ports    map[string][]struct{ HostIp, HostPort string }
		Networks map[string]struct{ IPAddress string }
	}
	Mounts []struct {
		Name, Source, Destination string
		RW                        bool
	}
}

// ParseContainer reads DescribeContainer's output.
func ParseContainer(out string) (ContainerInfo, error) {
	sec := sections(out)

	var found []inspected
	if err := json.Unmarshal([]byte(sec["inspect"]), &found); err != nil || len(found) == 0 {
		return ContainerInfo{}, errors.New("no container in docker inspect's output")
	}

	in := found[0]
	c := ContainerInfo{
		Name: strings.TrimPrefix(in.Name, "/"), Image: in.Config.Image,
		Command: strings.Join(slices.Concat(in.Config.Entrypoint, in.Config.Cmd), " "),
		Project: in.Config.Labels["com.docker.compose.project"], Service: in.Config.Labels["com.docker.compose.service"],
		State: in.State.Status, Running: in.State.Running, ExitCode: in.State.ExitCode, StartedAt: in.State.StartedAt,
		Restarts: in.RestartCount, RestartPolicy: in.HostConfig.RestartPolicy.Name,
		CPUs: int(count(sec["cpus"])),
	}

	if h := in.State.Health; h != nil {
		c.Health, c.HealthStreak = h.Status, h.FailingStreak
		if len(h.Log) > 0 {
			c.HealthLast = oneLine(h.Log[len(h.Log)-1].Output)
		}
	}

	for port, bindings := range in.NetworkSettings.Ports {
		if len(bindings) == 0 {
			c.Ports = append(c.Ports, port+" (not published)")
		}

		for _, b := range bindings {
			c.Ports = append(c.Ports, net.JoinHostPort(b.HostIp, b.HostPort)+" → "+port) // [::]:80 for IPv6
		}
	}

	for name, n := range in.NetworkSettings.Networks {
		c.Networks = append(c.Networks, strings.TrimSpace(name+" "+n.IPAddress))
	}

	for _, m := range in.Mounts {
		mode := "ro"
		if m.RW {
			mode = "rw"
		}

		c.Mounts = append(c.Mounts, fmt.Sprintf("%s → %s (%s)", cmp.Or(m.Name, m.Source), m.Destination, mode))
	}

	for _, e := range in.Config.Env {
		name, _, _ := strings.Cut(e, "=") // the value stays behind
		c.EnvNames = append(c.EnvNames, name)
	}

	slices.Sort(c.Ports)
	slices.Sort(c.Networks)

	if f := strings.Split(strings.TrimSpace(sec["stats"]), "\t"); len(f) == 6 {
		c.Stats = &Stats{CPU: percent(f[0]), Mem: percent(f[1]), MemUsage: f[2], NetIO: f[3], BlockIO: f[4], PIDs: f[5]}
	}

	return c, nil
}

// --- numbers ----------------------------------------------------------------

// count reads a whole number; anything else, like systemd's "[not set]" or
// "infinity", or its 2⁶⁴−1 for "unknown", is 0.
func count(s string) uint64 {
	n, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil || n == 1<<64-1 {
		return 0
	}

	return n
}

// number reads a decimal number; anything else is 0.
func number(s string) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}

	return n
}

// percent reads docker's "3.10%"; "--" (not running) is 0.
func percent(s string) float64 { return number(strings.TrimSuffix(strings.TrimSpace(s), "%")) }
