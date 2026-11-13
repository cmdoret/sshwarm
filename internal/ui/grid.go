package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
// cells without miscounting their width.
type grid struct {
	titles         []string
	rows           []row
	cursor, offset int
	width, height  int // the space it has, header included
}

func (g *grid) setRows(rows []row) {
	g.rows = rows
	g.clamp()
}

// visible is the number of rows shown under the header.
func (g grid) visible() int { return max(g.height-1, 1) }

// clamp keeps the cursor on a row, and the view on the cursor.
func (g *grid) clamp() {
	g.cursor = min(max(g.cursor, 0), max(len(g.rows)-1, 0))
	g.offset = min(max(g.offset, g.cursor-g.visible()+1), g.cursor)
}

// key moves the cursor.
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

// widths fits each column to its widest cell, the last taking the rest. If they
// don't all fit, a column gets up to a third of the grid, so that one long name
// can't push the others out.
func (g grid) widths() []int {
	ws, total := g.natural()
	last := len(ws) - 1

	if total <= g.width {
		ws[last] += g.width - total

		return ws
	}

	rest := g.width - 2*len(ws) // every column's padding

	for i := range ws[:last] {
		ws[i] = min(ws[i], max(g.width/3, 10))
		rest -= ws[i]
	}

	ws[last] = max(rest, 10)

	return ws
}

func (g grid) view() string {
	ws := g.widths()

	var head string
	for i, t := range g.titles {
		head += pad(t, ws[i])
	}

	var b strings.Builder

	b.WriteString(titleStyle.Render(head))

	for i := g.offset; i < g.offset+g.visible(); i++ {
		b.WriteString("\n")

		if i < len(g.rows) {
			b.WriteString(g.renderRow(i, ws))
		}
	}

	return b.String()
}

// renderRow draws row i, a cell per column: in the cell's tone, or else the row's.
// The selected row is a bold bar on a gray background, marked in the accent color
// where its first cell's padding would be.
func (g grid) renderRow(i int, widths []int) string {
	r := g.rows[i]

	var out strings.Builder

	for j, c := range r.cells {
		t := c.tone
		if t == inherit {
			t = r.tone
		}

		st, text := toneStyles[t], pad(c.shown(), widths[j]) // inherit: the zero style, plain
		if i == g.cursor {
			st = st.Bold(true).Background(selection)
		}

		if i == g.cursor && j == 0 {
			out.WriteString(keyStyle.Background(selection).Render("▌"))
			text = text[1:] // the marker takes the padding's space
		}

		out.WriteString(st.Render(text))
	}

	return out.String()
}

// pad fits s in a column w wide, with a space either side.
func pad(s string, w int) string {
	return " " + runewidth.FillRight(runewidth.Truncate(s, w, "…"), w) + " "
}
