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

// glyphs is the color logo's character set: half-blocks until Run measures
// the terminal with ProbeGlyphs.
var glyphs = GlyphsHalfBlocks

// SetGlyphs sets the color logo's character set: Run stores what
// ProbeGlyphs measured, and fixtures pin half-blocks.
func SetGlyphs(g Glyphs) { glyphs = g }

// Logo draws the largest variant of the brand mark that fits in width×height
// cells, or "" when none does. Mono terminals get the Braille mark, whose
// shape survives the loss of color; color terminals get octants or
// half-blocks, as SetGlyphs last chose.
func Logo(width, height int) string {
	variants := generatedLogoHalfBlocks
	switch {
	case theme.Mono():
		variants = generatedLogoBraille
	case glyphs == GlyphsOctants:
		variants = generatedLogoOctants
	}
	for _, variant := range variants {
		if len(variant) <= height && variantWidth(variant) <= width {
			return render(variant)
		}
	}
	return ""
}

// variantWidth is the cell width of a variant's first row; every row of a
// generated variant has the same width.
func variantWidth(lines [][]logoSpan) int {
	if len(lines) == 0 {
		return 0
	}
	width := 0
	for _, span := range lines[0] {
		width += lipgloss.Width(span.Text)
	}
	return width
}

// render draws one generated variant.
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
