package sync

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/common"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

func TestTargetSelectionAndSummary(t *testing.T) {
	m := New("/project")
	m = update(t, m, tea.KeyMsg{Type: tea.KeyDown})
	m = update(t, m, tea.KeyMsg{Type: tea.KeySpace})

	if got, want := m.selectedTargets(), []model.AgentID{model.AgentClaudeCode}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selectedTargets() = %v, want %v", got, want)
	}
	view := m.View()
	for _, text := range []string{"Managed files edited locally are preserved.", "Locally deleted managed files are restored."} {
		if !strings.Contains(view, text) {
			t.Fatalf("View() missing %q:\n%s", text, view)
		}
	}
}

func TestConfirmationCancelDoesNotEmitAction(t *testing.T) {
	m := enterConfirmation(t, New("/project"))
	m, cmd := updateWithCommand(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.Phase != PhaseSelect {
		t.Fatalf("Phase = %v, want PhaseSelect", m.Phase)
	}
	if cmd != nil {
		t.Fatal("cancel returned an action command")
	}
}

func TestConfirmationAcceptEmitsSyncRequest(t *testing.T) {
	m := enterConfirmation(t, New("/project"))
	m, cmd := updateWithCommand(t, m, common.AcceptConfirmation{})

	if m.Phase != PhaseRunning || cmd == nil {
		t.Fatalf("accept = phase %v, cmd %v; want running with command", m.Phase, cmd)
	}
	request := testutil.ActionRequest(t, cmd)
	want := runtime.ActionRequest{Action: runtime.ActionSync, RootDir: "/project", Targets: []model.AgentID{model.AgentClaudeCode, model.AgentCodex}}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("request = %#v, want %#v", request, want)
	}
}

func TestSyncResultPresentsChangedAndUnchanged(t *testing.T) {
	m := New("/project")
	m = update(t, m, runtime.ActionResultMsg{Request: runtime.ActionRequest{Action: runtime.ActionSync}, Result: runtime.ActionResult{Action: runtime.ActionSync, Changed: []string{"a"}, Unchanged: []string{"b", "c"}}})

	if m.Phase != PhaseResult {
		t.Fatalf("Phase = %v, want PhaseResult", m.Phase)
	}
	if got := m.View(); !strings.Contains(got, "Changed: 1 · Unchanged: 2") {
		t.Fatalf("View() = %q, want changed and unchanged counts", got)
	}
}

func TestSyncErrorPresentsFailure(t *testing.T) {
	m := update(t, New("/project"), runtime.ActionResultMsg{Request: runtime.ActionRequest{Action: runtime.ActionSync}, Err: errors.New("write denied")})
	view := m.View()
	if !strings.Contains(view, "Sync failed") || !strings.Contains(view, "write denied") {
		t.Fatalf("View() = %q, want failure and error", view)
	}
}

func TestBackNavigation(t *testing.T) {
	m, cmd := updateWithCommand(t, New("/project"), tea.KeyMsg{Type: tea.KeyEsc})
	if m.Phase != PhaseSelect || cmd == nil {
		t.Fatalf("escape = phase %v, cmd %v; want selection and back command", m.Phase, cmd)
	}
	if _, ok := cmd().(BackMsg); !ok {
		t.Fatalf("cmd() = %T, want BackMsg", cmd())
	}
}

func enterConfirmation(t *testing.T, m Model) Model {
	t.Helper()
	m = update(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.Phase != PhaseConfirm {
		t.Fatalf("Phase = %v, want PhaseConfirm", m.Phase)
	}
	return m
}



func update(t *testing.T, m Model, message tea.Msg) Model {
	t.Helper()
	next, _ := updateWithCommand(t, m, message)
	return next
}

func updateWithCommand(t *testing.T, m Model, message tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(message)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("Update() model = %T, want sync.Model", next)
	}
	return updated, cmd
}
