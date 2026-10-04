package install

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/verify"
)

const (
	stateWidth  = 200
	stateHeight = 80
	// restoreChoice is a conflict's second row, "Restore Takt", in cursor units.
	restoreChoice = 1
)

var (
	kEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	kEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	kTab   = tea.KeyPressMsg{Code: tea.KeyTab}
	kDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	kUp    = tea.KeyPressMsg{Code: tea.KeyUp}
	kRight = tea.KeyPressMsg{Code: tea.KeyRight}
	kSpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	kPgDn  = tea.KeyPressMsg{Code: tea.KeyPgDown}
)

// stateConflicts covers each impact a conflict can carry.
func stateConflicts() []setup.ConflictEntry {
	return []setup.ConflictEntry{
		{Path: "agents/bad.md", Reason: "user-edited", Impact: setup.ImpactIncompatible, Affects: "the planner", Consequence: "breaks planning", Alternative: "restore the Takt version"},
		{Path: "agents/maybe.md", Reason: "pre-existing", Impact: setup.ImpactUncertain, Affects: "the reviewer", Consequence: "review may drift", SHA256: "abc"},
		{Path: "agents/far.md", Reason: "missing", Impact: setup.ImpactUnrelated},
		{Path: "agents/old.md", Reason: "user-edited", Impact: setup.ImpactUncertain, Affects: "the writer", Accepted: true},
	}
}

// planModel places the flow on step with a hand-built plan.
func planModel(t *testing.T, step Step, plan runtime.InstallPlan) Model {
	t.Helper()
	m := New(t.TempDir())
	m.plan = plan
	m.step, m.cursor = step, m.cursorFor(step)
	next, _ := m.Update(tea.WindowSizeMsg{Width: stateWidth, Height: stateHeight})
	return next.(Model)
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func plain(m Model) string { return ansi.Strip(m.View().Content) }

func mustContain(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !testutil.Shows(got, want) {
			t.Errorf("view missing %q:\n%s", want, got)
		}
	}
}

func TestConfiguringAnExistingInstallStartsOnReview(t *testing.T) {
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setup.PlanRequest{}); err != nil {
		t.Fatal(err)
	}
	m := New(root)
	if m.Step() != StepReview || !m.configuring {
		t.Fatalf("step = %v configuring = %v, want configure on review", m.Step(), m.configuring)
	}
	if !strings.HasPrefix(m.Title(), ui.TextTitleConfigure) {
		t.Errorf("Title() = %q, want the configure title", m.Title())
	}
	if m.Init() != nil {
		t.Error("Init() returned a startup command")
	}
	if m.Dirty() {
		t.Error("Dirty() before any change = true")
	}
	_, cmd := step(t, m, kEsc)
	if _, ok := cmd().(ui.BackMsg); !ok {
		t.Error("Esc on the configure entry step did not request BackMsg")
	}
}

func TestComponentChecklistTogglesMovesAndMakesFlowDirty(t *testing.T) {
	m := planModel(t, StepComponents, runtime.InstallPlan{})
	mustContain(t, plain(m), ui.TextComponentsIntro, "Context7")

	before := len(m.components)
	m, _ = step(t, m, kSpace)
	if len(m.components) == before || !m.Dirty() {
		t.Fatalf("components = %v dirty = %v, want the toggle to change the draft", m.components, m.Dirty())
	}
	m, _ = step(t, m, kSpace)
	if m.Dirty() {
		t.Error("Dirty() after toggling back = true, want the baseline restored")
	}

	// Components submits with Enter and draws no footer: focus never leaves
	// the checklist, so the cursor can never land somewhere invisible.
	for _, k := range []tea.KeyPressMsg{kUp, kDown, kTab} {
		if m, _ = step(t, m, k); m.focus != ui.SectionBody || m.cursor >= m.rows() {
			t.Fatalf("focus = %v cursor = %v after %v, want a visible checklist row", m.focus, m.cursor, k)
		}
	}
}

func TestPageKeyScrollsAndBusyIgnoresKeys(t *testing.T) {
	m := planModel(t, StepReview, runtime.InstallPlan{})
	if m, _ = step(t, m, kPgDn); m.scroll == 0 {
		t.Error("PgDn did not scroll")
	}
	m.run = m.run.Start(runtime.ActionRequest{ID: 3, Action: runtime.ActionInstall})
	if got, cmd := step(t, m, kEnter); cmd != nil || got.Step() != StepReview {
		t.Error("a key while busy changed the flow")
	}
	mustContain(t, plain(m), ui.TextBusyInstallation)
	m.configuring = true
	mustContain(t, plain(m), ui.TextBusyConfiguration)
	if !strings.Contains(m.Title(), ui.TextStepApplying) {
		t.Errorf("Title() while configuring and busy = %q, want %q", m.Title(), ui.TextStepApplying)
	}
}

func TestConflictsScreenBlocksUntilIncompatibleFileIsRestored(t *testing.T) {
	m := planModel(t, StepConflicts, runtime.InstallPlan{})
	m.plan.Conflicts = stateConflicts()
	mustContain(t, plain(m),
		ui.TextConflictsIntro, "agents/bad.md", ui.TextConflictKnown+"breaks planning", ui.TextConflictAlternative+"restore the Takt version",
		"agents/maybe.md", ui.TextConflictUncertain+"review may drift", ui.TextConflictRecommend,
		ui.TextPreviouslyKeptTitle, "agents/old.md (the writer)", ui.TextNotAffectedTitle, "agents/far.md",
		fmt.Sprintf(ui.TextKeepCannotWorkFmt, "agents/bad.md", "restore the Takt version"))
	if got := len(m.decisions()); got != 2 {
		t.Fatalf("decisions = %d, want only the incompatible and uncertain undecided files", got)
	}

	// Enter on each file keeps it and moves on; on the last file it would
	// advance, but the kept incompatible file holds the flow here.
	m, _ = step(t, m, kEnter)
	if m.Step() != StepConflicts || m.cursor != conflictChoiceCount {
		t.Fatalf("step = %v cursor = %d, want the second file focused", m.Step(), m.cursor)
	}
	if m, _ = step(t, m, kEnter); m.Step() != StepConflicts {
		t.Fatalf("step = %v, want the flow held while agents/bad.md is kept", m.Step())
	}

	// Restore Takt for the incompatible file (second row of the first file).
	m, _ = step(t, m, kUp)
	m, _ = step(t, m, kEnter)
	if len(m.restorePaths) != 1 || m.restorePaths[0] != "agents/bad.md" || !m.Dirty() {
		t.Fatalf("restorePaths = %v, want agents/bad.md restored", m.restorePaths)
	}
	if !strings.Contains(plain(m), theme.Icon.Chosen+ui.TextRestoreTakt) {
		t.Errorf("the restored file's choice is not visible once focus moved:\n%s", plain(m))
	}
	// Choosing Keep again clears it.
	m, _ = step(t, m, kUp)
	m, _ = step(t, m, kUp)
	m, _ = step(t, m, kEnter)
	if len(m.restorePaths) != 0 {
		t.Fatalf("restorePaths = %v, want Keep to clear the restore", m.restorePaths)
	}
	// Restore it again, then Enter on the last file advances to review.
	m, _ = step(t, m, kUp)
	m, _ = step(t, m, kEnter)
	if m, _ = step(t, m, kEnter); m.Step() != StepReview {
		t.Fatalf("step = %v, want review once the blocker is restored", m.Step())
	}
	if m.cursor != m.rows()-1 {
		t.Errorf("cursor = %d, want the commit action focused", m.cursor)
	}

	// Review's Back returns to the decisions.
	if m, _ = step(t, m, kEsc); m.Step() != StepConflicts {
		t.Errorf("step = %v, want Esc on review to return to conflicts", m.Step())
	}
	// Conflicts are the flow's first step here, so Back leaves the flow.
	if _, cmd := step(t, m, kEsc); cmd == nil {
		t.Error("Esc on the first step must leave the flow")
	} else if _, ok := cmd().(ui.BackMsg); !ok {
		t.Error("Esc on the first step must request BackMsg")
	}
}

func TestChooseIgnoresRowsBeyondTheDecisions(t *testing.T) {
	m := planModel(t, StepConflicts, runtime.InstallPlan{})
	m.cursor = restoreChoice
	m.choose()
	if len(m.restorePaths) != 0 {
		t.Errorf("restorePaths = %v, want none when there is nothing to decide", m.restorePaths)
	}
}

func TestReviewListsEveryChangeCategory(t *testing.T) {
	plan := runtime.InstallPlan{
		InstallPreview: lifecycle.InstallPreview{
			Conflicts: stateConflicts(),
			Removals:  []catalog.Removal{{Component: model.ComponentContext7, Reason: "needs network"}},
		},
		Add:    []string{"opencode/agents/one.md", "opencode/skills/alpha/SKILL.md", "opencode/other.txt"},
		Modify: []string{"agents/bad.md", "cfg/opencode.json", "cfg/cli.json"},
		Summary: runtime.InstallSummary{
			Agents: []runtime.AgentModel{
				{Name: "takt", Role: model.RoleOrchestrator, Model: "openai/gpt-6-luna"},
				{Name: "dev", Role: model.RoleExecution, Model: "openai/gpt-6-luna"},
				{Name: "judge-a", Role: model.RoleVerification},
			},
			Skills:       27,
			MCPServers:   []string{"codegraph", "context7", "engram"},
			Integrations: []string{"DAG panel", "memory"},
			Configs:      []runtime.ConfigChange{{Path: "cfg/opencode.json", Merged: true}, {Path: "cfg/cli.json"}},
		},
	}
	m := planModel(t, StepReview, plan)
	got := plain(m)
	mustContain(t, got,
		ui.TextReviewInstallTitle, ui.TextReviewScope, ui.TextGroupInstalls, ui.TextGroupDisk,
		" 3 agents", fmt.Sprintf(ui.TextAgentRolesFmt, 1, 2),
		fmt.Sprintf(ui.TextAgentModelsFmt, 2, "openai/gpt-6-luna"),
		fmt.Sprintf(ui.TextAgentModelsFmt, 1, ui.TextInheritedModel),
		"27 skills", " 3 MCP servers", "codegraph · context7 · engram",
		" 2 integrations", "DAG panel · memory",
		"3 new files · 2 updated",
		"opencode.json", ui.TextConfigMerged, "cli.json", ui.TextConfigUpdated,
		fmt.Sprintf(ui.TextRemovalFmt, model.ComponentContext7, "needs network"),
		"agents/bad.md"+ui.TextYourVersionKept,
		"agents/far.md"+ui.TextNotAffectedNote, "agents/old.md"+ui.TextPreviouslyKeptNote,
		"review may drift",
		ui.TextActionPersonalize, ui.TextActionInstall)

	// Restoring Takt's version of a config says it replaces the user's.
	m.restorePaths = []string{"cfg/cli.json"}
	mustContain(t, plain(m), "replaces your version")

	// One shared model reads as "all on".
	m.plan.Summary.Agents = m.plan.Summary.Agents[:2]
	mustContain(t, plain(m), fmt.Sprintf(ui.TextAgentModelsAllFmt, "openai/gpt-6-luna"))
}

// File counts name only the parts that happen, in proper number.
func TestReviewFileCountsOmitZeroParts(t *testing.T) {
	m := planModel(t, StepReview, runtime.InstallPlan{Add: []string{"opencode/agents/one.md"}})
	got := plain(m)
	mustContain(t, got, "1 new file")
	if strings.Contains(got, "updated") || strings.Contains(got, "1 new files") {
		t.Errorf("review = %q, want only %q", got, "1 new file")
	}
}

func TestReviewWithNothingToChangeAndWithPlanError(t *testing.T) {
	m := planModel(t, StepReview, runtime.InstallPlan{})
	mustContain(t, plain(m), ui.TextNothingToChange)

	m.previewErr = errors.New("catalog offline")
	mustContain(t, plain(m), ui.TextCannotPrepareIntro+"catalog offline", ui.TextNothingChangedRetry, ui.TextActionPersonalize)
	if strings.Contains(plain(m), ui.TextActionInstall) {
		t.Error("an unresolvable plan still offers Install")
	}

	// Personalize is the only action and goes to the component checklist;
	// Back from there returns to review with the choices kept.
	m.cursor = 0
	m, _ = step(t, m, kEnter)
	if m.Step() != StepComponents || !m.setupCustom {
		t.Fatalf("step = %v custom = %v, want the components checklist", m.Step(), m.setupCustom)
	}
	if m, _ = step(t, m, kEsc); m.Step() != StepReview || !m.setupCustom {
		t.Errorf("step = %v custom = %v, want review with the personalized choices", m.Step(), m.setupCustom)
	}
}

func TestReviewBackSkipsConflictsWhenPlanFailed(t *testing.T) {
	m := planModel(t, StepReview, runtime.InstallPlan{})
	m.plan.Conflicts = stateConflicts()
	m.previewErr = errors.New("boom")
	next, cmd := step(t, m, kEsc)
	if next.Step() != StepReview || cmd == nil {
		t.Fatalf("step = %v, want Esc to skip the conflicts of a failed plan and leave", next.Step())
	}
	if _, ok := cmd().(ui.BackMsg); !ok {
		t.Error("Esc on a failed review must request BackMsg")
	}
}

func TestInstallRequestPreservesKeptFilesAndRecordsAcceptedRisks(t *testing.T) {
	m := planModel(t, StepReview, runtime.InstallPlan{})
	m.plan.Conflicts = stateConflicts()
	m.restorePaths = []string{"agents/bad.md"}
	m.cursor = m.rows() - 1
	next, cmd := step(t, m, kEnter)
	if !next.Run().Busy() {
		t.Fatal("Install did not start the run")
	}
	request := testutil.ActionRequest(t, cmd)
	if request.Action != runtime.ActionInstall || len(request.PreservePaths) != 3 {
		t.Fatalf("request = %+v, want install preserving the three kept files", request)
	}
	if len(request.AcceptedRisks) != 2 || request.AcceptedRisks[0].Path != "agents/maybe.md" || request.AcceptedRisks[0].SHA256 != "abc" {
		t.Errorf("AcceptedRisks = %+v, want the uncertain kept files recorded", request.AcceptedRisks)
	}
}

func TestResultSuccessDetails(t *testing.T) {
	report := verify.Report{Ready: true}
	tests := []struct {
		name        string
		configuring bool
		result      runtime.ActionResult
		want        []string
		absent      []string
	}{
		{
			name:   "install with preserved files and no verification",
			result: runtime.ActionResult{Changed: []string{"a"}, Unchanged: []string{"b"}, Preserved: []string{"c"}, CancelRequested: true},
			want: []string{fmt.Sprintf(ui.TextInstalledForFmt, ui.OpenCodeLabel), ui.TextLateCancel,
				fmt.Sprintf(ui.TextFilesChangedFmt, 1, 1) + fmt.Sprintf(ui.TextPreservedFmt, 1),
				ui.TextNotReady, fmt.Sprintf(ui.TextTakeEffectFmt, ui.OpenCodeLabel), fmt.Sprintf(ui.TextCannotGuaranteeFmt, "the reviewer, the writer")},
		},
		{
			name:        "configure with verified readiness and incomplete work",
			configuring: true,
			result:      runtime.ActionResult{Verify: &report, Incomplete: []string{"plugin-x: failed"}},
			want:        []string{fmt.Sprintf(ui.TextUpdatedForFmt, ui.OpenCodeLabel), ui.TextIncompleteWorkIntro, ui.TextIncompleteWorkTitle, "plugin-x: failed"},
			absent:      []string{ui.TextNotReady},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := planModel(t, StepResult, runtime.InstallPlan{})
			m.configuring = tt.configuring
			m.plan.Conflicts = stateConflicts()
			m.outcome = runtime.ActionResultMsg{Result: tt.result}
			got := plain(m)
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
	for configuring, want := range map[bool]string{false: ui.TextRetryInstall, true: ui.TextRetryApply} {
		m := planModel(t, StepResult, runtime.InstallPlan{})
		m.configuring = configuring
		m.outcome = runtime.ActionResultMsg{Err: errors.New("disk full")}
		mustContain(t, plain(m), "disk full", want, ui.TextActionRetry)
	}
}

func TestResultActionsDispatch(t *testing.T) {
	tests := []struct {
		name   string
		result runtime.ActionResultMsg
		moves  int
		check  func(*testing.T, Model, tea.Cmd)
	}{
		{
			name:   "assign models opens the models flow",
			result: runtime.ActionResultMsg{},
			check: func(t *testing.T, _ Model, cmd tea.Cmd) {
				if _, ok := cmd().(OpenModelsMsg); !ok {
					t.Error("Assign models did not emit OpenModelsMsg")
				}
			},
		},
		{
			name:   "menu goes back",
			result: runtime.ActionResultMsg{},
			moves:  1,
			check: func(t *testing.T, _ Model, cmd tea.Cmd) {
				if _, ok := cmd().(ui.BackMsg); !ok {
					t.Error("Back to menu did not emit BackMsg")
				}
			},
		},
		{
			name:   "quit quits",
			result: runtime.ActionResultMsg{},
			moves:  2,
			check: func(t *testing.T, _ Model, cmd tea.Cmd) {
				if _, ok := cmd().(tea.QuitMsg); !ok {
					t.Error("Quit did not emit QuitMsg")
				}
			},
		},
		{
			name:   "retry after failure prepares the plan again",
			result: runtime.ActionResultMsg{Err: errors.New("boom")},
			check: func(t *testing.T, m Model, _ tea.Cmd) {
				if m.Step() != StepReview {
					t.Errorf("step = %v, want a fresh review", m.Step())
				}
			},
		},
		{
			name:   "keep current state goes back after a partial stop",
			result: runtime.ActionResultMsg{Result: runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledPartial}},
			moves:  1,
			check: func(t *testing.T, _ Model, cmd tea.Cmd) {
				if _, ok := cmd().(ui.BackMsg); !ok {
					t.Error("Keep current state did not emit BackMsg")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := planModel(t, StepResult, runtime.InstallPlan{})
			m.outcome = tt.result
			for range tt.moves {
				m, _ = step(t, m, kRight)
			}
			m, cmd := step(t, m, kEnter)
			tt.check(t, m, cmd)
		})
	}
	// Esc on the result leaves the flow.
	m := planModel(t, StepResult, runtime.InstallPlan{})
	_, cmd := step(t, m, kEsc)
	if _, ok := cmd().(ui.BackMsg); !ok {
		t.Error("Esc on the result did not emit BackMsg")
	}
}

func TestUnrelatedKeysAndMessagesLeaveReviewAlone(t *testing.T) {
	m := planModel(t, StepReview, runtime.InstallPlan{})
	m.cursor = 0
	if got, cmd := step(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"}); cmd != nil || got.Step() != StepReview {
		t.Error("an unrelated key changed the review")
	}
	if got, cmd := step(t, m, struct{}{}); cmd != nil || got.Step() != StepReview {
		t.Error("an unknown message changed the review")
	}
	stale := runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: 99, Action: runtime.ActionInstall}}
	if got, _ := step(t, m, stale); got.Step() != StepReview {
		t.Error("a result with no running request changed the review")
	}
}
