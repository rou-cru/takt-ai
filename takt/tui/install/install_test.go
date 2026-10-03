package install_test

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/install"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// The flow opens on the prepared plan: review, with its commit focused.
func TestFlowStartsOnReviewAndEscLeaves(t *testing.T) {
	m := install.New(t.TempDir())
	if m.Step() != install.StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
	_, command := m.Update(key("esc"))
	if command == nil {
		t.Fatal("esc on the first step must leave the flow")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("esc on the first step must emit BackMsg")
	}
}

// Personalize opens the checklist; Enter there returns to review.
func TestPersonalizeOpensComponentsAndEnterReviews(t *testing.T) {
	m := reviewModel(t, t.TempDir())
	if next := press(t, m, "left", "enter"); next.Step() != install.StepComponents {
		t.Fatalf("Personalize: step = %v, want components", next.Step())
	} else if back := press(t, next, "enter"); back.Step() != install.StepReview {
		t.Fatalf("Enter on components: step = %v, want review", back.Step())
	}
}

func TestResultStatesAndActions(t *testing.T) {
	notReady := &verify.Report{Checks: []verify.CheckResult{{ID: "context7", State: verify.NotVerifiable, Explanation: "offline"}}}
	for _, test := range []struct {
		name   string
		result runtime.ActionResult
		err    error
	}{
		{name: "success", result: runtime.ActionResult{Changed: []string{"a"}, Verify: notReady}},
		{name: "failure", err: errors.New("disk full")},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, request := busyModel(t, t.TempDir())
			m = update(t, m, runtime.ActionResultMsg{Request: request, Result: test.result, Err: test.err})
			if m.Step() != install.StepResult {
				t.Fatalf("step %v, want result", m.Step())
			}
			if _, command := m.Update(key("esc")); command == nil {
				t.Fatal("esc on result must go back")
			} else if _, ok := command().(ui.BackMsg); !ok {
				t.Fatal("esc on result must emit BackMsg")
			}
		})
	}
}

func TestRetryReturnsToReviewWithChoices(t *testing.T) {
	m, request := busyModel(t, t.TempDir())
	m = update(t, m, runtime.ActionResultMsg{Request: request, Err: errors.New("boom")})
	m = press(t, m, "enter")
	if m.Step() != install.StepReview {
		t.Fatalf("retry: step %v", m.Step())
	}
	if again := testutil.ActionRequest(t, commit(t, m)); again.ID == request.ID || !reflect.DeepEqual(again.Components, request.Components) {
		t.Fatalf("retry request = %#v", again)
	}
}

func TestResultActionsEmitNavigation(t *testing.T) {
	m, request := busyModel(t, t.TempDir())
	m = update(t, m, runtime.ActionResultMsg{Request: request})
	for _, test := range []struct {
		rights int
		want   tea.Msg
	}{{0, install.OpenModelsMsg{}}, {1, ui.BackMsg{}}, {2, tea.QuitMsg{}}} {
		next := m
		for range test.rights {
			next = press(t, next, "right")
		}
		_, command := next.Update(key("enter"))
		if command == nil || reflect.TypeOf(command()) != reflect.TypeOf(test.want) {
			t.Fatalf("action %d emitted wrong message", test.rights)
		}
	}
}

func TestStaleResultIgnoredAndCancelRequestShown(t *testing.T) {
	m, request := busyModel(t, t.TempDir())
	m = update(t, m, runtime.CancelRequest{ID: request.ID + 1})
	if m.Run().CancelRequested {
		t.Fatal("cancel for another request was accepted")
	}
	m = update(t, m, runtime.CancelRequest{ID: request.ID})
	if !m.Run().CancelRequested {
		t.Fatal("cancel for the running request was not recorded")
	}
	stale := request
	stale.ID++
	if update(t, m, runtime.ActionResultMsg{Request: stale}).Step() == install.StepResult {
		t.Fatal("stale result accepted")
	}
	if update(t, m, runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: request.ID, Action: runtime.ActionSync}}).Step() == install.StepResult {
		t.Fatal("result for another action accepted")
	}
	m = update(t, m, runtime.ActionResultMsg{Request: request})
	if m.Step() != install.StepResult || m.Run().Busy() {
		t.Fatal("pending result not accepted")
	}
}

func TestInstallProgressShowsPhasesBarAndCurrentFile(t *testing.T) {
	m, request := busyModel(t, t.TempDir())
	for _, event := range []setup.DeploymentProgress{
		{Stage: "preparing", Message: "Checking OpenCode connection"},
		{Stage: "applied", Message: "Applying installation files", Path: "agents/alpha.md", Completed: 1, Total: 3},
		{Stage: "applied", Message: "Applying installation files", Path: "agents/beta.md", Completed: 2, Total: 3},
	} {
		m = update(t, m, runtime.ActionProgressMsg{Request: request, Progress: event})
	}
	shown := view(m)
	for _, want := range []string{"✓ Checking OpenCode connection", "Applying installation files", "agents/beta.md", ui.TextActionCancel} {
		if !strings.Contains(shown, want) {
			t.Errorf("busy screen missing %q:\n%s", want, shown)
		}
	}
	if strings.Contains(shown, "agents/alpha.md") {
		t.Errorf("busy screen lists finished files instead of the current one:\n%s", shown)
	}
	if !strings.Contains(shown, "67%") {
		t.Errorf("progress bar did not reflect 2 of 3 artifacts:\n%s", shown)
	}
	stale := request
	stale.ID++
	m = update(t, m, runtime.ActionProgressMsg{Request: stale, Progress: setup.DeploymentProgress{Stage: "applied", Path: "stale.md", Completed: 3, Total: 3}})
	if strings.Contains(view(m), "stale.md") {
		t.Fatal("progress for a different request appeared in the install screen")
	}
}

func TestInstallResultSurfacesIncompleteOptionalWork(t *testing.T) {
	m, request := busyModel(t, t.TempDir())
	m = update(t, m, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{
		Incomplete: []string{"sandbox adapter dependency was not installed: npm unavailable"},
	}})
	shown := view(m)
	for _, text := range []string{ui.TextIncompleteWorkTitle, "npm unavailable"} {
		if !strings.Contains(shown, text) {
			t.Errorf("install result omitted incomplete work %q:\n%s", text, shown)
		}
	}
}

func TestConfigurePersonalizesAndAppliesOnce(t *testing.T) {
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setup.PlanRequest{Components: []string{}}); err != nil {
		t.Fatal(err)
	}
	m := install.New(root)
	if m.Step() != install.StepReview || m.Title() != "Configure installation · Review" || m.Dirty() {
		t.Fatalf("step %v title %q dirty %v", m.Step(), m.Title(), m.Dirty())
	}
	m = press(t, m, "left", "enter") // Personalize
	if v := view(m); m.Step() != install.StepComponents || !strings.Contains(v, "[ ] Context7") {
		t.Fatalf("installed selections not preloaded:\n%s", v)
	}
	if press(t, press(t, m, "space"), "space").Dirty() {
		t.Fatal("reverted edit still dirty")
	}
	m = press(t, m, "space")
	if !m.Dirty() {
		t.Fatal("edit not dirty")
	}
	m = press(t, m, "enter")
	if m.Step() != install.StepReview || !strings.Contains(view(m), "Apply changes") {
		t.Fatalf("configure review:\n%s", view(m))
	}
	request := testutil.ActionRequest(t, commit(t, m))
	if !slices.Equal(request.Components, []string{"context7"}) {
		t.Fatalf("components = %v", request.Components)
	}
	_, command := install.New(root).Update(key("esc"))
	if command == nil {
		t.Fatal("configure esc must leave the flow")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("configure esc must emit BackMsg")
	}
}

// commit presses Enter on a review focused on its commit action.
func commit(t *testing.T, m install.Model) tea.Cmd {
	t.Helper()
	if m.Step() != install.StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
	_, command := m.Update(key("enter"))
	return command
}

func reviewModel(t *testing.T, root string) install.Model {
	t.Helper()
	m := install.New(root)
	if m.Step() != install.StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
	return m
}

func busyModel(t *testing.T, root string) (install.Model, runtime.ActionRequest) {
	t.Helper()
	next, command := reviewModel(t, root).Update(key("enter"))
	return next.(install.Model), testutil.ActionRequest(t, command)
}

func key(value string) tea.KeyPressMsg {
	switch value {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	default:
		runes := []rune(value)
		code := tea.KeyExtended
		if len(runes) == 1 {
			code = runes[0]
		}
		return tea.KeyPressMsg{Code: code, Text: value}
	}
}

func press(t *testing.T, m install.Model, inputs ...string) install.Model {
	t.Helper()
	for _, input := range inputs {
		m = update(t, m, key(input))
	}
	return m
}

func update(t *testing.T, m install.Model, message tea.Msg) install.Model {
	t.Helper()
	updated, _ := m.Update(message)
	return updated.(install.Model)
}

func view(m install.Model) string {
	m = func() install.Model {
		next, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 80})
		return next.(install.Model)
	}()
	return ansi.Strip(m.View().Content)
}

// A cancelled install reports what was applied and offers only the recovery
// that exists: keep the current state or review again. No rollback.
func TestCancelledResultsOfferNoRollback(t *testing.T) {
	m, request := busyModel(t, t.TempDir())
	partial := update(t, m, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{
		Action: runtime.ActionInstall, Outcome: lifecycle.OutcomeCancelledPartial,
		Changed: []string{"a.md"}, NotApplied: []string{"b.md", "c.md"}, BackupDir: "/root/.takt-backups",
	}})
	if next := press(t, partial, "enter"); next.Step() != install.StepReview {
		t.Fatalf("Review again step = %v, want review", next.Step())
	}
}
