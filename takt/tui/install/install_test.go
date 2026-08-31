package install

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/common"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

func TestTargetSelectionAndTransitions(t *testing.T) {
	m := New("/project")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := m.Targets(), []model.AgentID{model.AgentClaudeCode, model.AgentCodex}; !reflect.DeepEqual(got, want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}
	if m.Step() != StepClaudeModels {
		t.Fatalf("step = %v, want Claude models", m.Step())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Step() != StepCodexModels {
		t.Fatalf("step = %v, want Codex models", m.Step())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Step() != StepSetupChoice {
		t.Fatalf("step = %v, want setup choice", m.Step())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Step() != StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
}

func TestModelOptionsStayWithinCanonicalValidation(t *testing.T) {
	for _, alias := range ClaudeModels() {
		if !alias.Valid() {
			t.Errorf("invalid Claude option %q", alias)
		}
	}
	for _, effort := range CodexEfforts() {
		if _, err := model.ValidateCodexModelAssignment("test", model.ModelAssignment{Model: model.CodexModelTerra, Effort: effort}); err != nil {
			t.Errorf("invalid Codex effort %q: %v", effort, err)
		}
		if effort == "xhigh" {
			t.Error("Codex xhigh must not be offered")
		}
	}
}

func TestReviewIncludesSelectedTargetsAndDefaults(t *testing.T) {
	m := reviewModel(t)
	view := m.View()
	for _, value := range []string{"claude-code", "codex", "Setup: default", "canonical defaults", "OpenCode: stage default"} {
		if !strings.Contains(view, value) {
			t.Errorf("review missing %q:\n%s", value, view)
		}
	}
}

func TestDefaultSetupSkipsComponentSelection(t *testing.T) {
	m := choiceModel(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Step() != StepReview {
		t.Fatalf("step = %v, want review directly", m.Step())
	}
	if !strings.Contains(m.View(), "Setup: default") {
		t.Errorf("review missing default setup line:\n%s", m.View())
	}
	if names := m.componentNames(); names != nil {
		t.Errorf("component names = %v, want nil for default setup", names)
	}
}

func TestCustomSetupChecklistAndReview(t *testing.T) {
	m := choiceModel(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Step() != StepComponents {
		t.Fatalf("step = %v, want components", m.Step())
	}

	// Space toggles the focused component (context7).
	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	// Enter toggles the focused component (permissions).
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // Continue
	if m.Step() != StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
	view := m.View()
	if !strings.Contains(view, "Setup: custom · context7, permissions") {
		t.Errorf("review missing custom setup line:\n%s", view)
	}
	if want := []string{"context7", "permissions"}; !slices.Equal(m.componentNames(), want) {
		t.Errorf("component names = %v, want %v", m.componentNames(), want)
	}
}

func TestCustomSetupAllowsZeroComponents(t *testing.T) {
	m := componentsModel(t)
	for i := 0; i < len(componentChoices); i++ {
		m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // Continue
	if m.Step() != StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
	if !strings.Contains(m.View(), "Setup: custom · none") {
		t.Errorf("review missing empty custom setup line:\n%s", m.View())
	}
}

func TestSetupFlowBackNavigation(t *testing.T) {
	m := componentsModel(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.Step() != StepSetupChoice {
		t.Fatalf("step = %v, want setup choice", m.Step())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.Step() != StepCodexModels {
		t.Fatalf("step = %v, want Codex models", m.Step())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // to setup choice
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // default → review
	if m.Step() != StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEscape})
	if m.Step() != StepSetupChoice {
		t.Fatalf("step = %v, want setup choice", m.Step())
	}
}

func TestCancelDoesNotEmitAction(t *testing.T) {
	m := reviewModel(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Step() != StepConfirmation {
		t.Fatalf("step = %v, want confirmation", m.Step())
	}
	updated, command := m.Update(common.CancelConfirmation{})
	if command != nil {
		t.Fatal("cancel emitted a command")
	}
	if got := updated.(Model); got.Step() != StepReview {
		t.Fatalf("step = %v, want review", got.Step())
	}
}

func TestConfirmEmitsInstallRequest(t *testing.T) {
	m := reviewModel(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	updated, command := m.Update(common.AcceptConfirmation{})
	if updated.(Model).Step() != StepConfirmation {
		t.Fatal("confirmation should remain visible until the action result")
	}
	request := actionRequest(t, command)
	if request.Action != runtime.ActionInstall || request.RootDir != "/project" {
		t.Fatalf("request = %#v", request)
	}
	if want := []model.AgentID{model.AgentClaudeCode, model.AgentCodex}; !reflect.DeepEqual(request.Targets, want) {
		t.Fatalf("targets = %v, want %v", request.Targets, want)
	}
	if request.Components != nil {
		t.Fatalf("components = %v, want nil for default setup", request.Components)
	}
	_, duplicate := updated.(Model).Update(common.AcceptConfirmation{})
	if duplicate != nil {
		t.Fatal("confirmation emitted a second install request while waiting for a result")
	}
}

func TestConfirmCarriesSelectedComponents(t *testing.T) {
	m := componentsModel(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // Continue
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter}) // review → confirmation
	_, command := m.Update(common.AcceptConfirmation{})
	request := actionRequest(t, command)
	want := []string{"context7", "permissions"}
	if !slices.Equal(request.Components, want) {
		t.Fatalf("components = %v, want %v", request.Components, want)
	}
}

func TestActionResultPresentationAndReturn(t *testing.T) {
	for _, test := range []struct {
		name string
		msg  runtime.ActionResultMsg
		want string
	}{
		{name: "success", msg: runtime.ActionResultMsg{Result: runtime.ActionResult{Action: runtime.ActionInstall, Changed: []string{"a"}}}, want: "Install complete: 1 changed"},
		{name: "error", msg: runtime.ActionResultMsg{Err: errors.New("disk full")}, want: "Install failed: disk full"},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := reviewModel(t)
			m = update(t, m, test.msg)
			if m.Step() != StepResult || !strings.Contains(m.View(), test.want) {
				t.Fatalf("result view = %q", m.View())
			}
			m = update(t, m, tea.KeyMsg{Type: tea.KeyEscape})
			if m.Step() != StepReview {
				t.Fatalf("step = %v, want review", m.Step())
			}
		})
	}
}

// choiceModel selects claude-code and codex and lands on the setup choice.
func choiceModel(t *testing.T) Model {
	t.Helper()
	m := New("/project")
	for _, key := range []tea.KeyType{tea.KeyEnter, tea.KeyDown, tea.KeyEnter, tea.KeyDown, tea.KeyDown, tea.KeyEnter, tea.KeyEnter, tea.KeyEnter} {
		m = update(t, m, tea.KeyMsg{Type: key})
	}
	if m.Step() != StepSetupChoice {
		t.Fatalf("step = %v, want setup choice", m.Step())
	}
	return m
}

// componentsModel selects a custom setup and lands on the component checklist.
func componentsModel(t *testing.T) Model {
	t.Helper()
	m := choiceModel(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Step() != StepComponents {
		t.Fatalf("step = %v, want components", m.Step())
	}
	return m
}

func reviewModel(t *testing.T) Model {
	t.Helper()
	m := choiceModel(t)
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Step() != StepReview {
		t.Fatalf("step = %v, want review", m.Step())
	}
	return m
}

// actionRequest extracts the emitted ActionRequest from a command, following
// tea.Batch wrappers the flow attaches alongside the busy spinner tick.
func actionRequest(t *testing.T, cmd tea.Cmd) runtime.ActionRequest {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil command")
	}
	if request, ok := cmd().(runtime.ActionRequest); ok {
		return request
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, sub := range batch {
			if request, ok := sub().(runtime.ActionRequest); ok {
				return request
			}
		}
	}
	t.Fatalf("command did not emit ActionRequest")
	return runtime.ActionRequest{}
}

func update(t *testing.T, m Model, message tea.Msg) Model {
	t.Helper()
	updated, _ := m.Update(message)
	return updated.(Model)
}
