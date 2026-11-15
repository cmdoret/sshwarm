package ui

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// finder is a search in the lines an output screen shows, typed after /: the lines
// it matches, ignoring case, and the match shown.
type finder struct {
	input   textinput.Model
	pattern *regexp.Regexp // nil without a search
	hits    []int          // the lines it matches, in order
	match   int            // the line of the match shown, -1 if none
}

func newFinder() finder {
	in := textinput.New()
	in.Prompt = "/"

	return finder{input: in, match: -1}
}

// key types the search: enter keeps it, esc drops it. It says whether what's
// searched changed; then no match is shown, until step.
func (f *finder) key(k tea.KeyMsg) (bool, tea.Cmd) {
	var cmd tea.Cmd

	switch {
	case key.Matches(k, keyEnter):
		f.input.Blur()

		return false, nil
	case key.Matches(k, keyBack):
		f.input.SetValue("")
		f.input.Blur()
	default:
		f.input, cmd = f.input.Update(k)
	}

	f.pattern, f.match = nil, -1
	if q := f.input.Value(); q != "" {
		f.pattern = regexp.MustCompile("(?i)" + regexp.QuoteMeta(q))
	}

	return true, cmd
}

// scan notes which of lines the search matches, as they read: a card's line
// without its colors.
func (f *finder) scan(lines []string) {
	f.hits = nil
	if f.pattern == nil {
		return
	}

	for i, line := range lines {
		if f.pattern.MatchString(ansi.Strip(line)) {
			f.hits = append(f.hits, i)
		}
	}
}

// step shows the next match, in an older line with dir -1 or in a newer one with
// 1, round past the oldest and the newest; the newest without one shown, as logs
// are read from their end. It says whether there's one.
func (f *finder) step(dir int) bool {
	if len(f.hits) == 0 {
		return false
	}

	i := slices.Index(f.hits, f.match)
	if i < 0 {
		i = len(f.hits) - 1
	} else {
		i = (i + dir + len(f.hits)) % len(f.hits)
	}

	f.match = f.hits[i]

	return true
}

// highlight is lines with what the search found highlighted, and the match shown
// as a pill. A card's line found loses its colors, so that what's found stands out
// alone.
func (f *finder) highlight(lines []string) []string {
	if len(f.hits) == 0 {
		return lines
	}

	lines = slices.Clone(lines)

	for _, i := range f.hits {
		st := matchStyle
		if i == f.match {
			st = pillStyle
		}

		lines[i] = f.pattern.ReplaceAllStringFunc(ansi.Strip(lines[i]), func(s string) string { return st.Render(s) })
	}

	return lines
}

// view is the search, for after the title, while it's typed or set.
func (f *finder) view() string {
	if f.input.Focused() || f.input.Value() != "" {
		return " " + f.input.View()
	}

	return ""
}

// status says which match is shown, of how many: "2/5"; "" without a search.
func (f *finder) status() string {
	switch i := slices.Index(f.hits, f.match); {
	case f.pattern == nil:
		return ""
	case len(f.hits) == 0:
		return toneStyles[warn].Render("no match")
	case i >= 0:
		return dimStyle.Render(fmt.Sprintf("%d/%d", i+1, len(f.hits)))
	}

	return dimStyle.Render(fmt.Sprintf("%d matches", len(f.hits)))
}
