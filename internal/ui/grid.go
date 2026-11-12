package ui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// tone colors a row, or one cell when it alone needs attention (like k9s).
// Terminal palette colors, so they follow the user's theme.
type tone int

const (
	inherit tone = iota // plain for a row; the row's tone for a cell
	good
	warn
	bad // the worst: stackRow ranks tones with max
	dim
)

var toneStyles = map[tone]lipgloss.Style{
	good: lipgloss.NewStyle().Foreground(lipgloss.Color("2")),
	warn: lipgloss.NewStyle().Foreground(lipgloss.Color("3")),
	bad:  lipgloss.NewStyle().Foreground(lipgloss.Color("1")),
	dim:  dimStyle,
}

type cell struct {
	text string
	tone tone
	dot  bool // a status glyph in front, colored by tone
}

func (c cell) shown() string {
	if c.dot {
		return "● " + c.text
	}
	return c.text
}

type row struct {
	cells []cell
	tone  tone
	key   string // what the row is about: a machine, unit or stack name
}

func dot(t tone, text string) cell { return cell{text, t, true} }

// grid is a scrolling table with one selected row. Unlike bubbles/table,
// it colors cells without miscounting their width.
type grid struct {
	titles         []string
	widths         []int
	rows           []row
	cursor, offset int
	height         int // lines, header included
}

func newGrid(titles ...string) grid {
	g := grid{titles: titles, widths: make([]int, len(titles)), height: 3}
	for i, t := range titles {
		g.widths[i] = len(t)
	}
	return g
}

func (g *grid) SetRows(rows []row) { g.rows = rows; g.clamp() }
func (g *grid) SetCursor(i int)    { g.cursor = i; g.clamp() }

// visible is the number of rows shown under the header and its rule.
func (g grid) visible() int { return max(g.height-2, 1) }

func (g *grid) clamp() {
	g.cursor = min(max(g.cursor, 0), max(len(g.rows)-1, 0))
	g.offset = min(max(g.offset, g.cursor-g.visible()+1), g.cursor)
}

func (g *grid) Update(k tea.KeyMsg) {
	page := g.visible()
	switch k.String() {
	case "up", "k":
		g.cursor--
	case "down", "j":
		g.cursor++
	case "pgup", "b":
		g.cursor -= page
	case "pgdown", "f", " ":
		g.cursor += page
	case "ctrl+u", "u":
		g.cursor -= page / 2
	case "ctrl+d", "d":
		g.cursor += page / 2
	case "home", "g":
		g.cursor = 0
	case "end", "G":
		g.cursor = len(g.rows) - 1
	}
	g.clamp()
}

func (g grid) View() string {
	var head string
	for i, t := range g.titles {
		head += pad(t, g.widths[i])
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render(head) + "\n")
	b.WriteString(dimStyle.Render(strings.Repeat("─", runewidth.StringWidth(head))))
	for i := g.offset; i < g.offset+g.visible(); i++ {
		b.WriteString("\n")
		if i < len(g.rows) {
			b.WriteString(g.renderRow(i))
		}
	}
	return b.String()
}

func (g grid) renderRow(i int) string {
	r := g.rows[i]
	var out strings.Builder
	for j, w := range g.widths {
		c := cell{}
		if j < len(r.cells) {
			c = r.cells[j]
		}
		t := c.tone
		if t == inherit {
			t = r.tone
		}
		st := toneStyles[t] // inherit: the zero style, plain
		if i == g.cursor {
			st = toneStyles[r.tone].Reverse(true) // one bar, in the row's color
		}
		out.WriteString(st.Render(pad(c.shown(), w)))
	}
	return out.String()
}

// pad fits s in a column w wide, with a space either side.
func pad(s string, w int) string {
	return " " + runewidth.FillRight(runewidth.Truncate(s, w, "…"), w) + " "
}

// --- rows -------------------------------------------------------------------

// machineTitles head the machines table: `sshwarm status` prints all but PROBLEMS.
var machineTitles = []string{"MACHINE", "STATE", "REV", "DEPLOYED", "GEN", "UPTIME", "REBOOT", "FAILED", "STACKS", "PROBLEMS"}

// machineRow is a machine's row, for both the TUI and `sshwarm status`. Only the
// fields needing attention are colored, unless the machine is down.
func machineRow(r swarm.Report) row {
	if r.Err != nil {
		cells := []cell{{text: r.Host.Name}, dot(bad, "down")}
		for range len(machineTitles) - 3 { // all but MACHINE, STATE and PROBLEMS
			cells = append(cells, cell{text: "-"})
		}
		return row{tone: bad, key: r.Host.Name, cells: append(cells, cell{text: r.Err.Error()})}
	}
	problems := r.Problems()
	state := dot(good, "ok")
	if len(problems) > 0 {
		state = dot(warn, "degraded")
	}
	rev := cell{text: shortRev(r.Revision)}
	switch {
	case r.Revision == "":
		rev.tone = dim
	case strings.HasSuffix(r.Revision, "-dirty"):
		rev.tone = warn
	}
	deployed := cell{text: "-"}
	if !r.Deployed.IsZero() {
		deployed.text = short(r.Age(r.Deployed))
	}
	reboot := cell{text: "no"}
	if r.RebootPending {
		reboot = cell{text: "yes", tone: warn}
	}
	nFailed := len(r.Failed())
	failed := cell{text: strconv.Itoa(nFailed)}
	if nFailed > 0 {
		failed.tone = bad
	}
	stacks := cell{text: "-"}
	if r.HasDocker {
		up := r.StacksUp()
		stacks.text = fmt.Sprintf("%d/%d", up, len(r.Stacks))
		if up < len(r.Stacks) {
			stacks.tone = bad
		}
	}
	return row{key: r.Host.Name, cells: []cell{{text: r.Host.Name}, state, rev, deployed, {text: strconv.Itoa(r.Generation)},
		{text: short(r.Uptime)}, reboot, failed, stacks, {text: strings.Join(problems, "; "), tone: bad}}}
}

func unitRow(r swarm.Report, u swarm.Unit) row {
	age := "-"
	if !u.Since.IsZero() {
		age = short(r.Age(u.Since))
	}
	result := cell{text: u.Result}
	if u.Result != "" && u.Result != "success" {
		result.tone = bad
	}
	cells := func(state tone) []cell {
		return []cell{dot(state, u.Name), {text: u.Active, tone: state}, {text: u.Sub}, result, {text: age}, {text: u.Description}}
	}
	switch u.Active {
	case "failed":
		return row{tone: bad, cells: cells(bad), key: u.Name}
	case "activating", "deactivating", "reloading", "refreshing":
		return row{cells: cells(warn), key: u.Name}
	case "active":
		return row{cells: cells(good), key: u.Name}
	}
	return row{tone: dim, cells: cells(dim), key: u.Name} // inactive: there, but nothing to see
}

// containerTone rates c: restarting is a crash loop, so bad; starting is fine for now.
func containerTone(c swarm.Container) tone {
	switch {
	case !c.OK():
		return bad
	case strings.Contains(c.Status, "health: starting"), c.State == "created", c.State == "paused":
		return warn
	}
	return good
}

func stackRow(s swarm.Stack) row {
	worst := good
	var problems []string
	for _, c := range s.Containers {
		t := containerTone(c)
		worst = max(worst, t)
		if t != good {
			problems = append(problems, c.Name+" "+c.Status)
		}
	}
	ok := fmt.Sprintf("%d/%d", len(s.Containers)-len(s.Bad()), len(s.Containers))
	r := row{key: s.Name, cells: []cell{dot(worst, s.Name), {text: ok, tone: worst}, {text: strings.Join(problems, "; ")}}}
	if worst == bad {
		r.tone = bad
	}
	return r
}

// short formats a duration as its largest unit: 45s, 12m, 5h, 3d.
func short(d time.Duration) string {
	switch {
	case d < time.Minute:
		return strconv.Itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return strconv.Itoa(int(d.Minutes())) + "m"
	case d < 48*time.Hour:
		return strconv.Itoa(int(d.Hours())) + "h"
	}
	return strconv.Itoa(int(d.Hours()/24)) + "d"
}

// shortRev keeps 7 characters of a commit, and its -dirty suffix.
func shortRev(rev string) string {
	if rev == "" {
		return "-"
	}
	rev, dirty := strings.CutSuffix(rev, "-dirty")
	rev = rev[:min(len(rev), 7)]
	if dirty {
		rev += "-dirty"
	}
	return rev
}
