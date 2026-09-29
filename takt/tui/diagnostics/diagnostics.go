// Package diagnostics presents functional checks without reinstalling.
package diagnostics

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// State identifies the visible diagnostics step, mirroring the drift and
// uninstall screens' state shape.
type State int

const (
	// StateCollecting is the async verify.Collect run.
	StateCollecting State = iota
	// StateNotInstalled short-circuits collection: there is nothing to check.
	StateNotInstalled
	// StateReport is the collected report.
	StateReport
)

// actionMenu is the screen's only action: diagnostics change nothing, so the
// visible action is the way back.
const actionMenu = ui.TextActionBackToMenu

const (
	// eventReport carries the collected report into the report screen.
	eventReport = "report"
	// eventBack returns to the previous screen.
	eventBack = "back"
)

// Model displays one asynchronous diagnostic collection.
type Model struct {
	root          string
	state         State
	report        *verify.Report
	keymap        keys.KeyMap
	width, height int
	scroll        int
}

// New creates a diagnostic screen rooted at the installation directory.
func New(root string) Model {
	m := Model{root: root, keymap: keys.Default()}
	if !setup.IsInstalled(root) {
		m.state = StateNotInstalled
	}
	return m
}

// Init collects diagnostics without modifying installation files; with
// nothing installed there is nothing to check, so collection is skipped.
func (m Model) Init() tea.Cmd {
	if m.state == StateNotInstalled {
		return nil
	}
	return func() tea.Msg { return verify.Collect(context.Background(), m.root) }
}

// transitions declares the diagnostics flow: collection reaches the report,
// and every screen can go back.
func transitions() ui.Table[State, Model] {
	back := func(state State) func(*Model) (State, tea.Cmd) {
		return func(*Model) (State, tea.Cmd) {
			return state, func() tea.Msg { return ui.BackMsg{} }
		}
	}
	return ui.Table[State, Model]{
		{From: StateCollecting, Event: eventReport}: func(*Model) (State, tea.Cmd) { return StateReport, nil },
		{From: StateCollecting, Event: eventBack}:   back(StateCollecting),
		{From: StateNotInstalled, Event: eventBack}: back(StateNotInstalled),
		{From: StateReport, Event: eventBack}:       back(StateReport),
	}
}

// applyTransition applies one table rule to move between screens.
func (m Model) applyTransition(event string) (tea.Model, tea.Cmd) {
	next, cmd, _ := transitions().Apply(&m, m.state, event)
	m.state = next
	return m, cmd
}

// Update receives checks or requests navigation back.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case verify.Report:
		m.report = &msg
		return m.applyTransition(eventReport)
	case tea.KeyPressMsg:
		if scroll, ok := ui.Scroll(m.scroll, m.height, msg.String()); ok {
			m.scroll = scroll
			return m, nil
		}
		switch {
		case m.keymap.Back.Matches(msg), m.keymap.Confirm.Matches(msg):
			// The only action is the way back, so Esc and Enter agree.
			return m.applyTransition(eventBack)
		}
	}
	return m, nil
}

// Title identifies the task and current step for the stable header region.
func (m Model) Title() string {
	if m.state == StateReport {
		return ui.TextDiagReportTitle
	}
	return ui.TextDiagCheckingTitle
}

// View renders the checking state, the empty report, or the collected
// report inside the shared screen shell.
func (m Model) View() tea.View {
	body := ui.Status(ui.StatePending, ui.TextDiagBusy)
	switch {
	case m.state == StateNotInstalled:
		body = theme.Label.Render(setup.NotInstalledMessage)
	case m.state == StateReport && len(m.report.Checks) == 0:
		body = ui.Status(ui.StateWarning, ui.TextDiagNone) + "\n\n" + theme.Label.Render(m.report.FinalNote)
	case m.state == StateReport:
		body = ui.Verification(*m.report)
	}
	return tea.NewView(ui.Shell(ui.Frame{
		Header: m.Title(),
		Body:   body,
		Footer: ui.FooterActions(ui.Actions(actionMenu), 0, true),
		Width:  m.width,
		Height: m.height,
		Scroll: m.scroll,
	}))
}
