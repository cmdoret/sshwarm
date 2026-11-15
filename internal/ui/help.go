package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// anywhere are the keys every screen takes, for the help screen.
var anywhere = []key.Binding{as(keyGoTo, "go to: :ctr web, :svc all"), keyHelp, keyQuit, keyForceQuit}

// helpScreen lists the keys of the screen under it, as k9s's does: its own, then
// those that move around it, then those every screen takes.
type helpScreen struct {
	blocks []string // one per kind of keys, each a card
	pager  viewport.Model
}

// newHelp is the help screen of the screen whose frame is under: its help line,
// but for the keys every screen takes, and its moves.
func newHelp(under frame) *helpScreen {
	own := slices.DeleteFunc(slices.Clone(under.help), func(b key.Binding) bool {
		return slices.ContainsFunc(anywhere, func(a key.Binding) bool { return a.Help().Key == b.Help().Key })
	})

	return &helpScreen{
		blocks: []string{keysCard("Here", own), keysCard("Moving around", under.moves), keysCard("Anywhere", anywhere)},
		pager:  viewport.New(0, 0),
	}
}

// keysCard is a card of keys and what they do, headed name; "" without keys.
func keysCard(name string, keys []key.Binding) string {
	if len(keys) == 0 {
		return ""
	}

	var c card

	c.section(name)

	for _, b := range keys {
		c.field(b.Help().Key, b.Help().Desc)
	}

	return strings.TrimSuffix(c.b.String(), "\n")
}

func (h *helpScreen) open(*model) tea.Cmd { return nil }

func (h *helpScreen) update(m *model, msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}

	if key.Matches(k, keyBack) {
		m.pop()

		return nil
	}

	var cmd tea.Cmd
	h.pager, cmd = h.pager.Update(k)

	return cmd
}

// resize lays the cards out side by side if they fit in the window, or else one
// under the other, to scroll through.
func (h *helpScreen) resize(width, height int) {
	w, bodyHeight := bodySize(width, height, false)
	h.pager.Width, h.pager.Height = max(w, 1), max(bodyHeight, 1)

	blocks := slices.DeleteFunc(slices.Clone(h.blocks), func(b string) bool { return b == "" })

	var row []string

	for i, b := range blocks {
		if i > 0 {
			row = append(row, "    ")
		}

		row = append(row, b)
	}

	content := lipgloss.JoinHorizontal(lipgloss.Top, row...)
	if lipgloss.Width(content) > w {
		content = strings.Join(blocks, "\n\n")
	}

	h.pager.SetContent(content)
}

func (h *helpScreen) view(*model) frame {
	return frame{
		title: crumbs("help"), body: h.pager.View(),
		status: dimStyle.Render(fmt.Sprintf("%3.f%%", h.pager.ScrollPercent()*100)), // a narrow window scrolls
		help:   []key.Binding{keyBack, keyQuit, as(keyHelp, "back")},
	}
}

func (h *helpScreen) close()       {}
func (h *helpScreen) busy() bool   { return false }
func (h *helpScreen) typing() bool { return false }
