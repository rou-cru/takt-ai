package install

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

func TestLongErrorsStayReachable(t *testing.T) {
	m := New(t.TempDir())
	m.width, m.height = 80, 24
	m.step = StepReview
	m.run = m.run.Start(runtime.ActionRequest{ID: 1, Action: runtime.ActionInstall})
	next, _ := m.Update(runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: 1, Action: runtime.ActionInstall}, Err: fmt.Errorf("%s END-OF-ERROR", strings.Repeat("long error ", 100))})
	m = next.(Model)
	for range 30 {
		next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
		m = next.(Model)
	}
	if !strings.Contains(m.View().Content, "END-OF-ERROR") {
		t.Fatal("error details are inaccessible")
	}
}

// conflictModel opens the existing-files step over a fixed plan.
func conflictModel(t *testing.T, conflicts ...setup.ConflictEntry) Model {
	m := New(t.TempDir())
	m.plan.Conflicts = conflicts
	m.step, m.cursor = StepConflicts, m.cursorFor(StepConflicts)
	return m
}

func TestIncompatibleKeepBlocksContinueUntilRestored(t *testing.T) {
	m := conflictModel(t, setup.ConflictEntry{Path: ".config/opencode/opencode.json", Reason: "user-edited", Impact: setup.ImpactIncompatible, Affects: "OpenCode orchestrator agent and Takt configuration", Consequence: "Your version is not valid JSON.", Alternative: "Restore Takt version, or fix the JSON."})
	next, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	next, _ = next.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if next.(Model).step != StepConflicts {
		t.Fatal("Continue activated while a kept file is incompatible")
	}
	next, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	next, _ = next.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter}) // Restore Takt version
	next, _ = next.(Model).Update(tea.KeyPressMsg{Code: tea.KeyTab})
	next, _ = next.(Model).Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if next.(Model).step != StepReview {
		t.Fatalf("step = %v, want review", next.(Model).step)
	}
}
