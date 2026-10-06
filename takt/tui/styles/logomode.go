package styles

import (
	"bytes"
	"io"
	"os"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/muesli/cancelreader"
)

// LogoMode is how the color logo is drawn.
type LogoMode string

const (
	// ModeImage draws the logo as an image with the kitty graphics protocol:
	// the mark itself, at the terminal's full resolution.
	ModeImage LogoMode = "image"
	// ModeQuadrants draws 2×2 pixels per cell with the quadrant block
	// characters, which every terminal font has.
	ModeQuadrants LogoMode = "quadrants"
)

// logoModeEnv overrides the measurement: "image" or "quadrants".
const logoModeEnv = "TAKT_LOGO"

// probeTimeout bounds the wait for a terminal that answers nothing; one that
// answers the device-attributes query ends the wait itself.
const probeTimeout = 150 * time.Millisecond

// graphicsProbe asks whether the terminal takes kitty graphics, with a 1×1
// query that stores nothing, then asks for its primary device attributes
// (DA1). Every terminal answers DA1 and answers in order, so its reply
// closes the probe: a graphics reply comes before it or not at all.
const graphicsProbe = "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\\x1b[c"

// graphicsAccepted is the start of the reply to graphicsProbe's query from a
// terminal that draws kitty graphics.
const graphicsAccepted = "\x1b_Gi=31;OK"

// deviceAttributesPrefix starts the DA1 reply, which ends with 'c'.
const deviceAttributesPrefix = "\x1b[?"

// probeClear erases the probe line before the TUI takes the screen, should a
// terminal print any of the queries.
const probeClear = "\r\x1b[2K"

// replyMaxBytes bounds how much input is read while waiting for the replies.
const replyMaxBytes = 256

// ProbeLogoMode picks how the color logo is drawn on the terminal on in and
// out. TAKT_LOGO wins; otherwise the terminal is asked, never identified by
// name: an image only when it accepts kitty graphics, quadrants when it does
// not, does not answer, or is not a terminal.
func ProbeLogoMode(in, out *os.File) LogoMode {
	if mode, ok := logoModeOverride(); ok {
		return mode
	}
	if !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd()) {
		return ModeQuadrants
	}
	state, err := term.MakeRaw(in.Fd())
	if err != nil {
		return ModeQuadrants
	}
	defer func() { _ = term.Restore(in.Fd(), state) }()
	// A cancelable reader, so the read left waiting after a timeout never
	// steals input from the TUI that starts next.
	reader, err := cancelreader.NewReader(in)
	if err != nil {
		return ModeQuadrants
	}
	mode, finished := probeGraphics(out, reader, probeTimeout)
	reader.Cancel()
	// The read has returned before the reader closes, or it would race the close.
	select {
	case <-finished:
	case <-time.After(probeTimeout):
	}
	_ = reader.Close()
	_, _ = io.WriteString(out, probeClear)
	return mode
}

// logoModeOverride reads TAKT_LOGO; unknown values are ignored.
func logoModeOverride() (LogoMode, bool) {
	switch mode := LogoMode(os.Getenv(logoModeEnv)); mode {
	case ModeImage, ModeQuadrants:
		return mode, true
	}
	return "", false
}

// probeGraphics writes the probe to w and waits up to timeout for the replies
// on r. It also reports when its read goroutine has returned, so a caller can
// cancel the reader and wait before closing it.
func probeGraphics(w io.Writer, r io.Reader, timeout time.Duration) (LogoMode, <-chan struct{}) {
	finished := make(chan struct{})
	if _, err := io.WriteString(w, graphicsProbe); err != nil {
		close(finished)
		return ModeQuadrants, finished
	}
	accepted := make(chan bool, 1)
	go func() {
		defer close(finished)
		accepted <- readGraphicsReply(r)
	}()
	select {
	case ok := <-accepted:
		if ok {
			return ModeImage, finished
		}
	case <-time.After(timeout):
	}
	return ModeQuadrants, finished
}

// readGraphicsReply reads up to the end of the DA1 reply and reports whether
// the graphics query was accepted before it.
func readGraphicsReply(r io.Reader) bool {
	var input []byte
	next := make([]byte, 1)
	for len(input) < replyMaxBytes {
		if n, err := r.Read(next); err != nil || n == 0 {
			return false
		}
		input = append(input, next[0])
		if next[0] == 'c' && bytes.Contains(input, []byte(deviceAttributesPrefix)) {
			return bytes.Contains(input, []byte(graphicsAccepted))
		}
	}
	return false
}
