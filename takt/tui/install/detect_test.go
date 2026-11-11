package install_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/tui/install"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// TestMain makes the suite hermetic: RunWithFakeTools puts a fake opencode first on
// PATH, gives the binary a private memory directory, and serves `memory serve` when the
// install flow launches this binary as the memory owner.
func TestMain(m *testing.M) {
	os.Exit(testutil.RunWithFakeTools(m))
}

// withoutOpenCode puts only the fake codegraph on PATH, so OpenCode is absent while the
// install flow's other tool lookups still resolve.
func withoutOpenCode(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codegraph"), testutil.FakeCodegraphScript(codegraph.CodegraphVersion), 0o755); err != nil {
		t.Fatalf("WriteFile(codegraph) error = %v", err)
	}
	t.Setenv("PATH", dir)
}

func openCodeNoticeShown(t *testing.T) bool {
	t.Helper()
	m := install.New(t.TempDir())
	if m.Step() != install.StepReview {
		t.Fatalf("step = %v", m.Step())
	}
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 80})
	return strings.Contains(ansi.Strip(sized.View().Content), ui.TextOpenCodeNotFound)
}

// A missing OpenCode binary is a notice on the first screen, never a block:
// the flow still opens on the review of the full install scope.
func TestMissingOpenCodeNoticesWithoutBlocking(t *testing.T) {
	withoutOpenCode(t)
	if !openCodeNoticeShown(t) {
		t.Fatal("notice not shown")
	}
}

func TestDetectedOpenCodeLeavesNoNotice(t *testing.T) {
	if openCodeNoticeShown(t) {
		t.Fatal("notice shown with opencode on PATH")
	}
}
