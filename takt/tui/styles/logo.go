// Package styles renders the Takt brand logo for the TUI from generated data.
package styles

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

//go:generate go run ../../../development/generate-logo -input ../../../docs/assets/brand/takt-ai.png -output logo_generated.go -blocks-output logo_blocks_generated.go -installer ../../../install.sh

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
	if theme.Mono() {
		return render(generatedLogo)
	}
	return render(generatedLogoBlocks)
}

// RenderCompactLogo draws the same mark at half size for terminals too short
// for the full logo, so the home never loses it (PR-UX-2A).
func RenderCompactLogo() string {
	if theme.Mono() {
		return render(generatedLogoCompact)
	}
	return render(generatedLogoBlocksCompact)
}

// render draws one generated dataset.
func render(lines [][]logoSpan) string {
	var logo strings.Builder
	for lineIndex, line := range lines {
		for _, span := range line {
			if theme.Mono() {
				// Monochrome emits no styling at all; the Braille shape carries the mark.
				logo.WriteString(span.Text)
				continue
			}
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
