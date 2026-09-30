package drift

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

const (
	testWidth  = 200
	testHeight = 40
	// manyConflicts overflows the checklist so the position footer appears.
	manyConflicts = 80
	// shortHeight is a window too small to hold manyConflicts rows.
	shortHeight = 24
)

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	keyRight = tea.KeyPressMsg{Code: tea.KeyRight}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	keyPgDn  = tea.KeyPressMsg{Code: tea.KeyPgDown}
)

// conflicts returns one entry per recorded drift reason.
func conflicts() []setup.ConflictEntry {
	return []setup.ConflictEntry{
		{Path: "agents/missing.md", Reason: "missing"},
		{Path: "agents/edited.md", Reason: "user-edited"},
		{Path: "agents/preexisting.md", Reason: "pre-existing"},
	}
}

// scanned returns a report screen holding a finished scan.
func scanned(found []setup.ConflictEntry, version string, err error) Model {
	m := New("unused-root")
	m.state = StateReport
	next, _ := m.Update(tea.WindowSizeMsg{Width: testWidth, Height: testHeight})
	next, _ = next.Update(resultMsg{conflicts: found, installedVersion: version, err: err})
	return next.(Model)
}

func send(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func text(m Model) string { return ansi.Strip(m.View().Content) }

func requireContains(t *testing.T, got string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("view missing %q:\n%s", want, got)
		}
	}
}

// reviewScreen walks report, select (choosing every file) and review.
func reviewScreen(t *testing.T, version string) Model {
	t.Helper()
	m := scanned(conflicts(), version, nil)
	m, _ = send(t, m, keyEnter) // Select files
	for range conflicts() {
		m, _ = send(t, m, keySpace)
		m, _ = send(t, m, keyDown)
	}
	m, _ = send(t, m, keyEnter)
	if m.State() != StateReview {
		t.Fatalf("state = %v, want review", m.State())
	}
	return m
}

func TestReportStatesBeforeAndAfterScan(t *testing.T) {
	pending := New("unused-root")
	pending.state = StateReport
	requireContains(t, text(pending), ui.TextDriftChecking)

	requireContains(t, text(scanned(nil, "", errors.New("no manifest"))), ui.TextDriftCannotCheck+"no manifest")
	requireContains(t, text(scanned(nil, "", nil)), ui.TextDriftClean, ui.TextDriftNoVerify)

	found := text(scanned(conflicts(), "", nil))
	requireContains(t, found, count(len(conflicts()))+ui.TextDriftDifferOutro, "agents/missing.md", "agents/edited.md", ui.TextActionSelectFiles)
}

func TestCleanReportOffersOnlyTheMenuAndSelectIsBlocked(t *testing.T) {
	m := scanned(nil, "", nil)
	if got := m.actions(); len(got) != 1 || got[0] != actionMenu {
		t.Fatalf("actions = %v, want only the menu", got)
	}
	_, cmd := send(t, m, keyEnter)
	if cmd == nil {
		t.Fatal("Enter on Back to menu produced no command")
	}
	if _, ok := cmd().(ui.BackMsg); !ok {
		t.Error("Enter on Back to menu did not request BackMsg")
	}
	// The table refuses to open the checklist when nothing drifted.
	blocked, _ := m.applyTransition(eventSelect)
	if blocked.(Model).State() != StateReport {
		t.Errorf("select on a clean report moved to %v", blocked.(Model).State())
	}
}

func TestSelectionRequiresAtLeastOneFile(t *testing.T) {
	m := scanned(conflicts(), "", nil)
	m, _ = send(t, m, keyEnter)
	if m.State() != StateSelect {
		t.Fatalf("state = %v, want select", m.State())
	}
	requireContains(t, text(m), ui.TextDriftSelectIntro, ui.TextDriftSelectStar)

	m, _ = send(t, m, keyEnter)
	if m.State() != StateSelect {
		t.Errorf("state = %v, want Enter with nothing chosen to stay on select", m.State())
	}

	m, _ = send(t, m, keySpace)
	if !m.Dirty() || len(m.selected) != 1 {
		t.Fatalf("selected = %v dirty = %v, want one chosen file", m.selected, m.Dirty())
	}
	m, _ = send(t, m, keySpace)
	if len(m.selected) != 0 {
		t.Errorf("selected = %v, want a second Space to untoggle", m.selected)
	}

	back, _ := send(t, m, keyEsc)
	if back.State() != StateReport {
		t.Errorf("state = %v, want Esc on select to return to the report", back.State())
	}
}

func TestSelectShowsPositionWhenListOverflows(t *testing.T) {
	many := make([]setup.ConflictEntry, manyConflicts)
	for i := range many {
		many[i] = setup.ConflictEntry{Path: fmt.Sprintf("agents/file-%02d.md", i), Reason: "missing"}
	}
	m := scanned(many, "", nil)
	m, _ = send(t, m, tea.WindowSizeMsg{Width: testWidth, Height: shortHeight})
	m, _ = send(t, m, keyEnter)
	requireContains(t, text(m), fmt.Sprintf(ui.TextPickerPosFmt, 1, manyConflicts))
}

func TestPageKeysScrollTheBody(t *testing.T) {
	m := scanned(conflicts(), "", nil)
	m, _ = send(t, m, keyPgDn)
	if m.scroll == 0 {
		t.Error("PgDn did not scroll the body")
	}
	m, _ = send(t, m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.scroll != 0 {
		t.Errorf("scroll after PgUp = %d, want 0", m.scroll)
	}
}

func TestReviewNamesReferenceAndConsequencePerReason(t *testing.T) {
	m := reviewScreen(t, setup.BuildVersion)
	requireContains(t, text(m),
		fmt.Sprintf(ui.TextDriftRefFmt, setup.BuildVersion),
		ui.TextDriftRestoreHead, ui.TextDriftPreserveHead, ui.TextDriftPreserveAll,
		ui.TextDriftRecreated, ui.TextDriftReplacedEdit, ui.TextDriftReplacedFile,
		ui.TextActionRestoreSelected, ui.TextActionReselect)

	back, _ := send(t, m, keyEsc)
	if back.State() != StateSelect || len(back.selected) != len(conflicts()) {
		t.Errorf("state = %v selected = %v, want Esc to return to select keeping the choice", back.State(), back.selected)
	}
}

func TestRestoreIsBlockedWhenInstalledVersionDiffers(t *testing.T) {
	tests := []struct {
		name    string
		version string
		want    string
	}{
		{"unknown version", "", ui.TextDriftRefUnknown + setup.BuildVersion},
		{"other version", "0.0.0-other", ui.TextDriftRefMismatch + "0.0.0-other"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := reviewScreen(t, tt.version)
			requireContains(t, text(m), tt.want, ui.TextDriftVersionBlock)
			next, cmd := send(t, m, keyEnter)
			if cmd != nil || next.Run().Busy() {
				t.Error("Restore ran although the installed definitions are unavailable")
			}
		})
	}
}

func TestRestoreStartsCorrectionForSelectedPaths(t *testing.T) {
	m := reviewScreen(t, setup.BuildVersion)
	m, cmd := send(t, m, keyEnter)
	if !m.Run().Busy() || cmd == nil {
		t.Fatalf("busy = %v cmd = %v, want a running correction", m.Run().Busy(), cmd != nil)
	}
	request, ok := cmd().(runtime.ActionRequest)
	if !ok || request.Action != runtime.ActionCorrectDrift || len(request.SelectedDriftPaths) != len(conflicts()) {
		t.Fatalf("request = %#v, want correct-drift for every selected path", request)
	}
	requireContains(t, text(m), ui.TextDriftBusy)

	m, _ = send(t, m, keyEnter)
	if !m.Run().Busy() {
		t.Error("a key while busy ended the run")
	}
	m, _ = send(t, m, runtime.CancelRequest{ID: request.ID})
	if !m.Run().CancelRequested {
		t.Error("CancelRequest was not recorded")
	}
	m, _ = send(t, m, spinner.TickMsg{})
	if m.Run().ProgressView().Frame != 1 {
		t.Errorf("progress frame = %d, want 1 after a tick", m.Run().ProgressView().Frame)
	}

	stale := request
	stale.ID++
	if m, _ = send(t, m, runtime.ActionResultMsg{Request: stale}); m.State() != StateReview {
		t.Errorf("state = %v, want a stale result to be ignored", m.State())
	}
	if m, _ = send(t, m, struct{}{}); !m.Run().Busy() {
		t.Error("an unknown message ended the run")
	}
}

func TestResultBodies(t *testing.T) {
	tests := []struct {
		name       string
		correction runtime.ActionResult
		err        error
		want       []string
	}{
		{
			name:       "restored files with a backup note",
			correction: runtime.ActionResult{Changed: []string{"agents/edited.md"}, CancelRequested: true},
			want:       []string{ui.TextDriftRestoredOk, ui.TextDriftRestoredHead, "agents/edited.md", ui.TextDriftTakeEffect, ui.TextDriftBackupIntro + setup.BackupDir, ui.TextLateCancel},
		},
		{
			name:       "nothing needed restoring",
			correction: runtime.ActionResult{},
			want:       []string{ui.TextDriftAlreadyMatch, ui.TextDriftNoFuncVerify},
		},
		{
			name:       "only unplanned files",
			correction: runtime.ActionResult{Unresolved: []string{"agents/gone.md"}},
			want:       []string{ui.TextDriftNoPlanned, ui.TextDriftUnplanned, "agents/gone.md", ui.TextDriftLeftAsIs},
		},
		{
			name:       "restored and unplanned files",
			correction: runtime.ActionResult{Changed: []string{"agents/missing.md"}, Unresolved: []string{"agents/gone.md"}},
			want:       []string{"agents/missing.md", "agents/gone.md", ui.TextDriftLeftAsIs},
		},
		{
			name:       "cancelled after some restores",
			correction: runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledPartial, Changed: []string{"agents/missing.md"}, NotApplied: []string{"agents/edited.md"}},
			want:       []string{"agents/missing.md", ui.TextDriftCheckAgain, ui.TextActionKeepCurrent},
		},
		{
			name:       "failure after some restores",
			correction: runtime.ActionResult{Changed: []string{"agents/missing.md"}},
			err:        errors.New("engram gone"),
			want:       []string{"engram gone", ui.TextDriftRestoredHead, "agents/missing.md", ui.TextDriftSeeCurrent},
		},
		{
			name: "failure before any restore",
			err:  errors.New("locked"),
			want: []string{ui.TextDriftFailedIntro + "locked", ui.TextDriftSeeCurrent},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := reviewScreen(t, setup.BuildVersion)
			m, cmd := send(t, m, keyEnter)
			request := cmd().(runtime.ActionRequest)
			m, _ = send(t, m, runtime.ActionResultMsg{Request: request, Result: tt.correction, Err: tt.err})
			if m.State() != StateResult || m.Run().Busy() {
				t.Fatalf("state = %v busy = %v, want the idle result step", m.State(), m.Run().Busy())
			}
			if m.Title() != ui.TextDriftResultTitle || m.Dirty() {
				t.Errorf("title = %q dirty = %v, want the result title and a clean flow", m.Title(), m.Dirty())
			}
			requireContains(t, text(m), tt.want...)
		})
	}
}

func TestResultActionsRescanAndLeave(t *testing.T) {
	result := func(t *testing.T, correction runtime.ActionResult) Model {
		t.Helper()
		m := reviewScreen(t, setup.BuildVersion)
		m, cmd := send(t, m, keyEnter)
		m, _ = send(t, m, runtime.ActionResultMsg{Request: cmd().(runtime.ActionRequest), Result: correction})
		return m
	}

	t.Run("rescan clears the scan and starts another", func(t *testing.T) {
		m, cmd := send(t, result(t, runtime.ActionResult{Changed: []string{"agents/missing.md"}}), keyEnter)
		if m.State() != StateReport || m.scanned || len(m.selected) != 0 || cmd == nil {
			t.Errorf("state = %v scanned = %v selected = %v cmd = %v, want a fresh pending scan", m.State(), m.scanned, m.selected, cmd != nil)
		}
	})
	t.Run("menu leaves the flow", func(t *testing.T) {
		m := result(t, runtime.ActionResult{Changed: []string{"agents/missing.md"}})
		m, _ = send(t, m, keyRight)
		_, cmd := send(t, m, keyEnter)
		if _, ok := cmd().(ui.BackMsg); !ok {
			t.Error("Back to menu did not request BackMsg")
		}
	})
	t.Run("keep current state leaves after a partial correction", func(t *testing.T) {
		m := result(t, runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledPartial, Changed: []string{"agents/missing.md"}})
		m, _ = send(t, m, keyRight)
		_, cmd := send(t, m, keyEnter)
		if _, ok := cmd().(ui.BackMsg); !ok {
			t.Error("Keep current state did not request BackMsg")
		}
	})
}

func TestTitlesFollowTheStep(t *testing.T) {
	want := map[State]string{
		StateReport:       ui.TextDriftReportTitle,
		StateSelect:       ui.TextDriftSelectTitle,
		StateReview:       ui.TextDriftReviewTitle,
		StateResult:       ui.TextDriftResultTitle,
		StateNotInstalled: ui.TextDriftEmptyTitle,
	}
	m := New("unused-root")
	for state, title := range want {
		m.state = state
		if got := m.Title(); got != title {
			t.Errorf("Title() in state %v = %q, want %q", state, got, title)
		}
	}
}
