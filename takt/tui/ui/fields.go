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

// Row is one line of a Group: Lead carries the fact (a count and its noun,
// a file name), Detail the muted qualifier after it. An empty Lead continues
// the row above.
type Row struct {
	Lead   string
	Detail string
}

// Group renders a titled group: the heading in the secondary role, then
// each row with its Lead in one aligned column and its Detail muted after
// it, so the facts read first and the qualifiers stay out of their way.
func Group(title string, rows []Row) string {
	// Only rows with a detail align; a lone lead (a total) stays out of the
	// column so it cannot push every detail to the right.
	width := 0
	for _, row := range rows {
		if row.Detail != "" {
			width = max(width, lipgloss.Width(row.Lead))
		}
	}
	var out strings.Builder
	out.WriteString(theme.Secondary.Render(title) + "\n")
	for _, row := range rows {
		line := theme.Label.Render(row.Lead)
		if row.Detail != "" {
			line += strings.Repeat(" ", width-lipgloss.Width(row.Lead)+fieldGap) + theme.Caption.Render(row.Detail)
		}
		out.WriteString(line + "\n")
	}
	return out.String()
}
