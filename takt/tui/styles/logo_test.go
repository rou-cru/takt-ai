package styles

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// stubLogos swaps the generated datasets for small known variants: a 4×2 and
// a 2×1 of each character set.
func stubLogos(t *testing.T) {
	t.Helper()
	braille, quadrants, measured := generatedLogoBraille, generatedLogoQuadrants, mode
	t.Cleanup(func() {
		generatedLogoBraille, generatedLogoQuadrants, mode = braille, quadrants, measured
		theme.SetMode(theme.DetectMode())
	})
	variants := func(text, color, bg string) [][][]logoSpan {
		return [][][]logoSpan{
			{{{Text: strings.Repeat(text, 4), Color: color, Bg: bg}}, {{Text: strings.Repeat(text, 4), Color: color, Bg: bg}}},
			{{{Text: strings.Repeat(text, 2), Color: color, Bg: bg}}},
		}
	}
	generatedLogoBraille = variants("⣿", "", "")
	generatedLogoQuadrants = variants("▚", "#070809", "#0a0b0c")
}

func TestLogoDrawsTheLargestVariantThatFits(t *testing.T) {
	stubLogos(t)
	theme.SetMode(theme.ModeColor)
	mode = ModeQuadrants
	cell := lipgloss.NewStyle().Foreground(lipgloss.Color("#070809")).Background(lipgloss.Color("#0a0b0c"))

	if got, want := Logo(80, 24), cell.Render("▚▚▚▚")+"\n"+cell.Render("▚▚▚▚"); got != want {
		t.Errorf("Logo(80, 24) = %q, want the full variant %q", got, want)
	}
	if got, want := Logo(3, 24), cell.Render("▚▚"); got != want {
		t.Errorf("Logo(3, 24) = %q, want the small variant %q", got, want)
	}
	if got := Logo(1, 1); got != "" {
		t.Errorf("Logo(1, 1) = %q, want no logo", got)
	}
}

// As an image, the logo reserves the cells of the variant that fits, for the
// caller to place the image over.
func TestLogoReservesCellsForTheImage(t *testing.T) {
	stubLogos(t)
	theme.SetMode(theme.ModeColor)
	mode = ModeImage
	if !ImageLogo() {
		t.Fatal("ImageLogo() = false in image mode")
	}
	if got, want := Logo(80, 24), "\u2800\u2800\u2800\u2800\n\u2800\u2800\u2800\u2800"; got != want {
		t.Errorf("Logo(80, 24) = %q, want a 4×2 reserved block %q", got, want)
	}
	if got := Logo(3, 24); got != "\u2800\u2800" {
		t.Errorf("Logo(3, 24) = %q, want a 2×1 reserved block", got)
	}
}

func TestLogoKeepsBrailleMarkInMono(t *testing.T) {
	stubLogos(t)
	theme.SetMode(theme.ModeMono)
	mode = ModeImage
	if ImageLogo() {
		t.Error("ImageLogo() = true in mono")
	}
	// Monochrome emits no ANSI: the Braille shape alone carries the mark.
	if got := Logo(2, 1); got != "⣿⣿" {
		t.Errorf("Logo() in mono = %q, want the plain Braille mark", got)
	}
}

// Every generated variant is rectangular and twice as wide as tall, so the
// ring stays round and the home can center the mark.
func TestGeneratedVariantsAreRoundAndRectangular(t *testing.T) {
	for name, variants := range map[string][][][]logoSpan{"braille": generatedLogoBraille, "quadrants": generatedLogoQuadrants} {
		if len(variants) == 0 {
			t.Fatalf("%s: no variants", name)
		}
		for _, variant := range variants {
			width := variantWidth(variant)
			if width != 2*len(variant) {
				t.Errorf("%s: %d rows are %d cells wide, want %d", name, len(variant), width, 2*len(variant))
			}
			for row, line := range variant {
				if got := variantWidth([][]logoSpan{line}); got != width {
					t.Errorf("%s %d rows: row %d is %d cells, want %d", name, len(variant), row, got, width)
				}
			}
		}
	}
}
