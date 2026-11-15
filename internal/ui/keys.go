package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// The TUI's keys, with their help. Screens handle them with key.Matches, and list
// those they handle on their help line, in order: one binding is both.
var (
	keyForceQuit = bind("", "", "ctrl+c") // even while typing
	keyQuit      = bind("q", "quit", "q")
	keyGoTo      = bind(":", "go to", ":")
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
	keySortName = bind("", "", "N") // k9s's sort keys: name, status, age
	keySortStat = bind("", "", "S")
	keySortAge  = bind("", "", "A")

	// The grid's.
	keyUp        = bind("", "", "up", "k")
	keyDown      = bind("", "", "down", "j")
	keyPageUp    = bind("", "", "pgup")
	keyPageDown  = bind("", "", "pgdown")
	keyHalfUp    = bind("", "", "ctrl+u")
	keyHalfDown  = bind("", "", "ctrl+d")
	keyTop       = bind("", "", "home", "g")
	keyBottom    = bind("", "", "end", "G")
	keyScroll    = bind("←→", "scroll", "left", "right")
	keyLeft      = bind("", "", "left")
	keyRight     = bind("", "", "right")
	keyColumn    = bind("⇧←→", "column", "shift+left", "shift+right")
	keyColLeft   = bind("", "", "shift+left")
	keyColRight  = bind("", "", "shift+right")
	keySort      = bind("O", "sort", "O")
	keyTopBottom = bind("g/G", "top/bottom", "home", "g", "end", "G")

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
