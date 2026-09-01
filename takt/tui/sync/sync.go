// Package sync provides the interactive target-selection flow for native sync.
package sync

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/common"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/styles"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// Phase identifies the visible step of the sync flow.
type Phase int

const (
	PhaseSelect Phase = iota
	PhaseConfirm
	PhaseRunning
	PhaseResult
)

// BackMsg asks the hosting navigation layer to return from the sync flow.
type BackMsg struct{}

// syncTitle is the shared screen title rendered across all sync phases.
const syncTitle = "Sync managed configuration"

type target struct {
	id       model.AgentID
	label    string
	selected bool
}

// Model is a self-contained sync screen. It emits lifecycle messages but never
// plans or applies setup work itself.
type Model struct {
	RootDir string
	Phase   Phase

	cursor       int
	targets      []target
	confirmation common.Confirmation
	result       runtime.ActionResult
	err          error
	keymap       keys.KeyMap
	frame        int
}

// New creates a sync screen for the native targets supported by stage.
func New(rootDir string) Model {
	return Model{
		RootDir: rootDir,
		targets: []target{
			{id: model.AgentClaudeCode, label: "Claude Code", selected: true},
			{id: model.AgentCodex, label: "Codex", selected: true},
		},
		keymap: keys.Default(),
	}
}

// Init has no startup work.
func (Model) Init() tea.Cmd { return nil }

// Update maps input and action outcomes to the next visible sync state.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case runtime.ActionResultMsg:
		if message.Request.Action != runtime.ActionSync {
			return m, nil
		}
		m.Phase = PhaseResult
		m.result = message.Result
		m.err = message.Err
		return m, nil
	case runtime.TickMsg:
		if m.Phase != PhaseRunning {
			return m, nil
		}
		m.frame = message.Frame
		return m, runtime.Tick(message.Frame)
	case common.AcceptConfirmation, common.CancelConfirmation:
		return m.updateConfirmation(message)
	case tea.KeyMsg:
		return m.updateKey(message)
	default:
		return m, nil
	}
}

func (m Model) updateKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.Phase {
	case PhaseRunning:
		return m, nil
	case PhaseResult:
		return m.updateKeyResult(message)
	case PhaseConfirm:
		return m.updateKeyConfirm(message)
	default:
		return m.updateKeySelect(message)
	}
}

func (m Model) updateKeyResult(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.keymap.Confirm.Matches(message) || m.keymap.Back.Matches(message) {
		return m, backCommand()
	}
	return m, nil
}

func (m Model) updateKeyConfirm(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.keymap.Confirm.Matches(message) {
		return m.updateConfirmation(common.AcceptConfirmation{})
	}
	if m.keymap.Back.Matches(message) {
		return m.updateConfirmation(common.CancelConfirmation{})
	}
	return m, nil
}

func (m Model) updateKeySelect(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.keymap.Up.Matches(message) {
		m.cursor = (m.cursor + len(m.targets) - 1) % len(m.targets)
		return m, nil
	}
	if m.keymap.Down.Matches(message) {
		m.cursor = (m.cursor + 1) % len(m.targets)
		return m, nil
	}
	if message.String() == " " {
		m.targets[m.cursor].selected = !m.targets[m.cursor].selected
		return m, nil
	}
	if m.keymap.Confirm.Matches(message) && len(m.selectedTargets()) > 0 {
		m.Phase = PhaseConfirm
		m.confirmation = common.Confirmation{Prompt: "Sync the selected targets?"}
		return m, nil
	}
	if m.keymap.Back.Matches(message) {
		return m, backCommand()
	}
	return m, nil
}

func (m Model) updateConfirmation(message tea.Msg) (tea.Model, tea.Cmd) {
	switch m.confirmation.Update(message) {
	case common.DecisionAccepted:
		m.Phase = PhaseRunning
		request := runtime.ActionRequest{Action: runtime.ActionSync, RootDir: m.RootDir, Targets: m.selectedTargets()}
		return m, tea.Batch(func() tea.Msg { return request }, runtime.Tick(0))
	case common.DecisionCanceled:
		m.Phase = PhaseSelect
		m.confirmation = common.Confirmation{}
	}
	return m, nil
}

func (m Model) selectedTargets() []model.AgentID {
	targets := make([]model.AgentID, 0, len(m.targets))
	for _, target := range m.targets {
		if target.selected {
			targets = append(targets, target.id)
		}
	}
	return targets
}

func backCommand() tea.Cmd {
	return func() tea.Msg { return BackMsg{} }
}

// View renders target selection, confirmation, progress, and outcome states.
func (m Model) View() string {
	switch m.Phase {
	case PhaseRunning:
		var view strings.Builder
		view.WriteString(styles.TitleStyle.Render(syncTitle))
		view.WriteString("\n\n")
		view.WriteString(theme.Caption.Render(ui.Spinner(m.frame) + " "))
		view.WriteString(styles.WarningStyle.Render("Syncing selected targets…"))
		view.WriteString("\n\n")
		view.WriteString(styles.HelpStyle.Render("Please wait."))
		return view.String()
	case PhaseResult:
		return m.shell("return", []keys.Binding{m.keymap.Back}, m.resultBody())
	case PhaseConfirm:
		return m.shell("sync", []keys.Binding{m.keymap.Back}, m.confirmBody())
	default:
		return m.shell("review sync", []keys.Binding{{Keys: []string{" "}, Key: "space", Description: "toggle"}, m.keymap.Back}, m.selectBody())
	}
}

func (m Model) shell(enterLabel string, extra []keys.Binding, body string) string {
	return common.Shell(m.keymap, enterLabel, body, extra...)
}

func (m Model) selectBody() string {
	items := make([]ui.Item, len(m.targets))
	for i, target := range m.targets {
		items[i] = ui.Item{Label: target.label, Checked: target.selected}
	}
	var view strings.Builder
	view.WriteString(styles.TitleStyle.Render(syncTitle))
	view.WriteString("\n\n")
	view.WriteString(ui.CheckList(items, m.cursor, 0))
	view.WriteString("\n")
	m.writeSummary(&view)
	view.WriteString("\n\n")
	if len(m.selectedTargets()) == 0 {
		view.WriteString(styles.ErrorStyle.Render("Select at least one target to continue."))
		view.WriteString("\n")
	}
	return view.String()
}

func (m Model) confirmBody() string {
	var view strings.Builder
	view.WriteString(styles.TitleStyle.Render(syncTitle))
	view.WriteString("\n\n")
	m.writeSummary(&view)
	view.WriteString("\n\n")
	view.WriteString(styles.HeadingStyle.Render(m.confirmation.Prompt))
	return view.String()
}

func (m Model) writeSummary(view *strings.Builder) {
	view.WriteString(styles.HeadingStyle.Render("Native sync"))
	view.WriteString("\n")
	view.WriteString(styles.SubtextStyle.Render("Managed files edited locally are preserved."))
	view.WriteString("\n")
	view.WriteString(styles.SubtextStyle.Render("Locally deleted managed files are restored."))
}

func (m Model) resultBody() string {
	var view strings.Builder
	view.WriteString(styles.TitleStyle.Render(syncTitle))
	view.WriteString("\n\n")
	if m.err != nil {
		view.WriteString(styles.ErrorStyle.Render(theme.Icon.Cross + " Sync failed"))
		view.WriteString("\n\n")
		view.WriteString(styles.SubtextStyle.Render(m.err.Error()))
	} else {
		view.WriteString(styles.SuccessStyle.Render(theme.Icon.Check + " Sync complete"))
		view.WriteString("\n\n")
		view.WriteString(styles.SubtextStyle.Render(fmt.Sprintf("Changed: %d · Unchanged: %d", len(m.result.Changed), len(m.result.Unchanged))))
	}
	return view.String()
}
