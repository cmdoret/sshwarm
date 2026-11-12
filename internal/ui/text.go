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

// text is a screen showing a remote command's output, such as logs, as a stream
// it can follow.
type text struct {
	title  string
	host   swarm.Host
	cmd    string // the log command, as swarm.Logs runs it
	lines  []string
	ch     <-chan string // the current stream, nil if none
	cancel context.CancelFunc

	follow, wrap, full bool // full: no title and help lines
	pager              viewport.Model
}

func newText(title string, h swarm.Host, cmd string) *text {
	t := &text{title: title, host: h, cmd: cmd, wrap: true, pager: viewport.New(0, 0)}
	t.pager.SetHorizontalStep(8) // left and right, once lines don't wrap

	return t
}

func (t *text) open(m *model) tea.Cmd { return t.start(m.ctx, false) }

// start (re)runs the command, following its output or not.
func (t *text) start(ctx context.Context, follow bool) tea.Cmd {
	t.stop()
	ctx, t.cancel = context.WithCancel(ctx)
	t.follow = follow
	t.pager.SetContent("loading…")
	t.ch = swarm.Logs(ctx, t.host, t.cmd, follow)

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

	t.setContent()

	if bottom {
		t.pager.GotoBottom()
	}

	if msg.done {
		return nil
	}

	return waitLog(t.ch)
}

// setContent wraps long lines to the window, unless wrapping is off: then left and
// right scroll.
func (t *text) setContent() {
	content := strings.Join(t.lines, "\n")
	if t.wrap {
		content = lipgloss.NewStyle().Width(t.pager.Width).Render(content)
	}

	t.pager.SetContent(content)
}

// resize gives the pager the window, but for the title and help lines unless
// fullscreen.
func (t *text) resize(width, height int) {
	if !t.full {
		height -= chrome
	}

	t.pager.Width, t.pager.Height = width, max(height, 1)
	t.setContent()
}

func (t *text) key(m *model, k tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch k.String() {
	case "esc":
		m.pop()
	case "s":
		return t.start(m.ctx, !t.follow)
	case "w":
		t.wrap = !t.wrap
		t.setContent()
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

	title := titleStyle.Render(t.title) + dimStyle.Render(fmt.Sprintf(" · %3.f%%", t.pager.ScrollPercent()*100))

	follow := "s follow"
	if t.follow {
		follow = "s stop following"
	}

	return m.frame(title, t.pager.View(), follow+" · w wrap · f fullscreen · c copy · g/G top/bottom · esc back · q quit")
}

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
