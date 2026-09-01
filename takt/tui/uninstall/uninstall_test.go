package uninstall

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/common"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

func TestTargetSelectionAndDestructiveSummary(t *testing.T) {
	screen := New("/project")
	screen = update(t, screen, tea.KeyMsg{Type: tea.KeyEnter})
	screen = update(t, screen, tea.KeyMsg{Type: tea.KeyDown})
	screen = update(t, screen, tea.KeyMsg{Type: tea.KeyEnter})
	if got, want := screen.Targets(), []model.AgentID{model.AgentClaudeCode, model.AgentOpenCode}; !sameTargets(got, want) {
		t.Fatalf("targets = %v, want %v", got, want)
	}

	screen = update(t, screen, tea.KeyMsg{Type: tea.KeyDown})
	screen = update(t, screen, tea.KeyMsg{Type: tea.KeyDown})
	screen = update(t, screen, tea.KeyMsg{Type: tea.KeyEnter})
	if screen.State() != StateConfirmation {
		t.Fatalf("state = %v, want confirmation", screen.State())
	}
	for _, text := range []string{"claude-code", "opencode", "Locally modified managed files may be preserved."} {
		if !strings.Contains(screen.View(), text) {
			t.Fatalf("summary missing %q: %s", text, screen.View())
		}
	}
}

func TestConfirmationCancelDoesNotEmitAction(t *testing.T) {
	screen := confirmedModel(t)
	updated, command := screen.Update(common.CancelConfirmation{})
	if command != nil {
		t.Fatal("cancel emitted an action")
	}
	if screen := updated.(Model); screen.State() != StateTargets {
		t.Fatalf("state = %v, want targets", screen.State())
	}
}

func TestConfirmationEmitsTargetScopedUninstallRequest(t *testing.T) {
	screen := confirmedModel(t)
	updated, command := screen.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("confirm did not emit an action")
	}
	screen = updated.(Model)
	request := testutil.ActionRequest(t, command)
	want := runtime.ActionRequest{Action: runtime.ActionUninstall, RootDir: "/project", Targets: []model.AgentID{model.AgentClaudeCode}}
	if !sameRequest(request, want) {
		t.Fatalf("request = %#v, want %#v", request, want)
	}
}

func TestResultPresentationAndBackNavigation(t *testing.T) {
	screen := confirmedModel(t)
	updated, command := screen.Update(common.AcceptConfirmation{})
	screen = updated.(Model)
	request := testutil.ActionRequest(t, command)

	screen = update(t, screen, runtime.ActionResultMsg{
		Request: request,
		Result:  runtime.ActionResult{Action: runtime.ActionUninstall, Removed: []string{".claude/settings.json"}, Preserved: []string{".codex/config.toml"}},
	})
	if screen.State() != StateResult || !strings.Contains(screen.View(), "Removed: .claude/settings.json") || !strings.Contains(screen.View(), "Preserved: .codex/config.toml") {
		t.Fatalf("unexpected success result: %s", screen.View())
	}
	screen = update(t, screen, tea.KeyMsg{Type: tea.KeyEscape})
	if screen.State() != StateTargets {
		t.Fatalf("state = %v, want targets after back", screen.State())
	}
}

func TestErrorResultAndBackNavigation(t *testing.T) {
	screen := confirmedModel(t)
	updated, command := screen.Update(common.AcceptConfirmation{})
	screen = updated.(Model)
	request := testutil.ActionRequest(t, command)

	screen = update(t, screen, runtime.ActionResultMsg{Request: request, Err: errors.New("manifest unavailable")})
	if screen.State() != StateResult || !strings.Contains(screen.View(), "Uninstall failed: manifest unavailable") {
		t.Fatalf("unexpected error result: %s", screen.View())
	}
	screen = update(t, screen, tea.KeyMsg{Type: tea.KeyEnter})
	if screen.State() != StateTargets {
		t.Fatalf("state = %v, want targets after back", screen.State())
	}
}

func confirmedModel(t *testing.T) Model {
	t.Helper()
	model := New("/project")
	model = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.State() != StateConfirmation {
		t.Fatalf("state = %v, want confirmation", model.State())
	}
	return model
}



func update(t *testing.T, model Model, message tea.Msg) Model {
	t.Helper()
	updated, _ := model.Update(message)
	return updated.(Model)
}

func sameTargets(left, right []model.AgentID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
