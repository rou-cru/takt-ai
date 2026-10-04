//go:build linux

package styles

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// openPTY opens a pseudo-terminal pair, so ProbeLogoMode runs against a real
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

// answerProbe plays the terminal: it waits for the device-attributes query
// and answers with reply, or never answers when reply is empty.
func answerProbe(controller *os.File, reply string) <-chan string {
	written := make(chan string, 1)
	go func() {
		var seen bytes.Buffer
		chunk := make([]byte, 64)
		for !bytes.HasSuffix(seen.Bytes(), []byte("\x1b[c")) {
			n, err := controller.Read(chunk)
			if err != nil {
				written <- seen.String()
				return
			}
			seen.Write(chunk[:n])
		}
		if reply != "" {
			_, _ = controller.WriteString(reply)
		}
		written <- seen.String()
	}()
	return written
}

func TestProbeLogoModeMeasuresARealTerminal(t *testing.T) {
	t.Setenv(logoModeEnv, "")
	for _, check := range []struct {
		name, reply string
		want        LogoMode
	}{
		{"graphics accepted", "\x1b_Gi=31;OK\x1b\\\x1b[?62c", ModeImage},
		{"device attributes only", "\x1b[?62c", ModeQuadrants},
		{"no answer", "", ModeQuadrants},
	} {
		controller, terminal := openPTY(t)
		written := answerProbe(controller, check.reply)
		if got := ProbeLogoMode(terminal, terminal); got != check.want {
			t.Errorf("%s: ProbeLogoMode() = %q, want %q", check.name, got, check.want)
		}
		if check.reply != "" {
			if probe := <-written; !strings.Contains(probe, graphicsProbe) {
				t.Errorf("%s: terminal received %q, want the graphics probe", check.name, probe)
			}
		}
	}
}
