package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

// maxLogLines is what a logs view keeps of a followed stream: far more than a
// screen to scroll back through, in bounded memory.
const maxLogLines = 5000

// copyText puts text on the clipboard: with the local clipboard tool (wl-copy, xclip,
// pbcopy...) when there is one, or else through the terminal (OSC 52), which works
// over ssh but which some terminals garble, turning newlines into NULs. Tests
// replace it.
var copyText = func(text string) {
	if err := clipboard.WriteAll(text); err != nil {
		termenv.Copy(text)
	}
}

// logLinesMsg brings a stream's next lines; ch tells which stream they come from.
type logLinesMsg struct {
	ch    <-chan string
	lines []string
	done  bool
}

// text is a screen showing a remote command's output: logs, as a stream it can
// follow, or a description, drawn as a card once all of it is in.
type text struct {
	title  string // after the machine's name: a unit, "stack db", "describe web-1"...
	host   swarm.Host
	cmd    string // as swarm.Stream runs it
	lines  []string
	ch     <-chan string // the current stream, nil if none
	cancel context.CancelFunc

	card               cardFunc  // a description's; nil for logs, which can be followed instead
	runs               int       // of the command: a refresh only follows the latest
	refreshed          time.Time // when the last run ended
	raw                bool      // shows a description's output as it came, not its card
	follow, wrap, full bool      // full: no frame or help line
	pager              viewport.Model
}

func newText(title string, h swarm.Host, cmd string, card cardFunc) *text {
	t := &text{title: title, host: h, cmd: cmd, card: card, wrap: true, pager: viewport.New(0, 0)}
	t.pager.SetHorizontalStep(8) // left and right, once lines don't wrap

	return t
}

func (t *text) open(m *model) tea.Cmd { return t.start(m.ctx, false) }

// start (re)runs the command, following its output or not.
func (t *text) start(ctx context.Context, follow bool) tea.Cmd {
	again := t.card != nil && len(t.lines) > 0 // a description stays, where it was scrolled, until the next is in

	t.stop()
	ctx, t.cancel = context.WithCancel(ctx)
	t.follow = follow
	t.runs++

	if !again {
		t.pager.SetContent("loading…")
	}

	t.ch = swarm.Stream(ctx, t.host, t.cmd, follow)

	return waitLog(t.ch)
}

// stop ends the stream and drops its lines.
func (t *text) stop() {
	if t.cancel != nil {
		t.cancel()
	}

	t.cancel, t.ch, t.lines = nil, nil, nil
}

// add shows a stream's next lines, and waits for more.
func (t *text) add(msg logLinesMsg) tea.Cmd {
	bottom := t.pager.AtBottom() || len(t.lines) == 0

	t.lines = append(t.lines, msg.lines...)
	if len(t.lines) > maxLogLines {
		t.lines = t.lines[len(t.lines)-maxLogLines:]
	}

	if msg.done && t.follow {
		t.lines = append(t.lines, "[stream ended]")
	}

	if msg.done {
		t.ch, t.refreshed = nil, time.Now() // no stream any more
	}

	t.setContent()

	if bottom && t.card == nil { // logs follow their end; a description opens at its top
		t.pager.GotoBottom()
	}

	if msg.done {
		return nil
	}

	return waitLog(t.ch)
}

// setContent draws a description's card once all its output is in, or else wraps
// long lines to the window. Without wrapping, a card is as wide as its content, and
// lines that are wider than the window scroll left and right.
func (t *text) setContent() {
	content := strings.Join(t.lines, "\n")

	if t.showsCard() {
		if t.ch != nil {
			return // still "loading…"
		}

		width := t.pager.Width
		if !t.wrap {
			width = 0
		}

		if drawn, err := t.card(content, width); err == nil {
			t.pager.SetContent(drawn)

			return
		}
	}

	if t.wrap {
		content = lipgloss.NewStyle().Width(t.pager.Width).Render(content)
	}

	t.pager.SetContent(content)
}

// resize gives the pager the window, but for the frame unless fullscreen.
func (t *text) resize(width, height int) {
	if !t.full {
		width, height = width-sides, height-chrome
	}

	t.pager.Width, t.pager.Height = max(width, 1), max(height, 1)
	t.setContent()
}

func (t *text) key(m *model, k tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch k.String() {
	case "esc":
		m.pop()
	case "r": // runs the command again: a description's latest state, or the latest logs
		return t.start(m.ctx, t.follow)
	case "s":
		if t.card == nil {
			return t.start(m.ctx, !t.follow)
		}
	case "v":
		if t.card != nil {
			t.raw = !t.raw
			t.setContent()
			t.pager.GotoTop()
		}
	case "w":
		t.wrap = !t.wrap
		t.setContent()
		t.pager.SetXOffset(0) // wrapped, no line is wider than the window: show them whole
	case "f":
		t.full = !t.full
		t.resize(m.width, m.height)
	case "c":
		copyText(strings.Join(t.lines, "\n"))
		m.flash = fmt.Sprintf("copied %d lines", len(t.lines))
	case "g", "home":
		t.pager.GotoTop()
	case "G", "end":
		t.pager.GotoBottom()
	default:
		t.pager, cmd = t.pager.Update(k)
	}

	return cmd
}

func (t *text) view(m *model) string {
	if t.full { // the command bar or a message still shows, on the last line
		lines := strings.Split(t.pager.View(), "\n")
		if bar := m.bar(); bar != "" {
			lines[len(lines)-1] = bar
		}

		return strings.Join(lines, "\n")
	}

	title := crumbs(t.host.Name, t.title)

	status := dimStyle.Render(fmt.Sprintf("%3.f%%", t.pager.ScrollPercent()*100))
	if t.card != nil && !t.refreshed.IsZero() { // a description refreshes: say how fresh it is
		when := dimStyle.Render("refreshed " + t.refreshed.Format(time.TimeOnly))
		if t.ch != nil {
			when = m.spin.View() + dimStyle.Render(" refreshing")
		}

		status = when + dimStyle.Render(" · ") + status
	}

	wrap := "w wrap"

	switch {
	case t.showsCard() && t.wrap:
		wrap = "w full width"
	case t.showsCard():
		wrap = "w fit"
	}

	help := wrap + " · f fullscreen · c copy · g/G top/bottom · r refresh · esc back · q quit"

	switch {
	case t.follow:
		help = "s stop following · " + help
	case t.card == nil:
		help = "s follow · " + help
	case t.raw:
		help = "v card · " + help
	default:
		help = "v raw · " + help
	}

	return m.frame(title, status, t.pager.View(), help)
}

// redescribeMsg asks a description's screen to run again, unless it's closed or ran
// since, at r.
type redescribeMsg struct {
	t   *text
	run int
}

// again describes again after a while, as tables refresh.
func (t *text) again(after time.Duration) tea.Cmd {
	msg := redescribeMsg{t, t.runs}

	return tea.Tick(after, func(time.Time) tea.Msg { return msg })
}

// showsCard says whether the screen shows its output as a card, not as it came.
func (t *text) showsCard() bool { return t.card != nil && !t.raw }

// waitLog delivers the next lines of a stream, gathered for up to 50 ms, so a
// busy stream doesn't re-render the screen for every line.
func waitLog(ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		line, ok := <-ch
		if !ok {
			return logLinesMsg{ch: ch, done: true}
		}

		lines := []string{line}
		batch := time.After(50 * time.Millisecond)

		for {
			select {
			case l, ok := <-ch:
				if !ok {
					return logLinesMsg{ch: ch, lines: lines, done: true}
				}

				lines = append(lines, l)
			case <-batch:
				return logLinesMsg{ch: ch, lines: lines}
			}
		}
	}
}
