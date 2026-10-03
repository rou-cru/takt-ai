package uninstall

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

var (
	enter = tea.KeyPressMsg{Code: tea.KeyEnter}
	tab   = tea.KeyPressMsg{Code: tea.KeyTab}
	down  = tea.KeyPressMsg{Code: tea.KeyDown}
	right = tea.KeyPressMsg{Code: tea.KeyRight}
	left  = tea.KeyPressMsg{Code: tea.KeyLeft}
	esc   = tea.KeyPressMsg{Code: tea.KeyEscape}
)

// installedRoot returns a temp root recorded as installed.
func installedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setup.PlanRequest{}); err != nil {
		t.Fatalf("SaveInstalledConfig() error = %v", err)
	}
	return root
}

func TestNotInstalledShowsGuardAndBackNavigation(t *testing.T) {
	screen := New(t.TempDir())
	if screen.State() != StateNotInstalled || !strings.Contains(view(screen), setup.NotInstalledMessage) {
		t.Fatalf("state = %v, view = %s", screen.State(), view(screen))
	}
	_, command := screen.Update(esc)
	if command == nil {
		t.Fatal("Esc did not request back navigation")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Esc did not request back navigation")
	}
}

// The flow opens on the Engram question with the full scope and nothing to
// apply yet; Esc leaves it.
func TestFlowStartsOnEngramWithFixedScope(t *testing.T) {
	screen := New(installedRoot(t))
	if screen.State() != StateEngram || screen.Dirty() {
		t.Fatalf("state = %v dirty = %v", screen.State(), screen.Dirty())
	}
	_, command := screen.Update(esc)
	if command == nil {
		t.Fatal("Esc on the first screen did not request back navigation")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Esc on the first screen did not request back navigation")
	}
}

// TestEngramRemoveChoiceCarriedIntoRequest verifies that an explicit
// Remove choice reaches the runtime request.
func TestEngramRemoveChoiceCarriedIntoRequest(t *testing.T) {
	root := installedRoot(t)
	screen := New(root)
	screen = update(t, update(t, update(t, screen, down), down), enter) // choose Remove, advancing to review
	if screen.State() != StateReview {
		t.Fatalf("state = %v, want review", screen.State())
	}
	_, command := update(t, screen, left).Update(enter) // Uninstall
	if got := testutil.ActionRequest(t, command).EngramChoice; got != lifecycle.EngramRemove {
		t.Fatalf("request EngramChoice = %q, want %q", got, lifecycle.EngramRemove)
	}
}

// TestEngramRetainChoiceCarriedIntoRequest verifies that Retain reaches the
// runtime request and the review names where the copy will go.
func TestEngramRetainChoiceCarriedIntoRequest(t *testing.T) {
	root := installedRoot(t)
	screen := New(root)
	screen = update(t, update(t, screen, down), enter) // choose Retain, advancing to review
	if screen.State() != StateReview {
		t.Fatalf("state = %v, want review", screen.State())
	}
	if v := view(screen); !testutil.Shows(v, ui.TextUninstallEngramRetains) || !testutil.Shows(v, filepath.Join(root, setup.RetainedDirName)) {
		t.Fatalf("review must say the memory is retained and where: %s", v)
	}
	_, command := update(t, screen, left).Update(enter) // Uninstall
	if got := testutil.ActionRequest(t, command).EngramChoice; got != lifecycle.EngramRetain {
		t.Fatalf("request EngramChoice = %q, want %q", got, lifecycle.EngramRetain)
	}
}

// TestResultReportsRetainedMemoryLocation verifies that the delivered
// memory's location reaches the final screen and is not incomplete work.
func TestResultReportsRetainedMemoryLocation(t *testing.T) {
	screen := busyReview(t)
	location := "/home/u/takt-retained/20260912T103000Z/engram/engram.db"
	screen = update(t, screen, runtime.ActionResultMsg{Request: screen.run.Request, Result: runtime.ActionResult{
		Removed: []string{"c.md"}, RetainedDir: "/home/u/takt-retained/20260912T103000Z", RetainedMemory: location,
	}})
	v := view(screen)
	if !strings.Contains(v, ui.TextUninstallMemoryKept+location) || strings.Contains(v, ui.TextUninstallIncomplete) {
		t.Fatalf("result must name the delivered memory and report no incomplete work: %s", v)
	}
}

// Esc on the review returns to the Engram question so answers can be fixed.
func TestReviewBackReturnsToEngram(t *testing.T) {
	screen := update(t, update(t, New(installedRoot(t)), tab), enter)
	if screen.State() != StateReview {
		t.Fatalf("state = %v, want review", screen.State())
	}
	screen = update(t, screen, esc)
	if screen.State() != StateEngram {
		t.Fatalf("state = %v, want engram", screen.State())
	}
}

func TestResultSeparatesKeptFromIncomplete(t *testing.T) {
	screen := busyReview(t)
	screen.modified = []string{"a.md", "b.md"}
	screen.decisions = map[string]bool{"a.md": true, "b.md": false}
	screen.preview.PreservedReasons = map[string]string{"d.md": "pre-existing"}
	screen = update(t, screen, runtime.ActionResultMsg{Request: screen.run.Request, Result: runtime.ActionResult{
		Removed: []string{"c.md"}, Restored: []string{"original.md"}, Preserved: []string{"a.md", "b.md", "c.md", "d.md"},
		Retained: []string{"a.md"}, RetainedDir: "/home/u/takt-retained/x", Incomplete: []string{"b.md: permission denied"},
	}})
	v := view(screen)
	for _, text := range []string{
		ui.TextUninstallRestoreTitle, "original.md", ui.TextUninstallKeptChoice,
		ui.TextUninstallRetainedIn + "/home/u/takt-retained/x", ui.TextUninstallKeepInPlace,
		"d.md" + ui.TextReasonSep + ui.TextReasonPreexisting,
		ui.TextUninstallIncomplete, "b.md: permission denied",
	} {
		if !strings.Contains(v, text) {
			t.Errorf("result missing %q:\n%s", text, v)
		}
	}
	for _, path := range []string{"a.md", "b.md", "c.md", "d.md"} {
		if count := strings.Count(v, path); count != 1 {
			t.Errorf("%s reported %d times; each path must have one outcome:\n%s", path, count, v)
		}
	}
}

func TestStaleResultIDIgnored(t *testing.T) {
	screen := busyReview(t)
	stale := screen.run.Request
	stale.ID++
	screen = update(t, screen, runtime.ActionResultMsg{Request: stale})
	if screen.State() != StateReview || !screen.run.Busy() {
		t.Fatalf("stale result accepted: state %v busy %v", screen.State(), screen.run.Busy())
	}
}

// TestResultPartialWhenFailureFollowsRemoval: files removed before an error
// are reported with it, not hidden behind a total failure.
func TestResultPartialWhenFailureFollowsRemoval(t *testing.T) {
	screen := busyReview(t)
	screen = update(t, screen, runtime.ActionResultMsg{Request: screen.run.Request, Result: runtime.ActionResult{Removed: []string{"c.md"}}, Err: errors.New("remove engram: boom")})
	v := view(screen)
	if !strings.Contains(v, "c.md") || !strings.Contains(v, "remove engram: boom") {
		t.Fatalf("result must report what was removed and the failure: %s", v)
	}
}

func TestCancelRequestShowsCancellationRequested(t *testing.T) {
	screen := busyReview(t)
	screen = update(t, screen, runtime.CancelRequest{ID: screen.run.Request.ID + 1})
	if screen.run.CancelRequested {
		t.Fatal("cancel for another request was accepted")
	}
	screen = update(t, screen, runtime.CancelRequest{ID: screen.run.Request.ID})
	if !screen.run.CancelRequested {
		t.Fatal("cancel for the running request was not recorded")
	}
}

func TestFailedResultAndActions(t *testing.T) {
	screen := busyReview(t)
	screen = update(t, screen, runtime.ActionResultMsg{Request: screen.run.Request, Err: errors.New("manifest unavailable")})
	if screen.State() != StateResult {
		t.Fatalf("state = %v, want result", screen.State())
	}
	_, command := screen.Update(enter)
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Back to menu did not emit BackMsg")
	}
	_, command = update(t, screen, right).Update(enter)
	if _, ok := command().(tea.QuitMsg); !ok {
		t.Fatal("Quit did not quit")
	}
}

func busyReview(t *testing.T) Model {
	t.Helper()
	screen := update(t, New(installedRoot(t)), enter) // choose Leave, advancing to review
	return update(t, update(t, screen, left), enter)  // Uninstall (focus starts on Back)
}

func update(t *testing.T, screen Model, message tea.Msg) Model {
	t.Helper()
	updated, _ := screen.Update(message)
	return updated.(Model)
}

func view(screen Model) string {
	screen.width, screen.height = 200, 60
	return ansi.Strip(screen.View().Content)
}

func TestCancelledPartialResultKeepsCurrentState(t *testing.T) {
	screen := busyReview(t)
	screen = update(t, screen, runtime.ActionResultMsg{Request: screen.run.Request, Result: runtime.ActionResult{
		Outcome: lifecycle.OutcomeCancelledPartial, Removed: []string{"c.md"}, NotApplied: []string{"d.md"},
	}})
	if screen.State() != StateResult {
		t.Fatalf("state = %v, want result", screen.State())
	}
	if _, command := screen.Update(enter); command == nil {
		t.Fatal("Keep current state emitted nothing")
	} else if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Keep current state did not return to the menu")
	}
}
