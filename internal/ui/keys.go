package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// The TUI's keys, with their help. Screens handle them with key.Matches, and list
// those they handle on their help line, in order, and those that move around on the
// help screen: one binding is both. Those without help are listed by another.
var (
	keyForceQuit = bind("^c", "quit, even while typing", "ctrl+c")
	keyQuit      = bind("q", "quit", "q")
	keyGoTo      = bind(":", "go to", ":")
	keyHelp      = bind("?", "help", "?")
	keyBack      = bind("esc", "back", "esc")
	keyEnter     = bind("enter", "", "enter")
	keyYes       = bind("y", "", "y")
	keyRefresh   = bind("r", "refresh", "r")

	// The lists'.
	keyFilter   = bind("/", "filter", "/")
	keyLogs     = bind("l", "logs", "l")
	keyDescribe = bind("d", "describe", "d")
	keyShell    = bind("s", "shell", "s")
	keyStart    = bind("u", "start", "u") // up
	keyStop     = bind("^k", "stop", "ctrl+k")
	keyRestart  = bind("R", "restart", "R")
	keyTab      = bind("tab", "switch", "tab", "shift+tab")
	keyNextTab  = bind("", "", "tab")
	keyPrevTab  = bind("", "", "shift+tab")
	keyAll      = bind("a", "all units", "a")
	keyOwner    = bind("J", "stack", "J")
	keySortName = bind("N", "sort on the name", "N") // k9s's sort keys: name, status, age
	keySortStat = bind("S", "sort on the state", "S")
	keySortAge  = bind("A", "sort on the age", "A")

	// The grid's.
	keyUp       = bind("↑/k", "up", "up", "k")
	keyDown     = bind("↓/j", "down", "down", "j")
	keyPageUp   = bind("pgup", "page up", "pgup")
	keyPageDown = bind("pgdn", "page down", "pgdown")
	keyHalfUp   = bind("^u", "½ page up", "ctrl+u")
	keyHalfDown = bind("^d", "½ page down", "ctrl+d")
	keyTop      = bind("g/home", "top", "home", "g")
	keyBottom   = bind("G/end", "bottom", "end", "G")
	keyScroll   = bind("←→", "scroll the columns", "left", "right")
	keyLeft     = bind("", "", "left")
	keyRight    = bind("", "", "right")
	keyColumn   = bind("⇧←→", "select a column", "shift+left", "shift+right")
	keyColLeft  = bind("", "", "shift+left")
	keyColRight = bind("", "", "shift+right")
	keySort     = bind("O", "sort on the column selected", "O")

	// The output screen's.
	keyFollow = bind("s", "follow", "s")
	keyRaw    = bind("v", "raw", "v")
	keyWrap   = bind("w", "wrap", "w")
	keyFull   = bind("f", "fullscreen", "f")
	keyCopy   = bind("c", "copy", "c")
	keyMatch  = bind("n/N", "older/newer", "n", "N") // the search's matches
	keyOlder  = bind("", "", "n")
	keyNewer  = bind("", "", "N")
)

// bind binds keys, with the help k and desc.
func bind(k, desc string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(k, desc))
}

// gridMoves are the keys that move around a list, and sort it, for the help screen.
var gridMoves = []key.Binding{
	keyUp, keyDown, keyPageUp, keyPageDown, keyHalfUp, keyHalfDown, keyTop, keyBottom,
	keyScroll, keyColumn, keySort, keySortName, keySortStat, keySortAge,
}

// as is b, its help saying desc instead: what it does there.
func as(b key.Binding, desc string) key.Binding {
	b.SetHelp(b.Help().Key, desc)

	return b
}

// helpLine styles a screen's help, from the keys it handles: the keys stand out in
// the accent color, what they do is dim.
func helpLine(keys []key.Binding) string {
	var parts []string

	for _, b := range keys {
		h := b.Help()
		parts = append(parts, keyStyle.Render(h.Key)+" "+dimStyle.Render(h.Desc))
	}

	return strings.Join(parts, dimStyle.Render(" • "))
}
