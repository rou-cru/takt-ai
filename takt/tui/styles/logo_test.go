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
	braille, octants, half, detected := generatedLogoBraille, generatedLogoOctants, generatedLogoHalfBlocks, glyphs
	t.Cleanup(func() {
		generatedLogoBraille, generatedLogoOctants, generatedLogoHalfBlocks, glyphs = braille, octants, half, detected
		theme.SetMode(theme.DetectMode())
	})
	variants := func(text, color, bg string) [][][]logoSpan {
		return [][][]logoSpan{
			{{{Text: strings.Repeat(text, 4), Color: color, Bg: bg}}, {{Text: strings.Repeat(text, 4), Color: color, Bg: bg}}},
			{{{Text: strings.Repeat(text, 2), Color: color, Bg: bg}}},
		}
	}
	generatedLogoBraille = variants("⣿", "", "")
	generatedLogoOctants = variants("\U0001CD00", "#010203", "#040506")
	generatedLogoHalfBlocks = variants("▀", "#070809", "#0a0b0c")
}

func TestLogoDrawsTheLargestVariantThatFits(t *testing.T) {
	stubLogos(t)
	theme.SetMode(theme.ModeColor)
	glyphs = GlyphsHalfBlocks
	cell := lipgloss.NewStyle().Foreground(lipgloss.Color("#070809")).Background(lipgloss.Color("#0a0b0c"))

	if got, want := Logo(80, 24), cell.Render("▀▀▀▀")+"\n"+cell.Render("▀▀▀▀"); got != want {
		t.Errorf("Logo(80, 24) = %q, want the full variant %q", got, want)
	}
	if got, want := Logo(3, 24), cell.Render("▀▀"); got != want {
		t.Errorf("Logo(3, 24) = %q, want the small variant %q", got, want)
	}
	if got := Logo(1, 1); got != "" {
		t.Errorf("Logo(1, 1) = %q, want no logo", got)
	}
}

func TestLogoUsesOctantsOnlyWhenDetected(t *testing.T) {
	stubLogos(t)
	theme.SetMode(theme.ModeColor)
	glyphs = GlyphsOctants
	want := lipgloss.NewStyle().Foreground(lipgloss.Color("#010203")).Background(lipgloss.Color("#040506")).Render("\U0001CD00\U0001CD00")
	if got := Logo(2, 1); got != want {
		t.Errorf("Logo() with octants = %q, want %q", got, want)
	}
}

func TestLogoKeepsBrailleMarkInMono(t *testing.T) {
	stubLogos(t)
	theme.SetMode(theme.ModeMono)
	glyphs = GlyphsOctants
	// Monochrome emits no ANSI: the Braille shape alone carries the mark.
	if got := Logo(2, 1); got != "⣿⣿" {
		t.Errorf("Logo() in mono = %q, want the plain Braille mark", got)
	}
}

func TestDetectGlyphs(t *testing.T) {
	for _, check := range []struct {
		override, program, term string
		want                    Glyphs
	}{
		{"", "", "xterm-256color", GlyphsHalfBlocks},
		{"", "tmux", "screen-256color", GlyphsHalfBlocks},
		{"", "ghostty", "xterm-ghostty", GlyphsOctants},
		{"", "", "xterm-kitty", GlyphsOctants},
		{"", "", "foot", GlyphsOctants},
		{"halfblocks", "ghostty", "xterm-ghostty", GlyphsHalfBlocks},
		{"octants", "Apple_Terminal", "xterm-256color", GlyphsOctants},
		{"bogus", "", "xterm-256color", GlyphsHalfBlocks},
	} {
		t.Setenv(glyphsEnv, check.override)
		t.Setenv("TERM_PROGRAM", check.program)
		t.Setenv("TERM", check.term)
		if got := DetectGlyphs(); got != check.want {
			t.Errorf("DetectGlyphs(%q, %q, %q) = %q, want %q", check.override, check.program, check.term, got, check.want)
		}
	}
}

// Every generated variant is rectangular and twice as wide as tall, so the
// ring stays round and the home can center the mark.
func TestGeneratedVariantsAreRoundAndRectangular(t *testing.T) {
	for name, variants := range map[string][][][]logoSpan{"braille": generatedLogoBraille, "octants": generatedLogoOctants, "halfblocks": generatedLogoHalfBlocks} {
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
