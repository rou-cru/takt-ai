package styles

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"
)

// Glyphs is the character set the color logo is drawn with.
type Glyphs string

const (
	// GlyphsOctants draws 2×4 pixels per cell with the Unicode 16 block
	// octants: the sharpest mark.
	GlyphsOctants Glyphs = "octants"
	// GlyphsHalfBlocks draws 1×2 pixels per cell with ▀ ▄ █, which every
	// terminal font has.
	GlyphsHalfBlocks Glyphs = "halfblocks"
)

// glyphsEnv overrides the measurement: "octants" or "halfblocks". It is the
// way out for a font that draws a box of the right width instead of the
// octant, the one case no terminal query can see.
const glyphsEnv = "TAKT_LOGO_GLYPHS"

// probeTimeout bounds the wait for the terminal's cursor report; a terminal
// that never answers keeps the half-block logo.
const probeTimeout = 150 * time.Millisecond

// octantProbe returns to the first column, prints one octant (U+1CD00) and
// asks for the cursor position (CPR). A terminal whose character tables know
// Unicode 16 advances the cursor one cell and reports column 2.
const octantProbe = "\r\U0001CD00\x1b[6n"

// probeClear erases the probe line before the TUI takes the screen.
const probeClear = "\r\x1b[2K"

// cprMaxBytes bounds how much input is read while waiting for the report.
const cprMaxBytes = 32

// ProbeGlyphs picks the color logo's character set for the terminal on in
// and out. TAKT_LOGO_GLYPHS wins; otherwise the terminal is measured, never
// identified by name: octants only when it advances exactly one cell over
// one, half-blocks when it does not, does not answer, or is not a terminal.
func ProbeGlyphs(in, out *os.File) Glyphs {
	if glyphs, ok := glyphsOverride(); ok {
		return glyphs
	}
	if !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
		return GlyphsHalfBlocks
	}
	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return GlyphsHalfBlocks
	}
	defer func() { _ = term.Restore(in.Fd(), state) }()
	// A cancelable reader, so the read left waiting after a timeout never
	// steals input from the TUI that starts next.
	reader, err := cancelreader.NewReader(in)
	if err != nil {
		return GlyphsHalfBlocks
	}
	defer func() { _ = reader.Close() }()
	defer reader.Cancel()
	glyphs := measureGlyphs(out, reader, probeTimeout)
	_, _ = io.WriteString(out, probeClear)
	return glyphs
}

// glyphsOverride reads TAKT_LOGO_GLYPHS; unknown values are ignored.
func glyphsOverride() (Glyphs, bool) {
	switch glyphs := Glyphs(os.Getenv(glyphsEnv)); glyphs {
	case GlyphsOctants, GlyphsHalfBlocks:
		return glyphs, true
	}
	return "", false
}

// measureGlyphs writes the probe to w and waits up to timeout for the cursor
// report on r.
func measureGlyphs(w io.Writer, r io.Reader, timeout time.Duration) Glyphs {
	if _, err := io.WriteString(w, octantProbe); err != nil {
		return GlyphsHalfBlocks
	}
	column := make(chan int, 1)
	go func() { column <- readCursorColumn(r) }()
	select {
	case got := <-column:
		if got == 2 {
			return GlyphsOctants
		}
	case <-time.After(timeout):
	}
	return GlyphsHalfBlocks
}

// readCursorColumn reads up to the end of a cursor report (ESC [ row ; col R)
// and returns its column, or 0 when none arrives intact.
func readCursorColumn(r io.Reader) int {
	var input []byte
	next := make([]byte, 1)
	for len(input) < cprMaxBytes {
		if n, err := r.Read(next); err != nil || n == 0 {
			return 0
		}
		input = append(input, next[0])
		if next[0] == 'R' {
			break
		}
	}
	start := bytes.LastIndex(input, []byte("\x1b["))
	if start < 0 || input[len(input)-1] != 'R' {
		return 0
	}
	_, col, found := bytes.Cut(input[start+2:len(input)-1], []byte(";"))
	if !found {
		return 0
	}
	column, err := strconv.Atoi(string(col))
	if err != nil {
		return 0
	}
	return column
}
