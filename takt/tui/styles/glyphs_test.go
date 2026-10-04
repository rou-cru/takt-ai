package styles

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// blockingReader never answers, like a terminal that ignores the query.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }

func TestMeasureGlyphsReadsTheCursorReport(t *testing.T) {
	for _, check := range []struct {
		name   string
		answer io.Reader
		want   Glyphs
	}{
		{"one cell advance", strings.NewReader("\x1b[12;2R"), GlyphsOctants},
		{"typed input before the report", strings.NewReader("q\x1b[3;2R"), GlyphsOctants},
		{"wide advance", strings.NewReader("\x1b[12;3R"), GlyphsHalfBlocks},
		{"no advance", strings.NewReader("\x1b[12;1R"), GlyphsHalfBlocks},
		{"garbage", strings.NewReader("\x1b[12R"), GlyphsHalfBlocks},
		{"closed input", strings.NewReader(""), GlyphsHalfBlocks},
		{"no answer", blockingReader{}, GlyphsHalfBlocks},
	} {
		var written bytes.Buffer
		if got := measureGlyphs(&written, check.answer, 20*time.Millisecond); got != check.want {
			t.Errorf("%s: measureGlyphs() = %q, want %q", check.name, got, check.want)
		}
		if written.String() != octantProbe {
			t.Errorf("%s: wrote %q, want the probe %q", check.name, written.String(), octantProbe)
		}
	}
}

// The override wins over measuring, and a terminal-less run never probes.
func TestProbeGlyphsOverrideAndNonTerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devNull.Close() })

	t.Setenv(glyphsEnv, "octants")
	if got := ProbeGlyphs(devNull, devNull); got != GlyphsOctants {
		t.Errorf("ProbeGlyphs() with override = %q, want octants", got)
	}
	t.Setenv(glyphsEnv, "bogus")
	if got := ProbeGlyphs(devNull, devNull); got != GlyphsHalfBlocks {
		t.Errorf("ProbeGlyphs() on a non-terminal = %q, want half-blocks", got)
	}
}
