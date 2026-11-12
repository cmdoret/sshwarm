package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cmdoret/sshwarm/internal/swarm"
)

const maxLogLines = 5000

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
	remote string // the command, without follow
	follow bool
	lines  []string
	ch     <-chan string // the current stream, nil if none
	cancel context.CancelFunc
	pager  viewport.Model
}

func newText(title string, h swarm.Host, remote string) *text {
	return &text{title: title, host: h, remote: remote, pager: viewport.New(0, 0)}
}

func (t *text) open(m *model) tea.Cmd { return t.start(m.ctx, false) }

// start (re)runs the command, following its output or not.
func (t *text) start(ctx context.Context, follow bool) tea.Cmd {
	t.stop()
	ctx, t.cancel = context.WithCancel(ctx)
	t.follow = follow
	t.pager.SetContent("loading…")
	t.ch = swarm.StreamLines(ctx, t.host, swarm.LogCmd(t.remote, follow), follow)

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

// setContent wraps long lines to the window instead of cutting them off.
func (t *text) setContent() {
	t.pager.SetContent(lipgloss.NewStyle().Width(t.pager.Width).Render(strings.Join(t.lines, "\n")))
}

func (t *text) resize(width, height int) {
	t.pager.Width, t.pager.Height = width, max(height-2, 1)
	t.setContent()
}

func (t *text) key(m *model, k tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch k.String() {
	case "esc":
		t.stop()
		m.pop()
	case "f":
		return t.start(m.ctx, !t.follow)
	case "g", "home":
		t.pager.GotoTop()
	case "G", "end":
		t.pager.GotoBottom()
	default:
		t.pager, cmd = t.pager.Update(k)
	}

	return cmd
}

func (t *text) view(*model) string {
	title := titleStyle.Render(t.title) + dimStyle.Render(fmt.Sprintf(" · %3.f%%", t.pager.ScrollPercent()*100))

	follow := "f follow"
	if t.follow {
		follow = "f stop following"
	}

	return frame(title, t.pager.View(), follow+" · g/G top/bottom · esc back · q quit")
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

		for len(lines) < 1000 {
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

		return logLinesMsg{ch: ch, lines: lines}
	}
}
