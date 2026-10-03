package models

import (
	"errors"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/modelpicker"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

const (
	testWidth  = 120
	testHeight = 40
	// requestID is the identity the tests give the in-flight request.
	requestID = 7
	// backActionCursor is the result footer's "Back to menu" position.
	backActionCursor = 1
)

// installedModel builds a screen over a root that has a recorded installation.
func installedModel(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setup.PlanRequest{}); err != nil {
		t.Fatal(err)
	}
	return New(root)
}

// sized applies a window size so View renders a full frame.
func sized(m Model) Model {
	next, _ := m.Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	return next.(Model)
}

func plain(m Model) string { return ansi.Strip(sized(m).View().Content) }

// resultModel places the screen on its result step with the given outcome.
func resultModel(t *testing.T, result runtime.ActionResult, err error) Model {
	t.Helper()
	m := installedModel(t)
	m.state, m.result, m.err = StateResult, result, err
	return m
}

func TestUnavailableScreenOffersOnlyBack(t *testing.T) {
	m := installedModel(t)
	m.notInstalled = true

	if m.Init() != nil {
		t.Error("Init() on an unavailable screen started model discovery")
	}
	if got := m.Title(); got != ui.TextModelsTitle {
		t.Errorf("Title() = %q, want %q", got, ui.TextModelsTitle)
	}
	if got := plain(m); !strings.Contains(got, ui.TextModelsNothing) {
		t.Errorf("View() = %q, want the nothing-installed message", got)
	}
	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEnter}, {Code: tea.KeyEscape}} {
		_, cmd := m.Update(key)
		if cmd == nil {
			t.Fatalf("key %v produced no command", key)
		}
		if _, ok := cmd().(ui.BackMsg); !ok {
			t.Errorf("key %v did not request BackMsg", key)
		}
	}
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}); cmd != nil {
		t.Error("an unrelated key produced a command on the unavailable screen")
	}
}

func TestBusyScreenIgnoresKeysAndTracksCancelAndSpinner(t *testing.T) {
	m := sized(installedModel(t))
	m.run = m.run.Start(runtime.ActionRequest{ID: requestID, Action: runtime.ActionReassignModels})

	if got := ansi.Strip(m.View().Content); !strings.Contains(got, ui.TextModelsBusy) {
		t.Errorf("busy View() = %q, want %q", got, ui.TextModelsBusy)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || !next.(Model).Run().Busy() {
		t.Error("a key while busy changed state or produced a command")
	}

	next, _ = m.Update(runtime.CancelRequest{ID: requestID})
	m = next.(Model)
	if !m.Run().CancelRequested {
		t.Error("CancelRequest for the running request did not mark cancellation")
	}
	if got := ansi.Strip(m.View().Content); !strings.Contains(got, "ancel") {
		t.Errorf("busy View() after cancel = %q, want a cancelling notice", got)
	}

	next, _ = m.Update(spinner.TickMsg{})
	if got := next.(Model).Run().ProgressView().Frame; got != 1 {
		t.Errorf("progress frame after one tick = %d, want 1", got)
	}
}

func TestUnrelatedMessagesAndStaleResultsAreIgnored(t *testing.T) {
	m := installedModel(t)
	if next, cmd := m.Update(struct{}{}); cmd != nil || next.(Model).state != StatePicker {
		t.Error("an unknown message changed the screen")
	}
	stale := runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: requestID, Action: runtime.ActionReassignModels}}
	if next, _ := m.Update(stale); next.(Model).state != StatePicker {
		t.Error("a result with no running request moved the screen to its result step")
	}
	// Paste outside the picker step must not reach the picker.
	m.state = StateResult
	if next, cmd := m.Update(tea.PasteMsg{Content: "x"}); cmd != nil || next.(Model).state != StateResult {
		t.Error("paste on the result step changed the screen")
	}
}

func TestPickerEscapeAsksToLeave(t *testing.T) {
	m := sized(installedModel(t))
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("Esc on the picker produced no command")
	}
	if _, ok := cmd().(ui.BackMsg); !ok {
		t.Error("Esc on the picker did not request BackMsg")
	}
}

func TestResultMessageMovesRunningPickerToResult(t *testing.T) {
	m := installedModel(t)
	request := runtime.ActionRequest{ID: requestID, Action: runtime.ActionReassignModels}
	m.run = m.run.Start(request)
	msg := runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{Changed: []string{"a.md"}}}

	next, _ := m.Update(msg)
	m = next.(Model)
	if m.state != StateResult || m.Run().Busy() {
		t.Fatalf("state = %v busy = %v, want the idle result step", m.state, m.Run().Busy())
	}
	if got := m.Title(); got != ui.TextModelsResultTitle {
		t.Errorf("Title() = %q, want %q", got, ui.TextModelsResultTitle)
	}
}

func TestResultScreenBodies(t *testing.T) {
	tests := []struct {
		name   string
		result runtime.ActionResult
		err    error
		want   []string
		absent []string
	}{
		{
			name:   "cancelled before any change",
			result: runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledNothingApplied},
			want:   []string{ui.TextCancelledNone, ui.TextModelsAssignAgain, ui.TextActionAssignAnother, ui.TextActionBackToMenu},
			absent: []string{ui.TextActionKeepCurrent},
		},
		{
			name:   "cancelled after partial changes keeps current state",
			result: runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledPartial, Changed: []string{"a.md"}, NotApplied: []string{"b"}},
			want:   []string{"a.md", ui.TextModelsAssignAgain, ui.TextActionKeepCurrent},
			absent: []string{ui.TextActionBackToMenu},
		},
		{
			name:   "failure names the harness and the cause",
			result: runtime.ActionResult{},
			err:    errors.New("disk full"),
			want:   []string{ui.TextModelsAssignFail + ui.OpenCodeLabel + ": disk full", "keep their new model"},
		},
		{
			name:   "nothing needed changing",
			result: runtime.ActionResult{},
			want:   []string{ui.TextModelsNoChanges},
		},
		{
			name:   "success lists each agent's new model and when it applies",
			result: runtime.ActionResult{Changed: []string{".config/opencode/opencode.json"}, CancelRequested: true},
			want:   []string{ui.AgentsAssigned(2), "analyst", "provider/one", "dev", "provider/two", ui.TextModelsTakeEffect + ui.OpenCodeLabel, ui.TextLateCancel},
			absent: []string{".config/opencode/opencode.json"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := resultModel(t, tt.result, tt.err)
			m.changes = []modelpicker.Change{{Agent: "analyst", To: "provider/one"}, {Agent: "dev", To: "provider/two"}}
			got := plain(m)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("View() = %q, want it to contain %q", got, want)
				}
			}
			for _, absent := range tt.absent {
				if strings.Contains(got, absent) {
					t.Errorf("View() = %q, must not contain %q", got, absent)
				}
			}
		})
	}
}

func TestResultScreenNavigation(t *testing.T) {
	right := tea.KeyPressMsg{Code: tea.KeyRight}
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}

	t.Run("right then confirm goes back to the menu", func(t *testing.T) {
		m := resultModel(t, runtime.ActionResult{}, nil)
		next, cmd := m.Update(right)
		m = next.(Model)
		if m.resultCursor != backActionCursor || cmd != nil {
			t.Fatalf("cursor = %d cmd = %v, want cursor %d and no command", m.resultCursor, cmd != nil, backActionCursor)
		}
		_, cmd = m.Update(enter)
		if cmd == nil {
			t.Fatal("confirming Back to menu produced no command")
		}
		if _, ok := cmd().(ui.BackMsg); !ok {
			t.Error("confirming Back to menu did not request BackMsg")
		}
	})

	t.Run("confirm on Assign another reopens the picker and rediscovers", func(t *testing.T) {
		m := resultModel(t, runtime.ActionResult{}, nil)
		next, cmd := m.Update(enter)
		m = next.(Model)
		if m.state != StatePicker || !m.picker.Loading || cmd == nil {
			t.Errorf("state = %v loading = %v cmd = %v, want a loading picker with a discovery command", m.state, m.picker.Loading, cmd != nil)
		}
	})

	t.Run("escape requests back", func(t *testing.T) {
		m := resultModel(t, runtime.ActionResult{}, nil)
		_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
		if cmd == nil {
			t.Fatal("Esc on the result step produced no command")
		}
		if _, ok := cmd().(ui.BackMsg); !ok {
			t.Error("Esc on the result step did not request BackMsg")
		}
	})
}
