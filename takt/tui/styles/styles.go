// Package styles contains reusable brand presentation primitives.
package styles

import "github.com/rou-cru/takt-ai/takt/tui/theme"

var (
	TitleStyle      = theme.Display
	HeadingStyle    = theme.Title
	HelpStyle       = theme.Caption
	SubtextStyle    = theme.Caption
	SelectedStyle   = theme.Display
	UnselectedStyle = theme.Label
	SuccessStyle    = theme.Label.Foreground(theme.Success)
	ErrorStyle      = theme.Label.Foreground(theme.Danger)
	WarningStyle    = theme.Label.Foreground(theme.Warning)
)
