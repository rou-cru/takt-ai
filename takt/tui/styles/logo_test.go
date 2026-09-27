package styles

import (
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

func TestRenderLogoUsesGeneratedAsset(t *testing.T) {
	originalBlocks, originalBraille := generatedLogoBlocks, generatedLogo
	t.Cleanup(func() {
		generatedLogoBlocks, generatedLogo = originalBlocks, originalBraille
		theme.SetMode(theme.DetectMode())
	})
	theme.SetMode(theme.ModeColor)
	generatedLogoBlocks = [][]logoSpan{{{Text: "▀", Color: "#010203", Bg: "#040506"}}}
	generatedLogo = [][]logoSpan{{{Text: "⣿", Color: "#050607"}}}

	want := lipgloss.NewStyle().Foreground(lipgloss.Color("#010203")).Background(lipgloss.Color("#040506")).Render("▀")
	if got := RenderLogo(); got != want {
		t.Fatalf("RenderLogo() = %q, want generated asset rendering %q", got, want)
	}
}

func TestRenderLogoKeepsBrailleMarkInMono(t *testing.T) {
	originalBlocks, originalBraille := generatedLogoBlocks, generatedLogo
	t.Cleanup(func() {
		generatedLogoBlocks, generatedLogo = originalBlocks, originalBraille
		theme.SetMode(theme.DetectMode())
	})
	theme.SetMode(theme.ModeMono)
	generatedLogoBlocks = [][]logoSpan{{{Text: "▀", Color: "#010203", Bg: "#040506"}}}
	generatedLogo = [][]logoSpan{{{Text: "⣿", Color: "#050607"}}}

	want := lipgloss.NewStyle().Foreground(lipgloss.Color("#050607")).Render("⣿")
	if got := RenderLogo(); got != want {
		t.Fatalf("RenderLogo() = %q, want Braille mark rendering %q", got, want)
	}
}
