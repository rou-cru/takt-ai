package uninstall

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

var up = tea.KeyPressMsg{Code: tea.KeyUp}

// modifiedScreen returns a screen sitting on the modified-file questions for
// two user-edited files.
func modifiedScreen(t *testing.T) Model {
	t.Helper()
	screen := New(installedRoot(t))
	screen.state = StateModified
	screen.modified = []string{"a.md", "b.md"}
	return screen
}

func requireBack(t *testing.T, command tea.Cmd) {
	t.Helper()
	if command == nil {
		t.Fatal("no command, want back navigation")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("command did not request BackMsg")
	}
}

func TestEngramScreenRendersChoicesAndReportsPlanFailure(t *testing.T) {
	screen := New(installedRoot(t))
	if got := screen.Title(); got != ui.TextUninstallEngramTitle {
		t.Errorf("Title() = %q, want %q", got, ui.TextUninstallEngramTitle)
	}
	v := view(screen)
	for _, text := range append([]string{ui.TextEngramQuestion, ui.TextEngramNever}, engramChoices...) {
		if !testutil.Shows(v, text) {
			t.Errorf("engram view missing %q:\n%s", text, v)
		}
	}

	screen.previewErr = errors.New("manifest unreadable")
	if v := view(screen); !strings.Contains(v, ui.TextUninstallCannotPlan+"manifest unreadable") {
		t.Errorf("engram view must report the plan failure:\n%s", v)
	}
	screen = update(t, update(t, screen, tab), enter)
	if screen.State() != StateEngram {
		t.Errorf("state = %v, want Continue to stay blocked on the engram question", screen.State())
	}
}

func TestEngramCursorMovesAndSelectionMakesFlowDirty(t *testing.T) {
	screen := New(installedRoot(t))
	screen = update(t, screen, down)
	screen = update(t, screen, up)
	if screen.cursor != engramLeave {
		t.Fatalf("cursor after down, up = %d, want %d", screen.cursor, engramLeave)
	}
	screen = update(t, screen, down)
	screen = update(t, screen, enter)
	if screen.engram != engramRetain || !screen.Dirty() {
		t.Errorf("engram = %d dirty = %v, want Retain and a dirty flow", screen.engram, screen.Dirty())
	}
}

func TestModifiedFilesMustAllBeDecidedBeforeContinuing(t *testing.T) {
	screen := modifiedScreen(t)
	if got := screen.Title(); got != ui.TextUninstallModifiedTitle {
		t.Errorf("Title() = %q, want %q", got, ui.TextUninstallModifiedTitle)
	}
	v := view(screen)
	for _, text := range []string{"a.md", "b.md", ui.TextKeepMine, ui.TextUninstallRemoveOpt} {
		if !testutil.Shows(v, text) {
			t.Errorf("modified view missing %q:\n%s", text, v)
		}
	}

	// Enter keeps a.md and moves to the next undecided file; nothing advances
	// while b.md is undecided.
	screen = update(t, screen, enter)
	if screen.State() != StateModified || screen.cursor != len(modifiedChoices) {
		t.Fatalf("state = %v cursor = %d, want b.md focused while it is undecided", screen.State(), screen.cursor)
	}
	if v := view(screen); !strings.Contains(v, theme.Icon.Chosen+ui.TextKeepMine) {
		t.Errorf("a.md's chosen value is not visible once focus moved:\n%s", v)
	}

	// Removing b.md decides the last file and advances.
	screen = update(t, update(t, screen, down), enter)
	if !screen.allDecided() || screen.decisions["a.md"] != true || screen.decisions["b.md"] != false {
		t.Fatalf("decisions = %v, want a.md kept and b.md removed", screen.decisions)
	}
	if !screen.Dirty() {
		t.Error("Dirty() with decisions = false, want true")
	}
	if screen.State() != StateEngram {
		t.Fatalf("state = %v, want the engram question once every file is decided", screen.State())
	}

	// Choosing an Engram answer advances to review, carrying the decisions.
	screen = update(t, screen, enter)
	if screen.State() != StateReview {
		t.Fatalf("state = %v, want review", screen.State())
	}
	if v := view(screen); !strings.Contains(v, ui.TextUninstallKeepMine) || !strings.Contains(v, "b.md"+ui.TextModifiedSuffix) {
		t.Errorf("review must list the kept and to-remove modified files:\n%s", v)
	}
	_, command := update(t, screen, left).Update(enter)
	request := testutil.ActionRequest(t, command)
	if len(request.RetainPaths) != 1 || request.RetainPaths[0] != "a.md" || len(request.RemovePaths) != 1 || request.RemovePaths[0] != "b.md" {
		t.Errorf("request retain = %v remove = %v, want a.md kept and b.md removed", request.RetainPaths, request.RemovePaths)
	}
}

func TestBackNavigationAcrossSteps(t *testing.T) {
	// Modified step leaves the flow.
	_, command := modifiedScreen(t).Update(esc)
	requireBack(t, command)

	// Engram returns to the modified questions when there are any.
	screen := modifiedScreen(t)
	screen.state = StateEngram
	if screen = update(t, screen, esc); screen.State() != StateModified {
		t.Errorf("state = %v, want Esc on engram to return to the modified questions", screen.State())
	}

	// Review's Back action returns to the engram question.
	screen = update(t, update(t, New(installedRoot(t)), tab), enter)
	if screen = update(t, screen, enter); screen.State() != StateEngram {
		t.Errorf("state = %v, want the Back action to return to engram", screen.State())
	}

	// Not-installed guard accepts Enter and Esc.
	_, command = New(t.TempDir()).Update(enter)
	requireBack(t, command)
}

func TestBusyScreenIgnoresKeysAndAdvancesSpinner(t *testing.T) {
	screen := busyReview(t)
	if got := update(t, screen, enter); got.State() != StateReview {
		t.Errorf("Enter while busy changed state to %v", got.State())
	}
	if !strings.Contains(view(screen), ui.TextUninstallBusy) {
		t.Error("busy view does not name the operation")
	}
	next := update(t, screen, spinner.TickMsg{})
	if next.run.ProgressView().Frame != 1 {
		t.Errorf("progress frame after a tick = %d, want 1", next.run.ProgressView().Frame)
	}
	if next.Init() != nil {
		t.Error("Init() returned a startup command")
	}
	if got := update(t, screen, struct{}{}); !got.run.Busy() {
		t.Error("an unknown message ended the run")
	}
}

func TestDirtyIsFalseOnceFinishedOrNotInstalled(t *testing.T) {
	screen := New(installedRoot(t))
	screen.engram = engramRemove
	for _, state := range []State{StateResult, StateNotInstalled} {
		screen.state = state
		if screen.Dirty() {
			t.Errorf("Dirty() in state %v = true, want false", state)
		}
	}
	if got := New(t.TempDir()).Title(); got != ui.TextUninstallTitle {
		t.Errorf("Title() when not installed = %q, want %q", got, ui.TextUninstallTitle)
	}
}

func TestResultBodiesForCancelledAndFailedRuns(t *testing.T) {
	tests := []struct {
		name   string
		result runtime.ActionResult
		err    error
		want   []string
	}{
		{
			name:   "cancelled before applying reports nothing changed",
			result: runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledNothingApplied},
			want:   []string{ui.TextCancelledNone, ui.TextUninstallAgainNote, ui.TextActionBackToMenu},
		},
		{
			name:   "cancelled partway offers to keep the current state",
			result: runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledPartial, Removed: []string{"c.md"}, NotApplied: []string{"d.md"}},
			want:   []string{"c.md", ui.TextUninstallAgainNote, ui.TextActionKeepCurrent},
		},
		{
			name: "failure with nothing done is a plain failure",
			err:  errors.New("locked"),
			want: []string{ui.TextUninstallFailedIntro + "locked"},
		},
		{
			name:   "a late cancel is explained on success",
			result: runtime.ActionResult{Removed: []string{"c.md"}, CancelRequested: true},
			want:   []string{ui.TextLateCancel, "c.md"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			screen := busyReview(t)
			screen = update(t, screen, runtime.ActionResultMsg{Request: screen.run.Request, Result: tt.result, Err: tt.err})
			if screen.State() != StateResult {
				t.Fatalf("state = %v, want result", screen.State())
			}
			v := view(screen)
			for _, want := range tt.want {
				if !testutil.Shows(v, want) {
					t.Errorf("result view missing %q:\n%s", want, v)
				}
			}
		})
	}
}

func TestSuccessfulResultResetsAnswersButCancelledOneKeepsThem(t *testing.T) {
	screen := busyReview(t)
	screen.engram = engramRemove
	done := update(t, screen, runtime.ActionResultMsg{Request: screen.run.Request, Result: runtime.ActionResult{Removed: []string{"c.md"}}})
	if done.engram != engramLeave || done.Dirty() {
		t.Errorf("engram = %d after success, want the safe default", done.engram)
	}
	cancelled := update(t, screen, runtime.ActionResultMsg{Request: screen.run.Request, Result: runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledNothingApplied}})
	if cancelled.engram != engramRemove {
		t.Errorf("engram = %d after a cancelled run, want the answer kept", cancelled.engram)
	}
}

func TestLabelsAndFileCounts(t *testing.T) {
	if got := preserveReasonLabel("takt-additions"); got != ui.TextReasonAdditions {
		t.Errorf("preserveReasonLabel(takt-additions) = %q, want %q", got, ui.TextReasonAdditions)
	}
	if got := preserveReasonLabel("custom"); got != "custom" {
		t.Errorf("preserveReasonLabel(custom) = %q, want it passed through", got)
	}
	if got := files(1); got != ui.TextOneFile {
		t.Errorf("files(1) = %q, want %q", got, ui.TextOneFile)
	}
}
