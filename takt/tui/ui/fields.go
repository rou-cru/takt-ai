package ui

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// Field is one label/value row of an aligned summary; an empty Label
// continues the value of the row above.
type Field struct {
	Label string
	Value string
}

// fieldGap separates the label column from the values.
const fieldGap = 2

// Fields renders a summary with labels in one secondary column and values
// aligned after it, the label/value grammar of the review wireframe.
func Fields(fields []Field) string {
	width := 0
	for _, field := range fields {
		width = max(width, lipgloss.Width(field.Label))
	}
	var out strings.Builder
	for _, field := range fields {
		pad := strings.Repeat(" ", width-lipgloss.Width(field.Label)+fieldGap)
		out.WriteString(theme.Secondary.Render(field.Label) + pad + theme.Label.Render(field.Value) + "\n")
	}
	return out.String()
}
