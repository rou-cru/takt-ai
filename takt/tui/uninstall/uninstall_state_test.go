package uninstall_test

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/tui/uninstall"
)

var (
	downKey = tea.KeyPressMsg{Code: tea.KeyDown}
	upKey   = tea.KeyPressMsg{Code: tea.KeyUp}
	leftKey = tea.KeyPressMsg{Code: tea.KeyLeft}
	escKey  = tea.KeyPressMsg{Code: tea.KeyEscape}
)

func press(t *testing.T, model uninstall.Model, messages ...tea.Msg) uninstall.Model {
	t.Helper()
	for _, message := range messages {
		next, _ := model.Update(message)
		model = next.(uninstall.Model)
	}
	return model
}

// engramScreen is the uninstall flow of an installed root with no user edits,
// so it opens on the Engram question.
func engramScreen(t *testing.T) uninstall.Model {
	t.Helper()
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("SaveInstalledConfig() error = %v", err)
	}
	return uninstall.New(root)
}

// modifiedScreen installs a real root and edits its first two managed files,
// so the flow opens on the modified-file questions.
func modifiedScreen(t *testing.T) (uninstall.Model, []string) {
	t.Helper()
	root := t.TempDir()
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}
	manifest, err := setup.LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("LoadOwnershipManifest() error = %v", err)
	}
	paths := slices.Sorted(maps.Keys(manifest.Entries))
	if len(paths) < 2 {
		t.Fatalf("installed manifest has %d managed entries, want at least 2", len(paths))
	}
	edited := paths[:2]
	for _, path := range edited {
		full := filepath.Join(root, filepath.FromSlash(path))
		content, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("ReadFile(%q) error = %v", full, err)
		}
		if err := os.WriteFile(full, append(content, []byte("\n# tampered by test\n")...), 0o644); err != nil {
			t.Fatalf("WriteFile(%q) error = %v", full, err)
		}
	}
	return uninstall.New(root), edited
}

// uninstallRequest confirms the review's Uninstall action and returns the
// running model with the request it dispatched.
func uninstallRequest(t *testing.T, model uninstall.Model) (uninstall.Model, runtime.ActionRequest) {
	t.Helper()
	model = press(t, model, leftKey) // focus starts on Back
	next, command := model.Update(enterKey)
	return next.(uninstall.Model), testutil.ActionRequest(t, command)
}

func TestEngramScreenShowsOnlyTheFocusedOptionDescription(t *testing.T) {
	screen := engramScreen(t)
	if got := screen.Title(); got != ui.TextUninstallEngramTitle {
		t.Errorf("Title() = %q, want %q", got, ui.TextUninstallEngramTitle)
	}
	descriptions := []string{ui.TextEngramLeaveDesc, ui.TextEngramRetainDesc, ui.TextEngramRemoveDesc}
	for focused, labels := range []string{ui.TextEngramLeave, ui.TextEngramRetain, ui.TextEngramRemove} {
		v := viewText(screen)
		for _, want := range []string{ui.TextEngramQuestion, ui.TextEngramLeave, ui.TextEngramRetain, ui.TextEngramRemove} {
			if !testutil.Shows(v, want) {
				t.Errorf("focus on %s: view missing %q:\n%s", labels, want, v)
			}
		}
		for index, description := range descriptions {
			if shown := strings.Contains(v, description); shown != (index == focused) {
				t.Errorf("focus on %s: description %q shown = %v, want %v:\n%s", labels, description, shown, index == focused, v)
			}
		}
		screen = press(t, screen, downKey)
	}
}

func TestEngramAnswerReachesTheUninstallRequest(t *testing.T) {
	screen := press(t, engramScreen(t), downKey, upKey, downKey)
	if screen.Dirty() {
		t.Error("Dirty() before any answer = true, want false")
	}
	screen = press(t, screen, enterKey)
	if screen.State() != uninstall.StateReview || !screen.Dirty() {
		t.Fatalf("state = %v dirty = %v, want review and a dirty flow after choosing Retain", screen.State(), screen.Dirty())
	}
	_, request := uninstallRequest(t, screen)
	if request.EngramChoice != lifecycle.EngramRetain {
		t.Errorf("request.EngramChoice = %q, want %q", request.EngramChoice, lifecycle.EngramRetain)
	}
}

func TestEngramQuestionStaysBlockedWhenThePlanCannotBePrepared(t *testing.T) {
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("SaveInstalledConfig() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, setup.OwnershipManifestFilename), []byte("not json"), 0o644); err != nil {
		t.Fatalf("WriteFile(manifest) error = %v", err)
	}
	screen := uninstall.New(root)
	if v := viewText(screen); !strings.Contains(v, ui.TextUninstallCannotPlan) {
		t.Errorf("view must report the plan failure:\n%s", v)
	}
	screen = press(t, screen, tabKey, enterKey)
	if screen.State() == uninstall.StateReview {
		t.Error("Continue advanced to review without a plan")
	}
}

func TestModifiedFilesMustAllBeDecidedBeforeContinuing(t *testing.T) {
	screen, edited := modifiedScreen(t)
	if screen.State() != uninstall.StateModified {
		t.Fatalf("state = %v, want the modified-file questions", screen.State())
	}
	if got := screen.Title(); got != ui.TextUninstallModifiedTitle {
		t.Errorf("Title() = %q, want %q", got, ui.TextUninstallModifiedTitle)
	}
	v := viewText(screen)
	for _, want := range append([]string{ui.TextKeepMine, ui.TextUninstallRemoveOpt}, edited...) {
		if !testutil.Shows(v, want) {
			t.Errorf("modified view missing %q:\n%s", want, v)
		}
	}

	// Keeping the first file does not advance while the second is undecided,
	// and its chosen value stays visible once focus has moved on.
	screen = press(t, screen, enterKey)
	if screen.State() != uninstall.StateModified {
		t.Fatalf("state = %v, want to stay on the modified questions while a file is undecided", screen.State())
	}
	if v := viewText(screen); !strings.Contains(v, ui.TextKeepMine) {
		t.Errorf("first file's decision is not visible:\n%s", v)
	}

	// Removing the second decides the last file and advances to Engram, then
	// review, carrying both decisions.
	screen = press(t, screen, downKey, enterKey)
	if screen.State() != uninstall.StateEngram || !screen.Dirty() {
		t.Fatalf("state = %v dirty = %v, want the engram question and a dirty flow", screen.State(), screen.Dirty())
	}
	screen = press(t, screen, enterKey)
	if screen.State() != uninstall.StateReview {
		t.Fatalf("state = %v, want review", screen.State())
	}
	_, request := uninstallRequest(t, screen)
	if !slices.Equal(request.RetainPaths, edited[:1]) || !slices.Equal(request.RemovePaths, edited[1:]) {
		t.Errorf("request retain = %v remove = %v, want %v kept and %v removed", request.RetainPaths, request.RemovePaths, edited[:1], edited[1:])
	}
}

func TestBackNavigationAcrossSteps(t *testing.T) {
	// The modified step leaves the flow.
	screen, _ := modifiedScreen(t)
	_, command := screen.Update(escKey)
	if command == nil {
		t.Fatal("Esc on the modified step produced no command")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Errorf("Esc on the modified step = %#v, want ui.BackMsg", command())
	}

	// Engram returns to the modified questions when there are any.
	screen = press(t, screen, enterKey, enterKey)
	if screen.State() != uninstall.StateEngram {
		t.Fatalf("state = %v, want the engram question", screen.State())
	}
	if screen = press(t, screen, escKey); screen.State() != uninstall.StateModified {
		t.Errorf("state = %v, want Esc on engram to return to the modified questions", screen.State())
	}

	// Review's Back action returns to the engram question.
	screen = press(t, engramScreen(t), tabKey, enterKey)
	if screen = press(t, screen, enterKey); screen.State() != uninstall.StateEngram {
		t.Errorf("state = %v, want the Back action to return to engram", screen.State())
	}
}

func TestBusyScreenIgnoresKeysAndAdvancesSpinner(t *testing.T) {
	screen := press(t, engramScreen(t), enterKey)
	screen, _ = uninstallRequest(t, screen)
	if !screen.Run().Busy() {
		t.Fatal("model is not busy after confirming the uninstall")
	}
	if got := press(t, screen, enterKey); got.State() != uninstall.StateReview {
		t.Errorf("Enter while busy changed state to %v", got.State())
	}
	if !strings.Contains(viewText(screen), ui.TextUninstallBusy) {
		t.Error("busy view does not name the operation")
	}
	if next := press(t, screen, spinner.TickMsg{}); next.Run().ProgressView().Frame != 1 {
		t.Errorf("progress frame after a tick = %d, want 1", next.Run().ProgressView().Frame)
	}
	if screen.Init() != nil {
		t.Error("Init() returned a startup command")
	}
	if got := press(t, screen, struct{}{}); !got.Run().Busy() {
		t.Error("an unknown message ended the run")
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
			screen, request := uninstallRequest(t, press(t, engramScreen(t), enterKey))
			screen = press(t, screen, runtime.ActionResultMsg{Request: request, Result: tt.result, Err: tt.err})
			if screen.State() != uninstall.StateResult {
				t.Fatalf("state = %v, want result", screen.State())
			}
			v := viewText(screen)
			for _, want := range tt.want {
				if !testutil.Shows(v, want) {
					t.Errorf("result view missing %q:\n%s", want, v)
				}
			}
		})
	}
}

func TestDirtyIsFalseOnceFinishedOrNotInstalled(t *testing.T) {
	screen := press(t, engramScreen(t), downKey, downKey, enterKey)
	screen, request := uninstallRequest(t, screen)
	screen = press(t, screen, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{Removed: []string{"c.md"}}})
	if screen.Dirty() {
		t.Error("Dirty() on the result screen = true, want false")
	}
	if got := uninstall.New(t.TempDir()).Title(); got != ui.TextUninstallTitle {
		t.Errorf("Title() when not installed = %q, want %q", got, ui.TextUninstallTitle)
	}
}
