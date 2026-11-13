package uninstall_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/tui/uninstall"
)

var rightKey = tea.KeyPressMsg{Code: tea.KeyRight}

// installedRoot returns a temp root recorded as installed.
func installedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("SaveInstalledConfig() error = %v", err)
	}
	return root
}

// busyReview confirms the default answers and the Uninstall action, returning the running model and
// the request it dispatched.
func busyReview(t *testing.T) (uninstall.Model, runtime.ActionRequest) {
	t.Helper()
	return uninstallRequest(t, press(t, uninstall.New(installedRoot(t)), enterKey))
}

func TestNotInstalledShowsGuardAndBackNavigation(t *testing.T) {
	screen := uninstall.New(t.TempDir())
	if screen.State() != uninstall.StateNotInstalled || !strings.Contains(viewText(screen), setup.NotInstalledMessage) {
		t.Fatalf("state = %v, view = %s", screen.State(), viewText(screen))
	}
	_, command := screen.Update(escKey)
	if command == nil {
		t.Fatal("Esc did not request back navigation")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Esc did not request back navigation")
	}
}

// The flow opens on the memory question with nothing to apply yet; Esc leaves it.
func TestFlowStartsOnTheMemoryQuestion(t *testing.T) {
	screen := uninstall.New(installedRoot(t))
	if screen.State() != uninstall.StateMemory || screen.Dirty() {
		t.Fatalf("state = %v dirty = %v", screen.State(), screen.Dirty())
	}
	_, command := screen.Update(escKey)
	if command == nil {
		t.Fatal("Esc on the first screen did not request back navigation")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Esc on the first screen did not request back navigation")
	}
}

// TestRetainIsTheDefaultMemoryAnswer verifies that confirming the preselected answer retains the
// memory, and the review names where the copy will go.
func TestRetainIsTheDefaultMemoryAnswer(t *testing.T) {
	root := installedRoot(t)
	screen := press(t, uninstall.New(root), enterKey)
	if screen.State() != uninstall.StateReview || screen.Dirty() {
		t.Fatalf("state = %v dirty = %v, want review and a flow with nothing to apply", screen.State(), screen.Dirty())
	}
	if v := viewText(screen); !testutil.Shows(v, ui.TextUninstallMemoryRetains) || !testutil.Shows(v, filepath.Join(root, setup.RetainedDirName)) {
		t.Fatalf("review must say the memory is retained and where: %s", v)
	}
	if _, request := uninstallRequest(t, screen); !request.RetainMemory {
		t.Fatal("request.RetainMemory = false, want the default to retain")
	}
}

// TestDiscardMemoryChoiceCarriedIntoRequest verifies that choosing Discard makes the flow dirty and
// reaches the runtime request.
func TestDiscardMemoryChoiceCarriedIntoRequest(t *testing.T) {
	screen := press(t, uninstall.New(installedRoot(t)), downKey, enterKey)
	if screen.State() != uninstall.StateReview || !screen.Dirty() {
		t.Fatalf("state = %v dirty = %v, want review and a dirty flow", screen.State(), screen.Dirty())
	}
	if v := viewText(screen); !testutil.Shows(v, ui.TextUninstallMemoryGoes) {
		t.Fatalf("review must say the memory is deleted: %s", v)
	}
	if _, request := uninstallRequest(t, screen); request.RetainMemory {
		t.Fatal("request.RetainMemory = true after choosing Discard")
	}
}

// TestResultReportsRetainedMemoryLocation verifies that the delivered memory's location reaches the
// final screen and is not incomplete work.
func TestResultReportsRetainedMemoryLocation(t *testing.T) {
	screen, request := busyReview(t)
	location := "/home/u/takt-retained/20260912T103000Z/memory/memory.db"
	screen = press(t, screen, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{
		Removed: []string{"c.md"}, RetainedDir: "/home/u/takt-retained/20260912T103000Z", RetainedMemory: location,
	}})
	v := viewText(screen)
	if !strings.Contains(v, ui.TextUninstallMemoryKept+location) || strings.Contains(v, ui.TextUninstallIncomplete) {
		t.Fatalf("result must name the delivered memory and report no incomplete work: %s", v)
	}
}

func TestResultSeparatesKeptFromIncomplete(t *testing.T) {
	screen, edited := modifiedScreen(t)
	screen = press(t, screen, enterKey, downKey, enterKey, enterKey) // keep the first file, remove the second, retain the memory
	screen, request := uninstallRequest(t, screen)
	kept, failed := edited[0], edited[1]
	screen = press(t, screen, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{
		Removed: []string{"c.md"}, Restored: []string{"original.md"}, Preserved: []string{kept, failed, "c.md"},
		Retained: []string{kept}, RetainedDir: "/home/u/takt-retained/x", Incomplete: []string{failed + ": permission denied"},
	}})
	v := viewText(screen)
	for _, text := range []string{
		ui.TextUninstallRestoreTitle, "original.md", ui.TextUninstallKeptChoice,
		ui.TextUninstallRetainedIn + "/home/u/takt-retained/x",
		ui.TextUninstallIncomplete, failed + ": permission denied",
	} {
		if !strings.Contains(v, text) {
			t.Errorf("result missing %q:\n%s", text, v)
		}
	}
	for _, path := range []string{kept, failed, "c.md"} {
		if count := strings.Count(v, path); count != 1 {
			t.Errorf("%s reported %d times; each path must have one outcome:\n%s", path, count, v)
		}
	}
}

func TestStaleResultIDIgnored(t *testing.T) {
	screen, request := busyReview(t)
	request.ID++
	screen = press(t, screen, runtime.ActionResultMsg{Request: request})
	if screen.State() != uninstall.StateReview || !screen.Run().Busy() {
		t.Fatalf("stale result accepted: state %v busy %v", screen.State(), screen.Run().Busy())
	}
}

// TestResultPartialWhenFailureFollowsRemoval: files removed before an error are reported with it, not
// hidden behind a total failure.
func TestResultPartialWhenFailureFollowsRemoval(t *testing.T) {
	screen, request := busyReview(t)
	screen = press(t, screen, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{Removed: []string{"c.md"}}, Err: errors.New("remove memory: boom")})
	v := viewText(screen)
	if !strings.Contains(v, "c.md") || !strings.Contains(v, "remove memory: boom") {
		t.Fatalf("result must report what was removed and the failure: %s", v)
	}
}

func TestCancelRequestShowsCancellationRequested(t *testing.T) {
	screen, request := busyReview(t)
	screen = press(t, screen, runtime.CancelRequest{ID: request.ID + 1})
	if screen.Run().CancelRequested {
		t.Fatal("cancel for another request was accepted")
	}
	screen = press(t, screen, runtime.CancelRequest{ID: request.ID})
	if !screen.Run().CancelRequested {
		t.Fatal("cancel for the running request was not recorded")
	}
}

func TestFailedResultAndActions(t *testing.T) {
	screen, request := busyReview(t)
	screen = press(t, screen, runtime.ActionResultMsg{Request: request, Err: errors.New("manifest unavailable")})
	if screen.State() != uninstall.StateResult {
		t.Fatalf("state = %v, want result", screen.State())
	}
	_, command := screen.Update(enterKey)
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Back to menu did not emit BackMsg")
	}
	_, command = press(t, screen, rightKey).Update(enterKey)
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatal("Quit did not quit")
	}
}

func TestCancelledPartialResultKeepsCurrentState(t *testing.T) {
	screen, request := busyReview(t)
	screen = press(t, screen, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{
		Outcome: lifecycle.OutcomeCancelledPartial, Removed: []string{"c.md"}, NotApplied: []string{"d.md"},
	}})
	if screen.State() != uninstall.StateResult {
		t.Fatalf("state = %v, want result", screen.State())
	}
	if _, command := screen.Update(enterKey); command == nil {
		t.Fatal("Keep current state emitted nothing")
	} else if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Keep current state did not return to the menu")
	}
}
