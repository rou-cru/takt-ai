package styles

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

// blockingReader never answers, like a terminal that ignores the queries.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) { select {} }

// da1 is a device-attributes reply, which every terminal sends.
const da1 = "\x1b[?62;22c"

func TestMeasureLogoModeReadsTheGraphicsReply(t *testing.T) {
	for _, check := range []struct {
		name   string
		answer io.Reader
		want   LogoMode
	}{
		{"graphics accepted", strings.NewReader("\x1b_Gi=31;OK\x1b\\" + da1), ModeImage},
		{"typed input before the replies", strings.NewReader("q\x1b_Gi=31;OK\x1b\\" + da1), ModeImage},
		{"graphics refused", strings.NewReader("\x1b_Gi=31;ENOTSUPPORTED:no\x1b\\" + da1), ModeQuadrants},
		{"device attributes only", strings.NewReader(da1), ModeQuadrants},
		{"graphics accepted, no device attributes", strings.NewReader("\x1b_Gi=31;OK\x1b\\"), ModeQuadrants},
		{"garbage", strings.NewReader("ccc\x1b[12R"), ModeQuadrants},
		{"closed input", strings.NewReader(""), ModeQuadrants},
		{"no answer", blockingReader{}, ModeQuadrants},
	} {
		var written bytes.Buffer
		if got, _ := probeGraphics(&written, check.answer, 20*time.Millisecond); got != check.want {
			t.Errorf("%s: probeGraphics() = %q, want %q", check.name, got, check.want)
		}
		if written.String() != graphicsProbe {
			t.Errorf("%s: wrote %q, want the probe %q", check.name, written.String(), graphicsProbe)
		}
	}
}

// failingWriter refuses the probe, like a closed terminal.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestProbeGraphicsKeepsQuadrantsWhenTheProbeCannotBeWritten(t *testing.T) {
	if got, _ := probeGraphics(failingWriter{}, strings.NewReader("\x1b_Gi=31;OK\x1b\\"+da1), 20*time.Millisecond); got != ModeQuadrants {
		t.Errorf("probeGraphics() with a failed write = %q, want quadrants", got)
	}
}

// The override wins over measuring, and a terminal-less run never probes.
func TestProbeLogoModeOverrideAndNonTerminal(t *testing.T) {
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devNull.Close() })

	t.Setenv(logoModeEnv, "image")
	if got := ProbeLogoMode(devNull, devNull); got != ModeImage {
		t.Errorf("ProbeLogoMode() with override = %q, want image", got)
	}
	t.Setenv(logoModeEnv, "bogus")
	if got := ProbeLogoMode(devNull, devNull); got != ModeQuadrants {
		t.Errorf("ProbeLogoMode() on a non-terminal = %q, want quadrants", got)
	}
}
