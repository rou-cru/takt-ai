package styles

import (
	"strings"

	"charm.land/lipgloss/v2"
)

//go:generate go run ../../../cmd/generate-logo -input ../../../docs/assets/brand/takt-ai-logo-source.png -output logo_generated.go

// RenderLogo renders the generated Takt branding asset with its source colors.
func RenderLogo() string {
	var logo strings.Builder
	for lineIndex, line := range generatedLogo {
		for _, span := range line {
			style := lipgloss.NewStyle()
			if span.Color != "" {
				style = style.Foreground(lipgloss.Color(span.Color))
			}
			logo.WriteString(style.Render(span.Text))
		}
		if lineIndex < len(generatedLogo)-1 {
			logo.WriteByte('\n')
		}
	}
	return logo.String()
}
