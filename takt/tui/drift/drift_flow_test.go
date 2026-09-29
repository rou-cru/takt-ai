package drift_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/drift"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

var (
	enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}
	spaceKey = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
)

// installedRootWithDrift installs a real root then edits one Takt-managed
// file so a drift scan finds exactly one user-edited conflict; it returns
// the root and the edited path.
func installedRootWithDrift(t *testing.T) (string, string) {
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

// scan drives Init()'s scan command to completion and feeds the result back
// into the model, as the real event loop would.
func scan(t *testing.T, m drift.Model) drift.Model {
	t.Helper()
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned a nil command for an installed root")
	}
	msg := cmd()
	next, _ := m.Update(msg)
	model, ok := next.(drift.Model)
	if !ok {
		t.Fatalf("Update(scan result) = %T, want drift.Model", next)
	}
	return model
}

func viewText(m drift.Model) string {
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view, _ := sized.(drift.Model)
	return ansi.Strip(view.View().Content)
}

func TestDriftReportsCleanWhenNothingDiffers(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	model := scan(t, drift.New(root))
	if model.State() != drift.StateReport {
		t.Fatalf("State() = %v, want StateReport", model.State())
	}
	if got := viewText(model); !strings.Contains(got, "No drift") {
		t.Errorf("View() = %q, want the clean report", got)
	}
	if model.Dirty() {
		t.Error("Dirty() on a clean scan = true, want false")
	}
}

func TestDriftFullSelectReviewRestoreFlow(t *testing.T) {
	root, edited := installedRootWithDrift(t)
	model := scan(t, drift.New(root))

	if got := viewText(model); !strings.Contains(got, edited) {
		t.Fatalf("report View() = %q, want it to list the edited path %q", got, edited)
	}

	// StateReport -> StateSelect via the "Select files" action (Enter on
	// action index 0).
	next, _ := model.Update(enterKey)
	model = next.(drift.Model)
	if model.State() != drift.StateSelect {
		t.Fatalf("State() after selecting = %v, want StateSelect", model.State())
	}
	if got := viewText(model); !strings.Contains(got, edited) {
		t.Fatalf("select View() = %q, want it to list the conflicting path %q", got, edited)
	}

	// Toggle the one conflicting file on, then Enter to continue to review.
	next, _ = model.Update(spaceKey)
	model = next.(drift.Model)
	if !model.Dirty() {
		t.Error("Dirty() after toggling a selection = false, want true")
	}
	next, _ = model.Update(enterKey)
	model = next.(drift.Model)
	if model.State() != drift.StateReview {
		t.Fatalf("State() after continuing = %v, want StateReview", model.State())
	}
	if got := viewText(model); !strings.Contains(got, edited) {
		t.Errorf("review View() = %q, want it to name %q", got, edited)
	}

	// StateReview -> restore: Enter on action index 0 (Restore) starts the
	// runtime action and returns the ActionRequest as a command.
	restoring, cmd := model.Update(enterKey)
	model = restoring.(drift.Model)
	if cmd == nil {
		t.Fatal("restore confirmation produced no command")
	}
	requestMsg := cmd()
	request, ok := requestMsg.(runtime.ActionRequest)
	if !ok {
		t.Fatalf("restore command message = %#v, want runtime.ActionRequest", requestMsg)
	}
	if request.Action != runtime.ActionCorrectDrift || !slices.Contains(request.SelectedDriftPaths, edited) {
		t.Fatalf("request = %+v, want ActionCorrectDrift scoped to %q", request, edited)
	}
	if !model.Run().Busy() {
		t.Fatal("model is not busy after starting the restore action")
	}
	if got := viewText(model); !strings.Contains(got, "Drift correction") {
		t.Errorf("busy View() = %q, want the busy screen naming the operation", got)
	}

	// Execute the action for real (no fakes: this is the same Adapter the
	// production shell drives) and feed its result back in.
	result, err := (runtime.Adapter{}).Execute(request)
	if err != nil {
		t.Fatalf("Adapter.Execute() error = %v", err)
	}
	resultMsg := runtime.ActionResultMsg{Request: request, Result: result, Err: err}
	next, _ = model.Update(resultMsg)
	model = next.(drift.Model)
	if model.State() != drift.StateResult {
		t.Fatalf("State() after the result = %v, want StateResult", model.State())
	}
	if model.Run().Busy() {
		t.Error("model is still busy after the result was consumed")
	}
	if got := viewText(model); !strings.Contains(got, edited) {
		t.Errorf("result View() = %q, want it to name the restored path %q", got, edited)
	}

	restored, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(edited)))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Contains(string(restored), "tampered by test") {
		t.Error("restore did not remove the tampered content from disk")
	}
}

// driveToRestoreRequest walks a drift model from a fresh scan through
// selecting and reviewing the one conflicting path, confirming the restore
// and returning the resulting ActionRequest without executing it, so callers
// can feed back whatever ActionResultMsg their branch needs.
func driveToRestoreRequest(t *testing.T, root string) (drift.Model, runtime.ActionRequest) {
	t.Helper()
	model := scan(t, drift.New(root))
	next, _ := model.Update(enterKey) // report -> select
	model = next.(drift.Model)
	next, _ = model.Update(spaceKey) // toggle the one conflicting file on
	model = next.(drift.Model)
	next, _ = model.Update(enterKey) // select -> review
	model = next.(drift.Model)
	restoring, cmd := model.Update(enterKey) // review -> restore
	model = restoring.(drift.Model)
	if cmd == nil {
		t.Fatal("restore confirmation produced no command")
	}
	request, ok := cmd().(runtime.ActionRequest)
	if !ok {
		t.Fatalf("restore command message = %#v, want runtime.ActionRequest", cmd())
	}
	return model, request
}

func TestDriftRestoreFailureShowsFailedBody(t *testing.T) {
	root, edited := installedRootWithDrift(t)
	model, request := driveToRestoreRequest(t, root)

	// A failure after some files were already restored: failedBody must
	// still list what changed alongside the error.
	partial := runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{Changed: []string{edited}}, Err: errors.New("engram reinject failed")}
	next, _ := model.Update(partial)
	model = next.(drift.Model)
	if model.State() != drift.StateResult {
		t.Fatalf("State() after a failed result = %v, want StateResult", model.State())
	}
	got := viewText(model)
	if !strings.Contains(got, edited) || !strings.Contains(got, "engram reinject failed") {
		t.Errorf("failed-partial View() = %q, want it to list %q and the error", got, edited)
	}
}

func TestDriftRestoreFailureWithNothingChangedShowsFailedBody(t *testing.T) {
	root, _ := installedRootWithDrift(t)
	model, request := driveToRestoreRequest(t, root)

	// A failure before anything was restored: failedBody reports the plain
	// failure state, not a partial one.
	failed := runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{}, Err: errors.New("permission denied")}
	next, _ := model.Update(failed)
	model = next.(drift.Model)
	if model.State() != drift.StateResult {
		t.Fatalf("State() after a failed result = %v, want StateResult", model.State())
	}
	if got := viewText(model); !strings.Contains(got, "permission denied") {
		t.Errorf("failed View() = %q, want it to explain the error", got)
	}
}

func TestDriftSelectBackReturnsToReport(t *testing.T) {
	root, _ := installedRootWithDrift(t)
	model := scan(t, drift.New(root))

	next, _ := model.Update(enterKey) // report -> select
	model = next.(drift.Model)
	if model.State() != drift.StateSelect {
		t.Fatalf("State() = %v, want StateSelect", model.State())
	}
	next, cmd := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	model = next.(drift.Model)
	if model.State() != drift.StateReport {
		t.Fatalf("State() after Esc from select = %v, want StateReport", model.State())
	}
	if cmd != nil {
		t.Error("Esc from a nested screen requested navigation out of the flow, want it to only step back a screen")
	}
}
