package ui

import (
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"
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

// selection is the cursor row's background: a gray a step off the terminal's own,
// under which every tone stays readable.
var selection = lipgloss.AdaptiveColor{Light: "254", Dark: "237"}

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

func dot(t tone, text string) cell { return cell{text, t, true} }

// row is one line of a grid: a cell per column.
type row struct {
	cells []cell
	tone  tone
	key   string // what the row is about: a machine, unit, stack or container name
}

// grid is a scrolling table with one selected row. Unlike bubbles/table, it colors
// cells without miscounting their width. Columns too wide for it scroll sideways,
// under the first one, which names the row.
type grid struct {
	titles         []string
	rows           []row
	cursor, offset int
	left           int // how far the columns after the first are scrolled
	width, height  int // the space it has, header included
}

func (g *grid) setRows(rows []row) {
	g.rows = rows
	g.clamp()
}

// visible is the number of rows shown under the header.
func (g grid) visible() int { return max(g.height-1, 1) }

// clamp keeps the cursor on a row, the view on the cursor, and the columns from
// scrolling past their end.
func (g *grid) clamp() {
	g.cursor = min(max(g.cursor, 0), max(len(g.rows)-1, 0))
	g.offset = min(max(g.offset, g.cursor-g.visible()+1), g.cursor)
	g.left = min(max(g.left, 0), g.overflow())
}

// overflow is how much wider than the grid its columns are: how far they scroll.
func (g grid) overflow() int {
	total := 0
	for _, w := range g.widths() {
		total += w + 2 // and its padding
	}

	return max(total-g.width, 0)
}

// stops are where the columns after the first scroll to: each one's start, so that
// none shows cut on the left, then half the grid at a time through the last one,
// which may be long, up to the end.
func (g grid) stops() []int {
	ws, end := g.widths(), g.overflow()
	stops := []int{0}

	x := 0
	for _, w := range ws[1 : len(ws)-1] {
		x += w + 2
		stops = append(stops, min(x, end))
	}

	for x += max(g.width/2, 1); x < end; x += max(g.width/2, 1) {
		stops = append(stops, x)
	}

	return append(stops, end)
}

// scroll moves the columns to the next stop, or the previous one.
func (g *grid) scroll(next bool) {
	stops := g.stops()

	i, _ := slices.BinarySearch(stops, g.left) // the first stop at or past left
	if next {
		i, _ = slices.BinarySearch(stops, g.left+1)
	} else {
		i--
	}

	if i >= 0 && i < len(stops) {
		g.left = stops[i]
	}
}

// key moves the cursor, or scrolls the columns.
func (g *grid) key(k tea.KeyMsg) {
	page := g.visible()

	switch k.String() {
	case "up", "k":
		g.cursor--
	case "down", "j":
		g.cursor++
	case "pgup":
		g.cursor -= page
	case "pgdown":
		g.cursor += page
	case "ctrl+u":
		g.cursor -= page / 2
	case "ctrl+d":
		g.cursor += page / 2
	case "home", "g":
		g.cursor = 0
	case "end", "G":
		g.cursor = len(g.rows) - 1
	case "left", "right":
		g.scroll(k.String() == "right")
	}

	g.clamp()
}

// natural is the width of each column's widest cell, and the grid's with them all
// and their padding.
func (g grid) natural() (ws []int, total int) {
	ws = make([]int, len(g.titles))
	for i, t := range g.titles {
		ws[i] = runewidth.StringWidth(t)
	}

	for _, r := range g.rows {
		for i, c := range r.cells {
			ws[i] = max(ws[i], runewidth.StringWidth(c.shown()))
		}
	}

	total = 2 * len(ws) // every column's padding
	for _, w := range ws {
		total += w
	}

	return ws, total
}

// widths fits each column to its widest cell, the last taking any room left. If
// they don't all fit, a column but the last gets up to a third of the grid, so that
// one long name can't push the others out; the rest then scrolls.
func (g grid) widths() []int {
	ws, total := g.natural()
	last := len(ws) - 1

	if total > g.width {
		for i := range ws[:last] {
			total -= ws[i]
			ws[i] = min(ws[i], max(g.width/3, 10))
			total += ws[i]
		}
	}

	ws[last] += max(g.width-total, 0)

	return ws
}

func (g grid) view() string {
	ws := g.widths()

	var head []string
	for i, t := range g.titles {
		head = append(head, titleStyle.Render(pad(t, ws[i])))
	}

	var b strings.Builder

	b.WriteString(g.scrolled(head))

	for i := g.offset; i < g.offset+g.visible(); i++ {
		b.WriteString("\n")

		if i < len(g.rows) {
			b.WriteString(g.scrolled(g.renderRow(i, ws)))
		}
	}

	return b.String()
}

// scrolled joins a line's rendered cells: the first in place, the others scrolled
// under it, and cut at the grid's edge with "…" where more follows.
func (g grid) scrolled(cells []string) string {
	rest := ansi.TruncateLeft(strings.Join(cells[1:], ""), g.left, "")

	return cells[0] + ansi.Truncate(rest, max(g.width-ansi.StringWidth(cells[0]), 0), "…")
}

// renderRow draws row i's cells: in the cell's tone, or else the row's. The
// selected row is a bold bar on a gray background, marked in the accent color where
// its first cell's padding would be.
func (g grid) renderRow(i int, widths []int) []string {
	r := g.rows[i]

	var out []string

	for j, c := range r.cells {
		t := c.tone
		if t == inherit {
			t = r.tone
		}

		st, text := toneStyles[t], pad(c.shown(), widths[j]) // inherit: the zero style, plain
		if i == g.cursor {
			st = st.Bold(true).Background(selection)
		}

		marker := ""
		if i == g.cursor && j == 0 {
			marker = keyStyle.Background(selection).Render("▌")
			text = text[1:] // the marker takes the padding's space
		}

		out = append(out, marker+st.Render(text))
	}

	return out
}

// pad fits s in a column w wide, with a space either side.
func pad(s string, w int) string {
	return " " + runewidth.FillRight(runewidth.Truncate(s, w, "…"), w) + " "
}
