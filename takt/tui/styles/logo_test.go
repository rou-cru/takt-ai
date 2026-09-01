package styles

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestRenderLogoUsesGeneratedAsset(t *testing.T) {
	original := generatedLogo
	t.Cleanup(func() { generatedLogo = original })
	generatedLogo = [][]logoSpan{{{Text: "⣿", Color: "#010203"}}}

	want := lipgloss.NewStyle().Foreground(lipgloss.Color("#010203")).Render("⣿")
	if got := RenderLogo(); got != want {
		t.Fatalf("RenderLogo() = %q, want generated asset rendering %q", got, want)
	}
}

func TestGeneratedLogoContainsColoredBrailleBranding(t *testing.T) {
	var glyphs strings.Builder
	colored := false
	for _, line := range generatedLogo {
		for _, span := range line {
			glyphs.WriteString(span.Text)
			colored = colored || span.Color != ""
		}
	}
	if len(generatedLogo) < 2 || !colored || !strings.ContainsRune(glyphs.String(), '⣿') {
		t.Fatal("generated logo is not the expected colored Braille branding asset")
	}
}
