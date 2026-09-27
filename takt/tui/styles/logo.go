// Package styles renders the Takt brand logo for the TUI from generated data.
package styles

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

//go:generate go run ../../../cmd/generate-logo -input ../../../docs/assets/brand/takt-ai.png -output logo_generated.go -blocks-output logo_blocks_generated.go

// logoSpan holds one colored text run so logo rows stay compact. The type
// lives here, hand-written, because both generated datasets share it.
type logoSpan struct {
	Text  string
	Color string
	Bg    string
}

// RenderLogo draws the brand artwork so the menu has one recognizable mark.
// Color terminals draw the half-block dataset; mono terminals keep the
// Braille mark, whose shape survives the loss of color.
func RenderLogo() string {
	lines := generatedLogoBlocks
	if theme.Mono() {
		lines = generatedLogo
	}
	var logo strings.Builder
	for lineIndex, line := range lines {
		for _, span := range line {
			style := lipgloss.NewStyle()
			if span.Color != "" {
				style = style.Foreground(lipgloss.Color(span.Color))
			}
			if span.Bg != "" {
				style = style.Background(lipgloss.Color(span.Bg))
			}
			logo.WriteString(style.Render(span.Text))
		}
		if lineIndex < len(lines)-1 {
			logo.WriteByte('\n')
		}
	}
	return logo.String()
}
