package ui

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/cmdoret/sshwarm/internal/inventory"
	"github.com/cmdoret/sshwarm/internal/swarm"
)

// maxLogLines is what a logs view keeps of a followed stream: far more than a
// screen to scroll back through, in bounded memory.
const maxLogLines = 5000

// copyText puts text on the clipboard: with the local clipboard tool (wl-copy, xclip,
// pbcopy...) when there is one, or else through the terminal (OSC 52).
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

// output is a screen showing a remote command's output: logs, as a stream it can
// follow, or a description, drawn as a card once all of it is in.
type output struct {
	title  string // after the machine's name: a unit, "stack db", "describe web-1"...
	host   inventory.Host
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
	shown              string // what the pager shows, unwrapped; "" while loading: what c copies

	search  textinput.Model // what / looks for in the lines, ignoring case
	pattern *regexp.Regexp  // the search, nil if none
	hits    []int           // the lines it matches, in order
	match   int             // the line of the match shown, -1 if none
}

func newOutput(title string, h inventory.Host, cmd string, card cardFunc) *output {
	s := textinput.New()
	s.Prompt = "/"

	o := &output{title: title, host: h, cmd: cmd, card: card, wrap: true, pager: viewport.New(0, 0), search: s, match: -1}
	o.pager.SetHorizontalStep(8) // left and right, over a card or unwrapped lines

	return o
}

func (o *output) open(m *model) tea.Cmd { return o.start(m.ctx, false) }

// start (re)runs the command, following its output or not.
func (o *output) start(ctx context.Context, follow bool) tea.Cmd {
	again := o.card != nil && len(o.lines) > 0 // a description stays, where it was scrolled, until the next is in

	o.stop()
	ctx, o.cancel = context.WithCancel(ctx)
	o.follow = follow
	o.runs++

	if !again {
		o.shown = ""
		o.pager.SetContent("loading…")
	}

	o.ch = swarm.Stream(ctx, o.host, o.cmd, follow)

	return waitLog(o.ch)
}

// stop ends the stream and drops its lines.
func (o *output) stop() {
	if o.cancel != nil {
		o.cancel()
	}

	o.cancel, o.ch, o.lines, o.match = nil, nil, nil, -1
}

// add shows a stream's next lines, and waits for more.
func (o *output) add(msg logLinesMsg) tea.Cmd {
	bottom := o.pager.AtBottom() || len(o.lines) == 0

	o.lines = append(o.lines, msg.lines...)
	if cut := len(o.lines) - maxLogLines; cut > 0 {
		o.lines, o.match = o.lines[cut:], o.match-cut // the match shown moves up with its line
	}

	if msg.done && o.follow {
		o.lines = append(o.lines, "[stream ended]")
	}

	if msg.done {
		o.ch, o.refreshed = nil, time.Now() // no stream any more
	}

	o.setContent()

	if bottom && o.card == nil { // logs follow their end; a description opens at its top
		o.pager.GotoBottom()
	}

	if msg.done {
		return nil
	}

	return waitLog(o.ch)
}

// setContent draws a description's card once all its output is in, or else shows
// the output as it came, its long lines wrapped to the window unless wrapping is
// off. Lines wider than the window, a card's or unwrapped, scroll left and right.
func (o *output) setContent() {
	if o.showsCard() && o.ch != nil {
		return // still "loading…", or the last card: not the next one's partial output
	}

	o.shown = strings.Join(o.lines, "\n")

	if o.showsCard() {
		if drawn, err := o.card(o.shown); err == nil {
			o.shown = drawn
			o.pager.SetContent(drawn)

			return
		}
	}

	content := o.highlighted()
	if o.wrap {
		content = o.wrapped(content)
	}

	o.pager.SetContent(content)
}

// wrapped wraps s's lines to the window.
func (o *output) wrapped(s string) string { return lipgloss.NewStyle().Width(o.pager.Width).Render(s) }

// highlighted is the lines, with what the search found highlighted, and the match
// shown as a pill. It notes the lines that match in hits.
func (o *output) highlighted() string {
	o.hits = nil
	if o.pattern == nil {
		return o.shown
	}

	lines := slices.Clone(o.lines)

	for i, line := range lines {
		if !o.pattern.MatchString(line) {
			continue
		}

		o.hits = append(o.hits, i)

		st := matchStyle
		if i == o.match {
			st = pillStyle
		}

		lines[i] = o.pattern.ReplaceAllStringFunc(line, func(s string) string { return st.Render(s) })
	}

	return strings.Join(lines, "\n")
}

// find searches the lines for what's typed, ignoring case, and shows the newest
// match: logs are read from their end.
func (o *output) find() {
	o.pattern, o.match = nil, -1
	if q := o.search.Value(); q != "" {
		o.pattern = regexp.MustCompile("(?i)" + regexp.QuoteMeta(q))
	}

	o.setContent()
	o.step(-1)
}

// step shows the next match, in an older line with dir -1 or in a newer one with
// 1, round past the oldest and the newest; the newest without one shown.
func (o *output) step(dir int) {
	if len(o.hits) == 0 {
		return
	}

	i := slices.Index(o.hits, o.match)
	if i < 0 {
		i = len(o.hits) - 1
	} else {
		i = (i + dir + len(o.hits)) % len(o.hits)
	}

	o.match = o.hits[i]
	o.setContent() // the match shown becomes the pill

	// A third down the window: what led to it shows above, what followed below.
	o.pager.SetYOffset(o.offset(o.match) - o.pager.Height/3)
}

// offset is the pager's line where line i of the output starts: past the lines
// before it, as wrapped.
func (o *output) offset(i int) int {
	if !o.wrap || i == 0 {
		return i
	}

	return lipgloss.Height(o.wrapped(strings.Join(o.lines[:i], "\n")))
}

// searchKey types the search, which shows its matches as it's typed: enter keeps
// it, esc drops it.
func (o *output) searchKey(k tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd

	switch {
	case key.Matches(k, keyBack):
		o.search.SetValue("")
		o.search.Blur()
	case key.Matches(k, keyEnter):
		o.search.Blur()

		return nil
	default:
		o.search, cmd = o.search.Update(k)
	}

	o.find()

	return cmd
}

// resize gives the pager the window, but for the frame unless fullscreen.
func (o *output) resize(width, height int) {
	w, h := bodySize(width, height, o.full)
	o.pager.Width, o.pager.Height = max(w, 1), max(h, 1)
	o.setContent()
}

// update takes a key, the next lines of the stream on screen, or a request to
// describe again, unless it's for a run since replaced.
func (o *output) update(m *model, msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return o.key(m, msg)
	case logLinesMsg:
		if msg.ch != o.ch {
			return nil // a stream since stopped
		}

		if cmd := o.add(msg); !msg.done || o.card == nil {
			return cmd
		}

		return o.again(m.probes.interval) // a description refreshes as tables do
	case redescribeMsg:
		if msg.o == o && msg.run == o.runs {
			return o.start(m.ctx, false)
		}
	}

	return nil
}

func (o *output) close()       { o.stop() }
func (o *output) busy() bool   { return o.card != nil && o.ch != nil } // describing
func (o *output) typing() bool { return o.search.Focused() }

func (o *output) key(m *model, k tea.KeyMsg) tea.Cmd {
	if o.search.Focused() {
		return o.searchKey(k)
	}

	var cmd tea.Cmd

	switch {
	case key.Matches(k, keyFilter):
		if !o.showsCard() { // a card is short, and drawn whole
			return o.search.Focus()
		}
	case key.Matches(k, keyOlder):
		o.step(-1)
	case key.Matches(k, keyNewer):
		o.step(1)
	case key.Matches(k, keyBack):
		m.pop()
	case key.Matches(k, keyRefresh): // runs the command again: a description's latest state, or the latest logs
		return o.start(m.ctx, o.follow)
	case key.Matches(k, keyFollow):
		if o.card == nil {
			return o.start(m.ctx, !o.follow)
		}
	case key.Matches(k, keyRaw):
		if o.card != nil {
			o.raw = !o.raw
			o.setContent()
			o.pager.GotoTop()
		}
	case key.Matches(k, keyWrap):
		if !o.showsCard() { // a card is drawn whole: nothing in it to wrap
			o.wrap = !o.wrap
			o.setContent()
			o.pager.SetXOffset(0) // wrapped, no line is wider than the window: show them whole
		}
	case key.Matches(k, keyFull):
		o.full = !o.full
		o.resize(m.width, m.height)
	case key.Matches(k, keyCopy):
		m.flash = o.copyShown()
	case key.Matches(k, keyTop):
		o.pager.GotoTop()
	case key.Matches(k, keyBottom):
		o.pager.GotoBottom()
	default:
		o.pager, cmd = o.pager.Update(k)
	}

	return cmd
}

func (o *output) view(m *model) frame {
	if o.full {
		return frame{body: o.pager.View(), full: true}
	}

	title := crumbs(o.host.Name, o.title)
	if o.search.Focused() || o.search.Value() != "" {
		title += " " + o.search.View()
	}

	status := dimStyle.Render(fmt.Sprintf("%3.f%%", o.pager.ScrollPercent()*100))
	if o.pattern != nil && !o.showsCard() {
		status = o.matchStatus() + dimStyle.Render(" · ") + status
	}
	if o.card != nil && !o.refreshed.IsZero() { // a description refreshes: say how fresh it is
		when := dimStyle.Render("refreshed " + o.refreshed.Format(time.TimeOnly))
		if o.ch != nil {
			when = m.spin.View() + dimStyle.Render(" refreshing")
		}

		status = when + dimStyle.Render(" · ") + status
	}

	var help []key.Binding

	switch {
	case o.follow:
		help = append(help, as(keyFollow, "stop following"))
	case o.card == nil:
		help = append(help, keyFollow)
	case o.raw:
		help = append(help, as(keyRaw, "card"))
	default:
		help = append(help, keyRaw)
	}

	if !o.showsCard() {
		help = append(help, as(keyFilter, "search"))
		if len(o.hits) > 0 {
			help = append(help, keyMatch)
		}

		help = append(help, keyWrap)
	}

	help = append(help, keyFull, keyCopy, keyTopBottom, keyRefresh, keyBack, keyQuit)

	return frame{title: title, status: status, body: o.pager.View(), help: help}
}

// matchStatus says which of the search's matches is shown, of how many: "2/5".
func (o *output) matchStatus() string {
	if len(o.hits) == 0 {
		return toneStyles[warn].Render("no match")
	}

	if i := slices.Index(o.hits, o.match); i >= 0 {
		return dimStyle.Render(fmt.Sprintf("%d/%d", i+1, len(o.hits)))
	}

	return dimStyle.Render(fmt.Sprintf("%d matches", len(o.hits)))
}

// redescribeMsg asks a description's screen to run again, unless it's closed or ran
// since, at r.
type redescribeMsg struct {
	o   *output
	run int
}

// again describes again after a while, as tables refresh.
func (o *output) again(after time.Duration) tea.Cmd {
	msg := redescribeMsg{o, o.runs}

	return tea.Tick(after, func(time.Time) tea.Msg { return msg })
}

// copyShown puts what's on screen on the clipboard, without colors: a card, never
// the output it hides, or the output's lines as they came, not as wrapped. It says
// what it did.
func (o *output) copyShown() string {
	if o.shown == "" {
		return "nothing to copy"
	}

	s := ansi.Strip(o.shown)
	copyText(s)

	return fmt.Sprintf("copied %d lines", strings.Count(s, "\n")+1)
}

// showsCard says whether the screen shows its output as a card, not as it came.
func (o *output) showsCard() bool { return o.card != nil && !o.raw }

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
