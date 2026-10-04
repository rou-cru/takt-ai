package tui

import (
	"bytes"
	"os"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/styles"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

// Only real file streams are measured; the override decides here because a
// test has no terminal to answer.
func TestMeasureLogoGlyphsUsesFileStreamsOnly(t *testing.T) {
	t.Cleanup(func() { styles.SetGlyphs(styles.GlyphsHalfBlocks); theme.SetMode(theme.DetectMode()) })
	theme.SetMode(theme.ModeColor)
	t.Setenv("TAKT_LOGO_GLYPHS", "octants")

	// A run that cannot probe drops what an earlier run measured.
	styles.SetGlyphs(styles.GlyphsOctants)
	measureLogoGlyphs(&bytes.Buffer{}, &bytes.Buffer{})
	if got := styles.Logo(16, 8); got == "" || !bytes.ContainsRune([]byte(got), '▀') {
		t.Fatalf("non-file streams changed the glyphs: %q", got)
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devNull.Close() })
	measureLogoGlyphs(devNull, devNull)
	if got := styles.Logo(16, 8); !containsOctant(got) {
		t.Fatalf("file streams with the octant override = %q, want octants", got)
	}
}

func containsOctant(s string) bool {
	for _, r := range s {
		if r >= 0x1CD00 && r <= 0x1CDE5 {
			return true
		}
	}
	return false
}
