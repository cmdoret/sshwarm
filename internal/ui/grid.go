package ui

import (
	"cmp"
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

// Tones rank by severity: stackRow keeps the worst with max, and a status sorts on its
// tone, worst first.
const (
	inherit tone = iota // plain for a row; the row's tone for a cell
	dim
	good
	warn
	bad
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
	dot  bool    // a status glyph in front, colored by tone
	rank float64 // what the cell sorts by, before its text: a duration, a percentage, a severity
}

func (c cell) shown() string {
	if c.dot {
		return "● " + c.text
	}

	return c.text
}

func dot(t tone, text string) cell { return cell{text: text, tone: t, dot: true} }

// row is one line of a grid: a cell per column.
type row struct {
	cells []cell
	tone  tone
	key   string // what the row is about: a machine, unit, stack or container name
	owner string // what that belongs to: a container's stack
}

// grid is a scrolling table with one selected row, and one selected column to sort
// on. Unlike bubbles/table, it colors cells without miscounting their width. Columns
// too wide for it scroll sideways, under the first one, which names the row.
type grid struct {
	titles         []string
	rows           []row
	cursor, offset int
	col            int // the selected column; -1 for none, as in cards
	scroll         int // how many columns after the first are scrolled out of view
	width, height  int // the space it has, header included

	sorted  bool // on sortCol, descending or not; else the rows keep their kind's order
	sortCol int
	desc    bool
}

// setRows shows rows, sorted as the grid is, with the cursor on the row it was on if
// that's still there: a refresh or a sort moves rows around.
func (g *grid) setRows(rows []row) {
	key := ""
	if g.cursor < len(g.rows) {
		key = g.rows[g.cursor].key
	}

	g.rows = rows
	g.sort()
	g.choose(key)
}

// choose puts the cursor on the row about key, if there's one.
func (g *grid) choose(key string) {
	if i := slices.IndexFunc(g.rows, func(r row) bool { return r.key == key }); i >= 0 {
		g.cursor = i
	}

	g.clamp()
}

// sortOn selects column col and sorts the rows on it: numbers biggest first, text
// from A, or the other way round if they already are sorted on it.
func (g *grid) sortOn(col int) {
	if g.sorted && g.sortCol == col {
		g.desc = !g.desc
	} else {
		g.sorted, g.sortCol = true, col
		g.desc = slices.ContainsFunc(g.rows, func(r row) bool { return r.cells[col].rank != 0 })
	}

	g.col = col
	g.reveal()
	g.setRows(g.rows)
}

// sort orders the rows on the sorted column, by rank then text, ignoring case as the
// filter does. Ties keep their order.
func (g *grid) sort() {
	if !g.sorted {
		return
	}

	slices.SortStableFunc(g.rows, func(a, b row) int {
		x, y := a.cells[g.sortCol], b.cells[g.sortCol]

		c := cmp.Or(cmp.Compare(x.rank, y.rank), cmp.Compare(strings.ToLower(x.text), strings.ToLower(y.text)))
		if g.desc {
			return -c
		}

		return c
	})
}

// visible is the number of rows shown under the header.
func (g grid) visible() int { return max(g.height-1, 1) }

// clamp keeps the cursor on a row, the view on the cursor, and the columns from
// scrolling past their end.
func (g *grid) clamp() {
	g.cursor = min(max(g.cursor, 0), max(len(g.rows)-1, 0))
	g.offset = min(max(g.offset, g.cursor-g.visible()+1), g.cursor)
	g.scroll = min(max(g.scroll, 0), g.hidden())
}

// hidden is how many columns after the first can scroll out of view: until the
// others fit, or only the last one is left.
func (g grid) hidden() int {
	ws := g.widths()

	n := 0
	for n < len(ws)-2 && span(ws[:1])+span(ws[1+n:]) > g.width {
		n++
	}

	return n
}

// reveal scrolls the columns so that the selected one is in view.
func (g *grid) reveal() {
	if g.col < 1 {
		return // the first column is always in view
	}

	ws := g.widths()

	// From the column right after the first, at most: it then comes first.
	g.scroll = min(g.scroll, g.col-1)
	for g.scroll < g.col-1 && span(ws[:1])+span(ws[1+g.scroll:g.col+1]) > g.width {
		g.scroll++
	}
}

// span is how wide columns are, with their padding.
func span(ws []int) int {
	n := 0
	for _, w := range ws {
		n += w + 2
	}

	return n
}

// key moves the cursor, scrolls the columns, or selects one and sorts on it.
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
	case "left":
		g.scroll--
	case "right":
		g.scroll++
	case "shift+left":
		g.col = max(g.col-1, 0)
		g.reveal()
	case "shift+right":
		g.col = min(g.col+1, len(g.titles)-1)
		g.reveal()
	case "O":
		g.sortOn(g.col)
	}

	g.clamp()
}

// natural is the width of each column's widest cell, and the grid's with them all
// and their padding.
func (g grid) natural() (ws []int, total int) {
	ws = make([]int, len(g.titles))
	for i := range g.titles {
		ws[i] = runewidth.StringWidth(g.title(i))
	}

	for _, r := range g.rows {
		for i, c := range r.cells {
			ws[i] = max(ws[i], runewidth.StringWidth(c.shown()))
		}
	}

	return ws, span(ws)
}

// title is column i's title, with an arrow if the rows are sorted on it.
func (g grid) title(i int) string {
	switch {
	case !g.sorted || i != g.sortCol:
		return g.titles[i]
	case g.desc:
		return g.titles[i] + " ↓"
	}

	return g.titles[i] + " ↑"
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

	for i := range g.titles {
		st := titleStyle
		if i == g.col {
			st = keyStyle // the accent, as the cursor's marker
		}

		head = append(head, st.Render(pad(g.title(i), ws[i])))
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

// scrolled joins a line's rendered cells: the first, then those not scrolled out of
// view, cut at the grid's edge with "…" where more follows.
func (g grid) scrolled(cells []string) string {
	rest := strings.Join(cells[1+g.scroll:], "")

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
