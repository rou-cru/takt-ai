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

func TestFlowStartsOnSetupChoiceAndEscLeaves(t *testing.T) {
	m := install.New(t.TempDir())
	if m.Step() != install.StepSetupChoice {
		t.Fatalf("step = %v", m.Step())
	}
	_, command := m.Update(key("esc"))
	if command == nil {
		t.Fatal("esc on the first step must leave the flow")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("esc on the first step must emit BackMsg")
	}
}

func TestSetupChoiceIsSingleSelector(t *testing.T) {
	m := choiceModel(t, t.TempDir())
	if m.Step() != install.StepSetupChoice {
		t.Fatalf("step = %v, want setup choice", m.Step())
	}
	if next := press(t, m, "enter"); next.Step() != install.StepReview {
		t.Fatalf("Default: step = %v, want review", next.Step())
	}
	if next := press(t, m, "down", "enter"); next.Step() != install.StepComponents {
		t.Fatalf("Custom: step = %v, want components", next.Step())
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

func TestConfigureStartsAtComponentsAndAppliesOnce(t *testing.T) {
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setup.PlanRequest{Components: []string{"context7"}}); err != nil {
		t.Fatal(err)
	}
	m := install.New(root)
	if m.Step() != install.StepComponents || m.Title() != "Configure installation · Components" || m.Dirty() {
		t.Fatalf("step %v title %q dirty %v", m.Step(), m.Title(), m.Dirty())
	}
	if v := view(m); !strings.Contains(v, "[x] Context7") || !strings.Contains(v, "[ ] Takt theme") {
		t.Fatalf("installed selections not preloaded:\n%s", v)
	}
	m = press(t, m, "down", "space")
	if !m.Dirty() {
		t.Fatal("edit not dirty")
	}
	if press(t, m, "space").Dirty() {
		t.Fatal("reverted edit still dirty")
	}
	m = press(t, m, "enter")
	if m.Step() != install.StepReview || !strings.Contains(view(m), "Apply changes") {
		t.Fatalf("configure review:\n%s", view(m))
	}
	request := testutil.ActionRequest(t, commit(t, m))
	if !slices.Equal(request.Components, []string{"context7", "theme"}) {
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

func choiceModel(t *testing.T, root string) install.Model {
	t.Helper()
	m := install.New(root)
	if m.Step() != install.StepSetupChoice {
		t.Fatalf("step = %v, want setup choice", m.Step())
	}
	return m
}

func reviewModel(t *testing.T, root string) install.Model {
	t.Helper()
	m := press(t, choiceModel(t, root), "enter")
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
