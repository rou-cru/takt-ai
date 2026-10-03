package uninstall_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/uninstall"
)

var (
	enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}
	tabKey   = tea.KeyPressMsg{Code: tea.KeyTab}
)

func viewText(m uninstall.Model) string {
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view, _ := sized.(uninstall.Model)
	return ansi.Strip(view.View().Content)
}

// installedRootWithModifiedFile installs a real root then edits one
// Takt-managed file, so the uninstall preview finds it as user-edited and
// StateModified asks what to do with it.
func installedRootWithModifiedFile(t *testing.T) (string, string) {
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
	var edited string
	for path := range manifest.Entries {
		edited = path
		break
	}
	if edited == "" {
		t.Fatal("installed manifest has no managed entries to edit")
	}
	full := filepath.Join(root, filepath.FromSlash(edited))
	content, err := os.ReadFile(full)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", full, err)
	}
	if err := os.WriteFile(full, append(content, []byte("\n# tampered by test\n")...), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", full, err)
	}
	return root, edited
}

func TestUninstallFullFlowKeepModifiedFile(t *testing.T) {
	root, edited := installedRootWithModifiedFile(t)
	m := uninstall.New(root)
	if m.State() != uninstall.StateModified {
		t.Fatalf("State() = %v, want StateModified", m.State())
	}
	if got := viewText(m); !strings.Contains(got, edited) {
		t.Fatalf("modified View() = %q, want it to list %q", got, edited)
	}

	// Decide the one modified file: Enter on row 0 (Keep mine).
	next, _ := m.Update(enterKey)
	m = next.(uninstall.Model)
	if !m.Dirty() {
		t.Fatal("Dirty() after deciding a retention choice = false, want true")
	}

	// The only file is decided, so the flow advances to the Engram question.
	if m.State() != uninstall.StateEngram {
		t.Fatalf("State() after deciding the modified file = %v, want StateEngram", m.State())
	}

	// Leave is preselected: Enter chooses it and advances to review.
	next, _ = m.Update(enterKey)
	m = next.(uninstall.Model)
	if m.State() != uninstall.StateReview {
		t.Fatalf("State() after the Engram question = %v, want StateReview", m.State())
	}
	if got := viewText(m); !strings.Contains(got, "Uninstall") {
		t.Errorf("review View() = %q, want it to mention the uninstall action", got)
	}

	// Focus starts on Back; the destructive action needs a deliberate move.
	moved, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = moved.(uninstall.Model)
	restoring, cmd := m.Update(enterKey)
	m = restoring.(uninstall.Model)
	if cmd == nil {
		t.Fatal("confirming the uninstall produced no command")
	}
	if !m.Run().Busy() {
		t.Fatal("model is not busy after starting the uninstall action")
	}

	requestMsg := cmd()
	request, ok := requestMsg.(runtime.ActionRequest)
	if !ok {
		t.Fatalf("uninstall command message = %#v, want runtime.ActionRequest", requestMsg)
	}
	if request.Action != runtime.ActionUninstall {
		t.Fatalf("request.Action = %v, want ActionUninstall", request.Action)
	}
	if len(request.RetainPaths) != 1 || request.RetainPaths[0] != edited {
		t.Fatalf("request.RetainPaths = %v, want [%q] (kept)", request.RetainPaths, edited)
	}
	if request.EngramChoice != lifecycle.EngramLeave {
		t.Fatalf("request.EngramChoice = %q, want %q", request.EngramChoice, lifecycle.EngramLeave)
	}

	result, err := (runtime.Adapter{}).Execute(request)
	if err != nil {
		t.Fatalf("Adapter.Execute() error = %v", err)
	}
	resultMsg := runtime.ActionResultMsg{Request: request, Result: result, Err: err}
	next, _ = m.Update(resultMsg)
	m = next.(uninstall.Model)
	if m.State() != uninstall.StateResult {
		t.Fatalf("State() after the result = %v, want StateResult", m.State())
	}
	if m.Run().Busy() {
		t.Error("model is still busy after the result was consumed")
	}
	if got := viewText(m); got == "" {
		t.Error("result View() is empty")
	}
	if setup.IsInstalled(root) {
		t.Error("IsInstalled() after a completed uninstall = true, want false")
	}
}

func TestUninstallReviewBackReturnsToEngram(t *testing.T) {
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("SaveInstalledConfig() error = %v", err)
	}
	m := uninstall.New(root)
	if m.State() != uninstall.StateEngram {
		t.Fatalf("State() with nothing on disk = %v, want StateEngram (no modified files to ask about)", m.State())
	}

	next, _ := m.Update(tabKey)
	m = next.(uninstall.Model)
	next, _ = m.Update(enterKey)
	m = next.(uninstall.Model)
	if m.State() != uninstall.StateReview {
		t.Fatalf("State() = %v, want StateReview", m.State())
	}

	// Footer starts on actionBack, never on the destructive action.
	next, cmd := m.Update(enterKey)
	m = next.(uninstall.Model)
	if cmd != nil {
		t.Error("Back from review returned a command, want a plain state change")
	}
	if m.State() != uninstall.StateEngram {
		t.Fatalf("State() after Back from review = %v, want StateEngram", m.State())
	}
}

func TestUninstallNotInstalledGuard(t *testing.T) {
	m := uninstall.New(t.TempDir())
	if m.State() != uninstall.StateNotInstalled {
		t.Fatalf("State() on a fresh root = %v, want StateNotInstalled", m.State())
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("Esc on the not-installed screen produced no command")
	}
}
