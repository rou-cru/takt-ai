// Package styles renders the Takt brand logo for the TUI from generated data.
package styles

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

//go:generate go run ../../../development/generate-logo -input ../../../docs/assets/brand/takt-ai.png -output logo_generated.go -blocks-output logo_blocks_generated.go -image-output logo.png -installer ../../../install.sh

// logoSpan holds one colored text run so logo rows stay compact. The type
// lives here, hand-written, because both generated datasets share it.
type logoSpan struct {
	Text  string
	Color string
	Bg    string
}

// mode is how the color logo is drawn: quadrants until Run measures the
// terminal with ProbeLogoMode.
var mode = ModeQuadrants

// SetLogoMode sets how the color logo is drawn: Run stores what
// ProbeLogoMode measured, and fixtures pin quadrants.
func SetLogoMode(m LogoMode) { mode = m }

// ImageLogo reports whether the logo is drawn as an image, which its caller
// then places over the cells Logo reserves.
func ImageLogo() bool { return mode == ModeImage && !theme.Mono() }

// LogoCell fills the cells Logo reserves for the image: a blank Braille
// pattern, blank in every font, that nothing else on the home draws.
const LogoCell = '\u2800'

// Logo draws the largest brand-mark variant that fits in width×height cells,
// or "" when none does. Mono terminals get Braille, whose shape survives the
// loss of color; color terminals get quadrants, or a LogoCell block when the
// logo is an image.
func Logo(width, height int) string {
	variants := generatedLogoQuadrants
	if theme.Mono() {
		variants = generatedLogoBraille
	}
	for _, variant := range variants {
		if len(variant) <= height && variantWidth(variant) <= width {
			if ImageLogo() {
				return reserve(variantWidth(variant), len(variant))
			}
			return render(variant)
		}
	}
	return ""
}

// reserve is a cols×rows block of LogoCell.
func reserve(cols, rows int) string {
	row := strings.Repeat(string(LogoCell), cols)
	return strings.TrimSuffix(strings.Repeat(row+"\n", rows), "\n")
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
