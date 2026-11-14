package install_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/install"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/verify"
)

const (
	// otherModel is the model assigned to some agents so the review has to group by model.
	otherModel = "openai/gpt-6-luna"
	// uncertainPlugin is a managed artifact whose pre-existing content makes an uncertain conflict.
	uncertainPlugin = ".config/opencode/plugins/takt-dag/tui.tsx"
	// shortWindowHeight is short enough that the review needs scrolling.
	shortWindowHeight = 20
)

// seedFile writes content at the root-relative path.
func seedFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// configuredRoot records an installed configuration, so the flow opens as a configure.
func configuredRoot(t *testing.T, request setup.PlanRequest) string {
	t.Helper()
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, request); err != nil {
		t.Fatal(err)
	}
	return root
}

// conflictRoot holds an incompatible config (invalid JSON) and an uncertain plugin file.
func conflictRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	seedFile(t, root, opencode.ConfigPath(), "{not json")
	seedFile(t, root, uncertainPlugin, "user edit")
	return root
}

// keptUncertainRoot holds only the uncertain plugin file.
func keptUncertainRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	seedFile(t, root, uncertainPlugin, "user edit")
	return root
}

func shows(t *testing.T, m install.Model, wants ...string) {
	t.Helper()
	got := view(m)
	for _, want := range wants {
		if !testutil.Shows(got, want) {
			t.Errorf("view missing %q:\n%s", want, got)
		}
	}
}

func TestComponentChecklistTogglesAndMakesFlowDirty(t *testing.T) {
	m := press(t, reviewModel(t, t.TempDir()), "left", "enter")
	shows(t, m, ui.TextComponentsIntro, "Context7")

	m = press(t, m, "space")
	if !m.Dirty() {
		t.Fatal("Dirty() after a toggle = false")
	}
	if m = press(t, m, "space"); m.Dirty() {
		t.Error("Dirty() after toggling back = true, want the baseline restored")
	}
	// The checklist keeps focus on its rows whatever key moves it.
	for _, k := range []string{"up", "down", "tab"} {
		if m = press(t, m, k); m.Step() != install.StepComponents {
			t.Fatalf("step = %v after %s, want the checklist", m.Step(), k)
		}
		shows(t, m, "Context7")
	}
}

func TestPageKeyScrollsAndBusyIgnoresKeys(t *testing.T) {
	withoutOpenCode(t)
	m := reviewModel(t, t.TempDir())
	m = update(t, m, tea.WindowSizeMsg{Width: 80, Height: shortWindowHeight})
	before := m.View().Content
	if m = update(t, m, tea.KeyPressMsg{Code: tea.KeyPgDown}); m.View().Content == before {
		t.Error("PgDn did not scroll")
	}

	m, _ = busyModel(t, t.TempDir())
	if got, cmd := m.Update(key("enter")); cmd != nil || got.(install.Model).Step() != install.StepReview {
		t.Error("a key while busy changed the flow")
	}
	shows(t, m, ui.TextBusyInstallation)

	m, _ = func() (install.Model, tea.Cmd) {
		next, cmd := install.New(configuredRoot(t, setup.PlanRequest{})).Update(key("enter"))
		return next.(install.Model), cmd
	}()
	shows(t, m, ui.TextBusyConfiguration)
	if !strings.Contains(m.Title(), ui.TextStepApplying) {
		t.Errorf("Title() while configuring and busy = %q, want %q", m.Title(), ui.TextStepApplying)
	}
}

func TestConflictsScreenBlocksUntilIncompatibleFileIsRestored(t *testing.T) {
	m := install.New(conflictRoot(t))
	if m.Step() != install.StepConflicts {
		t.Fatalf("step = %v, want the file decisions", m.Step())
	}
	shows(t, m,
		ui.TextConflictsIntro, opencode.ConfigPath(), ui.TextConflictKnown, ui.TextConflictAlternative,
		uncertainPlugin, ui.TextConflictUncertain, ui.TextConflictRecommend,
		fmt.Sprintf(ui.TextKeepCannotWorkFmt, opencode.ConfigPath(), ""))

	// Enter on each file keeps it and moves on; on the last file it would
	// advance, but the kept incompatible file holds the flow here.
	if m = press(t, m, "enter", "enter"); m.Step() != install.StepConflicts {
		t.Fatalf("step = %v, want the flow held while the incompatible file is kept", m.Step())
	}

	// Restore Takt for the incompatible file (second row of the first file).
	m = press(t, m, "up", "enter")
	if !m.Dirty() {
		t.Error("Dirty() after restoring = false")
	}
	if !strings.Contains(view(m), theme.Icon.Chosen+ui.TextRestoreTakt) {
		t.Errorf("the restored file's choice is not visible once focus moved:\n%s", view(m))
	}
	// Choosing Keep again clears it.
	if m = press(t, m, "up", "up", "enter"); m.Dirty() {
		t.Error("Dirty() after Keep, want the restore cleared")
	}
	// Restore it again, then Enter on the last file advances to review.
	if m = press(t, m, "up", "enter", "enter"); m.Step() != install.StepReview {
		t.Fatalf("step = %v, want review once the blocker is restored", m.Step())
	}
	if m = press(t, m, "esc"); m.Step() != install.StepConflicts {
		t.Errorf("step = %v, want Esc on review to return to conflicts", m.Step())
	}
	// Conflicts are the flow's first step here, so Back leaves the flow.
	if _, cmd := m.Update(key("esc")); cmd == nil {
		t.Error("Esc on the first step must leave the flow")
	} else if _, ok := cmd().(ui.BackMsg); !ok {
		t.Error("Esc on the first step must request BackMsg")
	}
}

func TestReviewListsEveryChangeCategory(t *testing.T) {
	root := configuredRoot(t, setup.PlanRequest{
		OpenCodeModelOverrides: map[string]model.ModelAssignment{"dev": {Model: otherModel}, "fix": {Model: otherModel}},
	})
	seedFile(t, root, uncertainPlugin, "user edit")
	seedFile(t, root, opencode.ConfigPath(), `{"user":true}`)
	m := install.New(root)
	if m.Step() != install.StepConflicts {
		t.Fatalf("step = %v, want the file decision first", m.Step())
	}
	m = press(t, m, "enter") // keep my version of the uncertain file
	if m.Step() != install.StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
	shows(t, m,
		ui.TextReviewApplyTitle, ui.TextReviewScope, ui.TextGroupInstalls, ui.TextGroupDisk,
		fmt.Sprintf(ui.TextAgentModelsFmt, 2, otherModel), ui.TextInheritedModel,
		"MCP servers", "codegraph", "integrations", "updated",
		"opencode.json", ui.TextConfigMerged,
		"tui.tsx"+ui.TextYourVersionKept, ui.TextFieldKept, ui.TextFieldUncertain,
		ui.TextActionApplyChanges)
}

func TestReviewWithPlanErrorOffersOnlyPersonalize(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(uncertainPlugin)), 0o755); err != nil {
		t.Fatal(err)
	}
	m := install.New(root)
	if m.Step() != install.StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
	shows(t, m, ui.TextCannotPrepareIntro, ui.TextNothingChangedRetry, ui.TextActionPersonalize)
	if testutil.Shows(view(m), ui.TextActionInstall) {
		t.Error("an unresolvable plan still offers Install")
	}
	// Personalize goes to the component checklist; Back returns to review.
	if m = press(t, m, "enter"); m.Step() != install.StepComponents {
		t.Fatalf("step = %v, want the components checklist", m.Step())
	}
	if m = press(t, m, "esc"); m.Step() != install.StepReview {
		t.Errorf("step = %v, want review", m.Step())
	}
	if _, cmd := install.New(root).Update(key("esc")); cmd == nil {
		t.Error("Esc on a failed review must leave the flow")
	} else if _, ok := cmd().(ui.BackMsg); !ok {
		t.Error("Esc on a failed review must request BackMsg")
	}
}

func TestInstallRequestPreservesKeptFilesAndRecordsAcceptedRisks(t *testing.T) {
	m := install.New(keptUncertainRoot(t))
	m = press(t, m, "enter") // keep the uncertain file
	next, cmd := m.Update(key("enter"))
	if !next.(install.Model).Run().Busy() {
		t.Fatal("Install did not start the run")
	}
	request := testutil.ActionRequest(t, cmd)
	if request.Action != runtime.ActionInstall || !slices.Contains(request.PreservePaths, uncertainPlugin) {
		t.Fatalf("request = %+v, want install preserving the kept file", request)
	}
	if len(request.AcceptedRisks) != 1 || request.AcceptedRisks[0].Path != uncertainPlugin || request.AcceptedRisks[0].SHA256 == "" {
		t.Errorf("AcceptedRisks = %+v, want the uncertain kept file recorded", request.AcceptedRisks)
	}
}

func TestResultSuccessDetails(t *testing.T) {
	report := verify.Report{Ready: true}
	tests := []struct {
		name   string
		root   func(*testing.T) string
		keep   bool
		result runtime.ActionResult
		want   []string
		absent []string
	}{
		{
			name:   "install with preserved files and no verification",
			root:   keptUncertainRoot,
			keep:   true,
			result: runtime.ActionResult{Changed: []string{"a"}, Unchanged: []string{"b"}, Preserved: []string{"c"}, CancelRequested: true},
			want: []string{fmt.Sprintf(ui.TextInstalledForFmt, ui.OpenCodeLabel), ui.TextLateCancel,
				fmt.Sprintf(ui.TextFilesChangedFmt, 1, 1) + fmt.Sprintf(ui.TextPreservedFmt, 1),
				ui.TextNotReady, fmt.Sprintf(ui.TextTakeEffectFmt, ui.OpenCodeLabel), "OpenCode tui.tsx"},
		},
		{
			name:   "configure with verified readiness and incomplete work",
			root:   func(t *testing.T) string { return configuredRoot(t, setup.PlanRequest{}) },
			result: runtime.ActionResult{Verify: &report, Incomplete: []string{"plugin-x: failed"}},
			want:   []string{fmt.Sprintf(ui.TextUpdatedForFmt, ui.OpenCodeLabel), ui.TextIncompleteWorkIntro, ui.TextIncompleteWorkTitle, "plugin-x: failed"},
			absent: []string{ui.TextNotReady},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := install.New(tt.root(t))
			if tt.keep {
				m = press(t, m, "enter")
			}
			m, cmd := func() (install.Model, tea.Cmd) {
				next, cmd := m.Update(key("enter"))
				return next.(install.Model), cmd
			}()
			m = update(t, m, runtime.ActionResultMsg{Request: testutil.ActionRequest(t, cmd), Result: tt.result})
			got := view(m)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("view missing %q:\n%s", want, got)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("view must not contain %q:\n%s", absent, got)
				}
			}
		})
	}
}

func TestResultErrorNamesTheRightRetry(t *testing.T) {
	roots := map[string]func(*testing.T) string{
		ui.TextRetryInstall: func(t *testing.T) string { return t.TempDir() },
		ui.TextRetryApply:   func(t *testing.T) string { return configuredRoot(t, setup.PlanRequest{}) },
	}
	for want, root := range roots {
		next, cmd := install.New(root(t)).Update(key("enter"))
		m := update(t, next.(install.Model), runtime.ActionResultMsg{Request: testutil.ActionRequest(t, cmd), Err: errors.New("disk full")})
		shows(t, m, "disk full", want, ui.TextActionRetry)
	}
}

// Leaving the result: Keep current state after a partial stop and Esc both
// return to the menu.
func TestResultLeavesTheFlowAfterPartialStopAndOnEsc(t *testing.T) {
	m, request := busyModel(t, t.TempDir())
	partial := update(t, m, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledPartial}})
	for name, leave := range map[string]install.Model{
		"keep current state": press(t, partial, "right"),
		"esc":                partial,
	} {
		input := "enter"
		if name == "esc" {
			input = "esc"
		}
		_, cmd := leave.Update(key(input))
		if _, ok := cmd().(ui.BackMsg); !ok {
			t.Errorf("%s did not emit BackMsg", name)
		}
	}
}

func TestUnrelatedKeysAndMessagesLeaveReviewAlone(t *testing.T) {
	m := reviewModel(t, t.TempDir())
	if got, cmd := m.Update(key("x")); cmd != nil || got.(install.Model).Step() != install.StepReview {
		t.Error("an unrelated key changed the review")
	}
	if got, cmd := m.Update(struct{}{}); cmd != nil || got.(install.Model).Step() != install.StepReview {
		t.Error("an unknown message changed the review")
	}
}
