//go:build linux

package styles

import (
	"bytes"
	"fmt"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal pair, so ProbeGlyphs runs against a real
// terminal device whose other end the test drives.
func openPTY(t *testing.T) (controller, terminal *os.File) {
	t.Helper()
	controller, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no pseudo-terminals here: %v", err)
	}
	t.Cleanup(func() { _ = controller.Close() })
	fd := int(controller.Fd())
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlock pty: %v", err)
	}
	number, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("pty number: %v", err)
	}
	terminal, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", number), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("open pty terminal: %v", err)
	}
	t.Cleanup(func() { _ = terminal.Close() })
	return controller, terminal
}

// answerProbe plays the terminal: it waits for the cursor query and answers
// with report, or never answers when report is empty.
func answerProbe(controller *os.File, report string) <-chan string {
	written := make(chan string, 1)
	go func() {
		var seen bytes.Buffer
		chunk := make([]byte, 64)
		for !bytes.Contains(seen.Bytes(), []byte("\x1b[6n")) {
			n, err := controller.Read(chunk)
			if err != nil {
				written <- seen.String()
				return
			}
			seen.Write(chunk[:n])
		}
		if report != "" {
			_, _ = controller.WriteString(report)
		}
		written <- seen.String()
	}()
	return written
}

func TestProbeGlyphsMeasuresARealTerminal(t *testing.T) {
	t.Setenv(glyphsEnv, "")
	for _, check := range []struct {
		name, report string
		want         Glyphs
	}{
		{"one cell advance", "\x1b[1;2R", GlyphsOctants},
		{"wide advance", "\x1b[1;3R", GlyphsHalfBlocks},
		{"no answer", "", GlyphsHalfBlocks},
	} {
		controller, terminal := openPTY(t)
		written := answerProbe(controller, check.report)
		if got := ProbeGlyphs(terminal, terminal); got != check.want {
			t.Errorf("%s: ProbeGlyphs() = %q, want %q", check.name, got, check.want)
		}
		if check.report != "" {
			if probe := <-written; !bytes.Contains([]byte(probe), []byte("\U0001CD00")) {
				t.Errorf("%s: terminal received %q, want the octant probe", check.name, probe)
			}
		}
	}
}
