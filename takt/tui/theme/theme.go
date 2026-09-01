// Package theme defines the visual vocabulary shared by TUI primitives.
package theme

import "charm.land/lipgloss/v2"

const (
	overlay = "#26233a"
	love    = "#eb6f92"
	gold    = "#f6c177"
	rose    = "#ebbcba"
	foam    = "#9ccfd8"
	iris    = "#c4a7e7"
	text    = "#e0def4"
	muted   = "#6e6a86"
)

var (
	Primary = lipgloss.Color(iris)
	Accent  = lipgloss.Color(rose)
	Text    = lipgloss.Color(text)
	Muted   = lipgloss.Color(muted)
	Success = lipgloss.Color(foam)
	Warning = lipgloss.Color(gold)
	Danger  = lipgloss.Color(love)

	Border      = Muted
	BorderFocus = Primary
	SelectionBg = lipgloss.Color(overlay)
)

const (
	SpaceSM = 2
	SpaceLG = 1
)

var (
	Display = lipgloss.NewStyle().Foreground(Primary).Bold(true)
	Title   = lipgloss.NewStyle().Foreground(Text).Bold(true)
	Label   = lipgloss.NewStyle().Foreground(Text)
	Caption = lipgloss.NewStyle().Foreground(Muted).Faint(true)
	Frame   = lipgloss.NewStyle().Border(lipgloss.DoubleBorder()).BorderForeground(BorderFocus).Padding(SpaceLG, SpaceSM)
)

var Icon = struct {
	Check       string
	Cross       string
	Warn        string
	Dot         string
	Cursor      string
	CheckboxOn  string
	CheckboxOff string
	RadioOn     string
	RadioOff    string
}{
	Check:       "✓",
	Cross:       "✗",
	Warn:        "⚠",
	Dot:         "·",
	Cursor:      "▸ ",
	CheckboxOn:  "[x]",
	CheckboxOff: "[ ]",
	RadioOn:     "(*)",
	RadioOff:    "( )",
}
