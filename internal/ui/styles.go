package ui

import "github.com/charmbracelet/lipgloss"

// The TUI's colors and styles: the accent for what sshwarm draws, tones for what
// the machines report.

// accent marks what sshwarm draws itself, the title, tabs, cursor and keys, apart
// from the tones of what the machines report. A terminal palette color, like those,
// so it follows the user's theme.
var accent = lipgloss.Color("5") // magenta

var (
	titleStyle = lipgloss.NewStyle().Bold(true)
	dimStyle   = lipgloss.NewStyle().Faint(true)
	keyStyle   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	pillStyle  = keyStyle.Reverse(true)
	matchStyle = lipgloss.NewStyle().Reverse(true) // what a search found; the match shown is a pill
)

// pill highlights text with the accent color as its background.
func pill(text string) string { return pillStyle.Render(" " + text + " ") }

// tone colors a row, or one cell when it alone needs attention (like k9s).
// Terminal palette colors, so they follow the user's theme.
type tone int

// Tones rank by severity, as conditions do (toneOf): a status sorts on its tone,
// worst first.
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
