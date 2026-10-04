// Package styles renders the Takt brand logo for the TUI from generated data.
package styles

import (
	"os"
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

// Glyphs is the character set the color logo is drawn with.
type Glyphs string

const (
	// GlyphsOctants draws 2×4 pixels per cell with the Unicode 16 block
	// octants: the sharpest mark, for terminals known to draw them.
	GlyphsOctants Glyphs = "octants"
	// GlyphsHalfBlocks draws 1×2 pixels per cell with ▀ ▄ █, which every
	// terminal font has: the mark any other terminal shows as intended.
	GlyphsHalfBlocks Glyphs = "halfblocks"
)

// glyphsEnv overrides glyph detection: "octants" or "halfblocks".
const glyphsEnv = "TAKT_LOGO_GLYPHS"

// octantTerminals are the TERM_PROGRAM or TERM values of terminals that draw
// the octants themselves instead of relying on the font. Anything else,
// multiplexers included, gets half-blocks: an unknown terminal never gets a
// glyph it may lack.
var octantTerminals = []string{"kitty", "ghostty", "foot"}

// DetectGlyphs picks the logo's character set from the environment: the
// TAKT_LOGO_GLYPHS override first, then the known octant terminals.
func DetectGlyphs() Glyphs {
	switch Glyphs(os.Getenv(glyphsEnv)) {
	case GlyphsOctants:
		return GlyphsOctants
	case GlyphsHalfBlocks:
		return GlyphsHalfBlocks
	}
	for _, value := range []string{os.Getenv("TERM_PROGRAM"), os.Getenv("TERM")} {
		value = strings.ToLower(value)
		for _, terminal := range octantTerminals {
			if strings.Contains(value, terminal) {
				return GlyphsOctants
			}
		}
	}
	return GlyphsHalfBlocks
}

// glyphs is the character set detected once at start-up.
var glyphs = DetectGlyphs()

// SetGlyphs forces the color logo's character set; fixtures pin it so their
// output never depends on the terminal running the tests.
func SetGlyphs(g Glyphs) { glyphs = g }

// Logo draws the largest variant of the brand mark that fits in width×height
// cells, or "" when none does. Mono terminals get the Braille mark, whose
// shape survives the loss of color; color terminals get octants or
// half-blocks, as DetectGlyphs decided.
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
