// Package models lets the user reassign specialists' models after install,
// keeping the rest of a custom installation.
package models

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/modelpicker"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// modelsLoaded carries the discovered OpenCode models into Update.
type modelsLoaded struct {
	models []string
	err    error
}

// discoverModels lists the OpenCode models at the runtime boundary.
func discoverModels() tea.Msg {
	models, err := (runtime.Adapter{}).OpenCodeModels()
	return modelsLoaded{models, err}
}

// State identifies the visible reassignment step.
type State int

// States are the model-reassignment flow's screens.
const (
	// StatePicker is the nested assignment list, model and effort pickers
	// for the harness.
	StatePicker State = iota
	// StateResult is the post-assignment result screen.
	StateResult
)

const (
	// eventConfirm applies the draft or opens the focused choice.
	eventConfirm = "confirm"
	// eventBack returns to the previous screen.
	eventBack = "back"
	// eventResult shows the assignment outcome.
	eventResult = "result"
)

// resultActions lists the result screen's footer actions in order.
var resultActions = []string{ui.TextActionAssignAnother, ui.TextActionBackToMenu}

// Model owns model-reassignment interaction state.
type Model struct {
	root    string
	loadErr error
	// notInstalled reports an installation with no recorded harness.
	notInstalled bool

	state  State
	picker modelpicker.Model
	run    runtime.Run
	// assigned is the number of specialists the pending request changes.
	assigned     int
	result       runtime.ActionResult
	err          error
	resultCursor int
	keymap       keys.KeyMap
	width        int
	height       int
}

// New creates a model-reassignment screen rooted at the installation
// directory; a missing configuration is reported in View instead of blocking.
func New(root string) Model {
	m := Model{root: root, keymap: keys.Default(), run: runtime.NewRun()}
	m.loadPicker()
	return m
}

// loadPicker builds a fresh picker from the installed configuration.
func (m *Model) loadPicker() {
	installed, err := setup.LoadInstalledConfig(m.root)
	switch {
	case err != nil:
		m.loadErr = err
		return
	}
	m.picker = modelpicker.New()
	m.picker.Preload(installed.OpenCodeModelOverrides)
	m.picker.Height = ui.BodyHeight(m.height)
	m.picker.Loading = true
}

// unavailable reports that no assignment can be made.
func (m Model) unavailable() bool { return m.loadErr != nil || m.notInstalled }

// Init discovers the available models for the picker New opened.
func (m Model) Init() tea.Cmd {
	if m.unavailable() {
		return nil
	}
	return discoverModels
}

// Run exposes the flow's action state so the shell gates quit and cancel.
func (m Model) Run() runtime.Run { return m.run }

// transitions declares the confirm/back flow; the picker phases stay nested
// and report only completion and exit.
func transitions() ui.Table[State, Model] {
	return ui.Table[State, Model]{
		{From: StatePicker, Event: eventConfirm}: func(m *Model) (State, tea.Cmd) {
			m.assigned = m.picker.Changes()
			request := runtime.ActionRequest{
				ID:                runtime.NextID(),
				Action:            runtime.ActionReassignModels,
				RootDir:           m.root,
				ReassignOverrides: m.picker.SparseOverrides(),
			}
			m.run = m.run.Start(request)
			return StatePicker, func() tea.Msg { return request }
		},
		// Leaving an unapplied draft asks whether to keep editing or discard.
		{From: StatePicker, Event: eventBack}: func(*Model) (State, tea.Cmd) {
			return StatePicker, ui.Back
		},
		{From: StatePicker, Event: eventResult}: func(m *Model) (State, tea.Cmd) {
			m.run = m.run.End()
			m.resultCursor = 0
			return StateResult, nil
		},
		{From: StateResult, Event: eventConfirm}: func(m *Model) (State, tea.Cmd) {
			if m.resultCursor == 1 {
				return StateResult, ui.Back
			}
			m.loadPicker()
			return StatePicker, discoverModels
		},
		{From: StateResult, Event: eventBack}: func(*Model) (State, tea.Cmd) {
			return StateResult, ui.Back
		},
	}
}

// applyTransition applies one table rule to move between screens.
func (m Model) applyTransition(event string) (tea.Model, tea.Cmd) {
	next, cmd, _ := transitions().Apply(&m, m.state, event)
	m.state = next
	return m, cmd
}

// Update handles interaction and action results without performing lifecycle work.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg), nil
	case modelsLoaded:
		return m.modelsLoaded(msg), nil
	case runtime.ActionResultMsg:
		return m.actionResult(msg)
	case runtime.CancelRequest:
		m.run = m.run.Cancel(msg)
		return m, nil
	case spinner.TickMsg:
		// Same-screen animation only: no navigation, no table event.
		var cmd tea.Cmd
		m.run, cmd = m.run.Tick(msg)
		return m, cmd
	case tea.PasteMsg:
		return m.paste(msg)
	case tea.KeyPressMsg:
		if m.run.Busy() {
			return m, nil
		}
		return m.updateKey(msg)
	}
	return m, nil
}

func (m Model) resize(msg tea.WindowSizeMsg) Model {
	m.width, m.height = msg.Width, msg.Height
	m.picker.Height = ui.BodyHeight(m.height)
	return m
}
func (m Model) modelsLoaded(msg modelsLoaded) Model {
	m.picker.Available, m.picker.LoadErr, m.picker.Loading = msg.models, msg.err, false
	return m
}
func (m Model) actionResult(msg runtime.ActionResultMsg) (tea.Model, tea.Cmd) {
	if message, ok := m.run.Result(msg); ok {
		m.result, m.err = message.Result, message.Err
		return m.applyTransition(eventResult)
	}
	return m, nil
}

func (m Model) paste(msg tea.PasteMsg) (tea.Model, tea.Cmd) {
	if !m.run.Busy() && m.state == StatePicker {
		m.picker.Paste(msg.Content)
	}
	return m, nil
}

func (m Model) updateKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.unavailable() {
		if m.keymap.Back.Matches(key) || m.keymap.Confirm.Matches(key) {
			return m, ui.Back
		}
		return m, nil
	}
	switch m.state {
	case StatePicker:
		done, exit := m.picker.Update(key)
		if exit {
			return m.applyTransition(eventBack)
		}
		if done {
			return m.applyTransition(eventConfirm)
		}
		return m, nil
	case StateResult:
		if ui.NudgeHorizontal(&m.resultCursor, len(resultActions), m.keymap, key) {
			return m, nil
		}
	}
	switch {
	case m.keymap.Confirm.Matches(key):
		return m.applyTransition(eventConfirm)
	case m.keymap.Back.Matches(key):
		return m.applyTransition(eventBack)
	}
	return m, nil
}

// Dirty reports draft assignments that differ from the installed baseline.
func (m Model) Dirty() bool {
	return m.state == StatePicker && !m.run.Busy() && m.picker.Changes() > 0
}

// Title identifies the flow and current step for the stable header region.
func (m Model) Title() string {
	switch {
	case m.unavailable():
		return ui.TextModelsTitle
	case m.state == StateResult:
		return ui.TextModelsResultTitle
	case m.state == StatePicker && m.picker.Detail() != "" && !m.run.Busy():
		return fmt.Sprintf(ui.TextModelsTitleFmt, m.picker.Detail())
	}
	return fmt.Sprintf(ui.TextModelsTitleFmt, ui.OpenCodeLabel)
}

// View renders the active reassignment step.
func (m Model) View() tea.View {
	var frame ui.Frame
	switch {
	case m.unavailable():
		frame = m.unavailableFrame()
	case m.run.Busy():
		frame = ui.Frame{Body: ui.Busy(ui.TextModelsBusy, m.run.CancelRequested, m.run.SpinView())}
	case m.state == StateResult:
		frame = m.resultFrame()
	default:
		frame = m.picker.Frame()
	}
	frame.Header, frame.Width, frame.Height = m.Title(), m.width, m.height
	return tea.NewView(ui.Shell(frame))
}

// unavailableFrame explains why nothing is assignable yet.
func (m Model) unavailableFrame() ui.Frame {
	if m.loadErr != nil {
		body := ui.Status(ui.StateFailed, ui.TextModelsLoadFail+m.loadErr.Error()) + "\n\n" + theme.Caption.Render(ui.TextModelsNothingKept)
		return ui.Frame{Body: body}
	}
	return ui.Frame{Body: theme.Label.Render(ui.TextModelsNothing)}
}

// resultFrame reports what happened, what changed, when it takes effect,
// and the next actions.
func (m Model) resultFrame() ui.Frame {
	harness := ui.OpenCodeLabel
	var b strings.Builder
	actions := resultActions
	switch {
	case m.result.Cancelled() && m.err == nil:
		b.WriteString(runtime.CancelledBody(m.result))
		b.WriteString("\n\n")
		b.WriteString(theme.Label.Render(ui.TextModelsAssignAgain))
		if m.result.Partial() {
			actions = []string{resultActions[0], ui.TextActionKeepCurrent}
		}
	case m.err != nil:
		b.WriteString(ui.Status(ui.StateFailed, ui.TextModelsAssignFail+harness+": "+m.err.Error()))
		b.WriteString("\n\n")
		b.WriteString(theme.Caption.Render(ui.TextModelsFailNote))
	case len(m.result.Changed) == 0:
		b.WriteString(ui.Status(ui.StateSuccess, ui.TextModelsNoChanges))
	default:
		b.WriteString(ui.Status(ui.StateSuccess, fmt.Sprintf(ui.TextModelsAssignedFmt, m.assigned, harness)))
		if note := runtime.LateCancelNote(m.result); note != "" {
			b.WriteString("\n" + note)
		}
		b.WriteString("\n\n")
		b.WriteString(theme.Label.Render(ui.TextModelsChangedHead))
		for _, path := range m.result.Changed {
			b.WriteString("\n")
			b.WriteString(theme.Label.Render("  " + path))
		}
		b.WriteString("\n\n")
		b.WriteString(theme.Label.Render(ui.TextModelsTakeEffect + harness + "."))
	}
	return ui.Frame{Body: b.String(), Footer: ui.FooterActions(ui.Actions(actions...), m.resultCursor, true)}
}
