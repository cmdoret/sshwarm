package ui

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// A card is a description made readable, like k9s's: headed sections of aligned
// fields, tables, and bars for what's used. Each kind's card parses its describe
// command's output with swarm, and draws it whole: lines wider than the window
// scroll left and right. On an error, the text screen shows the output as it came
// instead.

const (
	// keyWidth is the column fields' values start at: two spaces past "Triggered
	// by", the longest key.
	keyWidth = 14

	// barWidth is a field's bar, in cells: 5% each. cellBar is a table cell's, 10%.
	barWidth, cellBar = 20, 10
)

// cardFunc draws a describe command's output as a card.
type cardFunc func(out string) (string, error)

type card struct{ b strings.Builder }

func (c *card) line(s string) { c.b.WriteString(s + "\n") }

// section starts a headed block, after a blank line unless it's the first.
func (c *card) section(name string) {
	if c.b.Len() > 0 {
		c.b.WriteString("\n")
	}

	c.line(keyStyle.Render(name))
}

// field adds a key and its value, unless there's no value to show.
func (c *card) field(key, value string) {
	if value == "" {
		return
	}

	c.line("  " + titleStyle.Render(fmt.Sprintf("%-*s", keyWidth, key)) + value)
}

// usage adds a field showing f, a fraction of a whole, as a bar, then text.
func (c *card) usage(key string, f float64, text string) {
	filled, empty := bar(f, barWidth)
	c.field(key, toneStyles[usageTone(f)].Render(filled)+dimStyle.Render(empty)+"  "+text)
}

// list adds a section with an item per line, unless there are none.
func (c *card) list(name string, items []string) {
	if len(items) == 0 {
		return
	}

	c.section(name)

	for _, item := range items {
		c.line("  " + item)
	}
}

// table adds a table, as the TUI's tables look but without a cursor or a selected
// column, as wide as its cells.
func (c *card) table(titles []string, rows []row) {
	g := grid{titles: titles, rows: rows, height: len(rows) + 1, cursor: -1, col: -1}
	_, g.width = g.natural()

	for _, l := range strings.Split(g.view(), "\n") {
		c.line(l)
	}
}

func (c *card) String() string { return strings.TrimSuffix(c.b.String(), "\n") }

// bar draws f, a fraction of a whole, n cells wide: its filled cells, then the empty
// ones, in light shade.
func bar(f float64, n int) (filled, empty string) {
	full := int(math.Round(min(max(f, 0), 1) * float64(n)))

	return strings.Repeat("█", full), strings.Repeat("░", n-full)
}

// barCell is a table's cell showing f as a bar, then text: in one tone, as cells are.
func barCell(f float64, text string) cell {
	filled, empty := bar(f, cellBar)

	return cell{text: filled + empty + " " + text, tone: usageTone(f)}
}

// usageTone rates how much of something is used: fine below 70%, a warning below
// 90%, then bad. From 90%, a disk is also one of a machine's problems (swarm.FullDisk).
func usageTone(f float64) tone {
	switch {
	case f >= 0.9:
		return bad
	case f >= 0.7:
		return warn
	}

	return good
}

// fraction is part of whole, 0 for nothing of nothing.
func fraction(part, whole uint64) float64 {
	if whole == 0 {
		return 0
	}

	return float64(part) / float64(whole)
}

// size formats bytes in IEC units: 512 B, 4.9 GiB.
func size(b uint64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	}

	f, unit := float64(b)/1024, 0
	for ; f >= 1024 && unit < 5; unit++ {
		f /= 1024
	}

	return fmt.Sprintf("%.1f %ciB", f, "KMGTPE"[unit])
}

// joined joins the parts that aren't empty with dots, as titles do.
func joined(parts ...string) string {
	return strings.Join(slices.DeleteFunc(parts, func(p string) bool { return p == "" }), " · ")
}

// --- the cards --------------------------------------------------------------

// machineCard: what the machine is, how much of it is used, its disks and busiest
// processes.
func machineCard(out string) (string, error) {
	m, err := swarm.ParseMachine(out)
	if err != nil {
		return "", err
	}

	var c card

	virt := m.Virt
	if virt == "none" {
		virt = "bare metal"
	}

	c.section("System")
	c.field("OS", m.OS)
	c.field("Kernel", joined(m.Kernel, m.Arch, virt))
	c.field("CPU", joined(m.Model, fmt.Sprintf("%d CPUs", m.CPUs)))
	c.field("Up", short(m.Uptime))

	c.section("Usage")

	if m.CPUBusy >= 0 {
		c.usage("CPU", m.CPUBusy/100, fmt.Sprintf("%.0f%%", m.CPUBusy))
	}

	c.usage("Load", m.Load[0]/float64(max(m.CPUs, 1)), fmt.Sprintf("%.2f %.2f %.2f on %d CPUs", m.Load[0], m.Load[1], m.Load[2], m.CPUs))

	used := m.MemTotal - m.MemAvailable
	c.usage("Memory", fraction(used, m.MemTotal), size(used)+" / "+size(m.MemTotal))

	if m.SwapTotal > 0 {
		swapped := m.SwapTotal - m.SwapFree
		c.usage("Swap", fraction(swapped, m.SwapTotal), size(swapped)+" / "+size(m.SwapTotal))
	}

	if len(m.Disks) > 0 {
		var rows []row

		for _, d := range m.Disks {
			f := fraction(d.Used, d.Used+d.Free) // as df counts: the root's reserve isn't free
			rows = append(rows, row{cells: []cell{{text: d.Mount}, {text: size(d.Size)}, {text: size(d.Used)}, barCell(f, fmt.Sprintf("%.0f%%", f*100))}})
		}

		c.section("Disks")
		c.table([]string{"MOUNT", "SIZE", "USED", "USE"}, rows)
	}

	if len(m.Processes) > 0 {
		var rows []row

		for _, p := range m.Processes {
			rows = append(rows, row{cells: []cell{{text: p.PID}, {text: p.User}, {text: fmt.Sprintf("%.1f%%", p.CPU)}, {text: fmt.Sprintf("%.1f%%", p.Mem)}, {text: p.Command}}})
		}

		c.section("Processes, the busiest over a second")
		c.table([]string{"PID", "USER", "CPU", "MEM", "COMMAND"}, rows)
	}

	return c.String(), nil
}

// unitCard: the unit's state, its process, what it uses, and its last log lines.
func unitCard(out string) (string, error) {
	u, err := swarm.ParseUnit(out)
	if err != nil {
		return "", err
	}

	var c card

	state := toneStyles[unitTone(u.Active)].Render("● " + u.Active + " (" + u.Sub + ")")

	since := ""
	if !u.Since.IsZero() {
		since = "since " + short(time.Since(u.Since)) + " ago"
	}

	result := u.Result
	if result != "" && result != "success" {
		result = toneStyles[bad].Render(result)
	}

	process := ""
	if u.MainPID > 0 {
		process = fmt.Sprintf("PID %d", u.MainPID)
	}

	if u.ExitStatus != 0 {
		process = joined(process, toneStyles[bad].Render(fmt.Sprintf("last exit status %d", u.ExitStatus)))
	}

	restarts := ""
	if u.Restarts > 0 {
		restarts = fmt.Sprintf("%d restarts", u.Restarts)
	}

	c.section("Unit")
	c.field("Name", u.Name)
	c.field("Description", u.Description)
	c.field("State", joined(state, result, since))
	c.field("Enabled", joined(u.UnitFileState, u.File))
	c.field("Restart", joined(u.Restart, restarts))
	c.field("Process", process)
	c.field("Triggered by", u.TriggeredBy)

	if u.Memory > 0 || u.CPUTime > 0 || u.Tasks > 0 {
		c.section("Usage")

		if u.MemoryMax > 0 {
			c.usage("Memory", fraction(u.Memory, u.MemoryMax), size(u.Memory)+" / "+size(u.MemoryMax))
		} else if u.Memory > 0 {
			c.field("Memory", size(u.Memory)+dimStyle.Render(" (no limit)"))
		}

		if u.CPUTime > 0 {
			c.field("CPU time", u.CPUTime.Round(time.Millisecond).String())
		}

		if u.Tasks > 0 {
			c.field("Tasks", fmt.Sprint(u.Tasks))
		}
	}

	c.list("Recent logs", u.Journal)

	return c.String(), nil
}

// stackCard: the stack's containers, and what each uses.
func stackCard(out string) (string, error) {
	s, err := swarm.ParseStack(out)
	if err != nil {
		return "", err
	}

	var c card

	ok := 0

	var rows []row

	for _, sc := range s.Containers {
		t := containerTone(sc.Container)
		if sc.OK() {
			ok++
		}

		cpu, mem := cell{text: "-"}, cell{text: "-"}
		if sc.Stats != nil {
			cpu = barCell(sc.Stats.CPU/float64(100*max(s.CPUs, 1)), fmt.Sprintf("%.1f%%", sc.Stats.CPU))
			mem = barCell(sc.Stats.Mem/100, strings.TrimSpace(strings.Split(sc.Stats.MemUsage, "/")[0]))
		}

		rows = append(rows, row{cells: []cell{dot(t, sc.Name), {text: sc.Service}, {text: sc.State, tone: t}, cpu, mem, {text: sc.Ports}}})
	}

	c.section(fmt.Sprintf("Containers, %d of %d fine", ok, len(s.Containers)))
	c.table([]string{"CONTAINER", "SERVICE", "STATE", "CPU", "MEMORY", "PORTS"}, rows)

	return c.String(), nil
}

// containerCard: the container's state, what it uses, its health, ports, networks,
// mounts, and the names of its environment's variables (not their values).
func containerCard(out string) (string, error) {
	ci, err := swarm.ParseContainer(out)
	if err != nil {
		return "", err
	}

	var c card

	t := good

	switch {
	case ci.Health == "unhealthy", ci.State == "restarting", ci.State == "dead", ci.State == "exited" && ci.ExitCode != 0:
		t = bad
	case ci.Health == "starting", ci.State == "created", ci.State == "paused":
		t = warn
	}

	state := toneStyles[t].Render("● " + ci.State)
	if !ci.Running && ci.ExitCode != 0 {
		state += toneStyles[bad].Render(fmt.Sprintf(" (exit %d)", ci.ExitCode))
	}

	health := ""
	if ci.Health != "" {
		health = "health " + ci.Health
	}

	started := ""
	if !ci.StartedAt.IsZero() {
		started = ci.StartedAt.Local().Format("2006-01-02 15:04") + " · " + short(time.Since(ci.StartedAt)) + " ago"
	}

	stack := ""
	if ci.Project != "" {
		stack = joined(ci.Project, "service "+ci.Service)
	}

	c.section("Container")
	c.field("Name", ci.Name)
	c.field("Image", ci.Image)
	c.field("Stack", stack)
	c.field("State", joined(state, health))
	c.field("Started", started)
	c.field("Restarts", joined(fmt.Sprint(ci.Restarts), "policy "+cmp.Or(ci.RestartPolicy, "no")))
	c.field("Command", ci.Command)

	if ci.Running && ci.Stats != nil {
		cpus := max(ci.CPUs, 1)

		c.section("Usage")
		c.usage("CPU", ci.Stats.CPU/float64(100*cpus), fmt.Sprintf("%.1f%% of a CPU, %d here", ci.Stats.CPU, cpus))
		c.usage("Memory", ci.Stats.Mem/100, ci.Stats.MemUsage)
		c.field("Net I/O", ci.Stats.NetIO)
		c.field("Disk I/O", ci.Stats.BlockIO)
		c.field("PIDs", ci.Stats.PIDs)
	}

	if ci.Health != "" {
		failing := ""
		if ci.HealthStreak > 0 {
			failing = toneStyles[bad].Render(fmt.Sprintf("%d failed checks in a row", ci.HealthStreak))
		}

		c.section("Health")
		c.field("Status", joined(ci.Health, failing))
		c.field("Last check", ci.HealthLast)
	}

	c.list("Ports", ci.Ports)
	c.list("Networks", ci.Networks)
	c.list("Mounts", ci.Mounts)
	c.list("Environment "+dimStyle.Render("(values hidden)"), ci.EnvNames)

	return c.String(), nil
}
