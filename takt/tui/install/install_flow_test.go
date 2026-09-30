package install_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/install"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

var (
	enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}
)

func viewText(m install.Model) string {
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view, _ := sized.(install.Model)
	return ansi.Strip(view.View().Content)
}

// nonJSONManagedPath installs a real root once, purely to learn a managed
// relative path that isn't a mergeable .json config, then discards the root.
func nonJSONManagedPath(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("LoadOwnershipManifest() error = %v", err)
	}
	for path := range manifest.Entries {
		if !strings.HasSuffix(path, ".json") {
			return path
		}
	}
	t.Fatal("no non-JSON managed path found in the installed manifest")
	return ""
}

func TestInstallConflictFlow(t *testing.T) {
	dir, err := os.MkdirTemp("", "takt-install-fake-bin-")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.WriteFile(filepath.Join(dir, "engram"), testutil.FakeEngramScript("dev"), 0o755); err != nil {
		t.Fatalf("WriteFile(engram) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode"), testutil.FakeOpenCodeScript(), 0o755); err != nil {
		t.Fatalf("WriteFile(opencode) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "codegraph"), testutil.FakeCodegraphScript(codegraph.CodegraphVersion), 0o755); err != nil {
		t.Fatalf("WriteFile(codegraph) error = %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	conflictPath := nonJSONManagedPath(t)

	root := t.TempDir()
	full := filepath.Join(root, filepath.FromSlash(conflictPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(full, []byte("pre-existing user content that will not match"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	m := install.New(root)
	if m.Step() != install.StepSetupChoice {
		t.Fatalf("Step() on a fresh root = %v, want StepSetupChoice", m.Step())
	}

	// Confirm the default (non-custom) setup, which prepares the plan and
	// should land on StepConflicts because of the pre-existing file.
	next, _ := m.Update(enterKey)
	m = next.(install.Model)
	if m.Step() != install.StepConflicts {
		t.Fatalf("Step() after confirming default setup = %v, want StepConflicts (preview error: check that %q is really a conflict)", m.Step(), conflictPath)
	}
	if got := viewText(m); !strings.Contains(got, conflictPath) {
		t.Fatalf("conflicts View() = %q, want it to name %q", got, conflictPath)
	}

	// Choose "restore Takt's version" for the one conflict (row index 1:
	// row 0 is keep, row 1 is restore, per conflictChoiceCount=2). Down
	// moves the cursor onto the restore row, Enter with body focus chooses
	// it, a further Down pushes focus onto the footer's single action, and
	// Enter there advances the step.
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(install.Model)
	next, _ = m.Update(enterKey)
	m = next.(install.Model)
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = next.(install.Model)
	next, _ = m.Update(enterKey)
	m = next.(install.Model)
	if m.Step() != install.StepReview {
		t.Fatalf("Step() after resolving the conflict = %v, want StepReview", m.Step())
	}

	// Footer starts on the commit action; confirm it.
	restoring, cmd := m.Update(enterKey)
	m = restoring.(install.Model)
	if cmd == nil {
		t.Fatal("confirming the review produced no command")
	}
	if !m.Run().Busy() {
		t.Fatal("model is not busy after starting the install action")
	}

	requestMsg := cmd()
	request, ok := requestMsg.(runtime.ActionRequest)
	if !ok {
		t.Fatalf("install command message = %#v, want runtime.ActionRequest", requestMsg)
	}
	if request.Action != runtime.ActionInstall {
		t.Fatalf("request.Action = %v, want ActionInstall", request.Action)
	}
	if contains(request.PreservePaths, conflictPath) {
		t.Fatalf("request.PreservePaths = %v, want it to exclude %q: choosing restore means Takt's version overwrites it, nothing is preserved", request.PreservePaths, conflictPath)
	}

	result, err := (runtime.Adapter{}).Execute(request)
	if err != nil {
		t.Fatalf("Adapter.Execute() error = %v", err)
	}
	resultMsg := runtime.ActionResultMsg{Request: request, Result: result, Err: err}
	next, _ = m.Update(resultMsg)
	m = next.(install.Model)
	if m.Step() != install.StepResult {
		t.Fatalf("Step() after the result = %v, want StepResult", m.Step())
	}
	if m.Run().Busy() {
		t.Error("model is still busy after the result was consumed")
	}
	if got := viewText(m); got == "" {
		t.Error("result View() is empty")
	}
	if !setup.IsInstalled(root) {
		t.Error("IsInstalled() after a completed install = false, want true")
	}
	restored, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(restored), "pre-existing user content") {
		t.Error("choosing restore did not overwrite the pre-existing content")
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
