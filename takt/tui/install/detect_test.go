package install

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// TestMain makes the suite hermetic: the OpenCode binary may exist on the
// developer's PATH, but tests that assume a fresh start need it undetected.
func TestMain(m *testing.M) {
	original := lookPath
	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	code := m.Run()
	lookPath = original
	os.Exit(code)
}

// stubLookPath fakes which binaries exist on PATH.
func stubLookPath(t *testing.T, found ...string) {
	t.Helper()
	original := lookPath
	lookPath = func(program string) (string, error) {
		if slices.Contains(found, program) {
			return "/bin/" + program, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = original })
}

// A missing OpenCode binary is a notice on the first screen, never a block:
// the flow still starts on the setup choice with the full install scope.
func TestMissingOpenCodeNoticesWithoutBlocking(t *testing.T) {
	stubLookPath(t)
	m := New(t.TempDir())
	if m.Step() != StepSetupChoice {
		t.Fatalf("step = %v", m.Step())
	}
	if m.notice != ui.TextOpenCodeNotFound {
		t.Fatalf("notice = %q", m.notice)
	}
	m.width, m.height = 200, 80
	if body := ansi.Strip(m.View().Content); !strings.Contains(body, ui.TextOpenCodeNotFound) {
		t.Fatalf("notice not shown:\n%s", body)
	}
}

func TestDetectedOpenCodeLeavesNoNotice(t *testing.T) {
	stubLookPath(t, "opencode")
	if m := New(t.TempDir()); m.notice != "" {
		t.Fatalf("notice = %q, want none", m.notice)
	}
}
