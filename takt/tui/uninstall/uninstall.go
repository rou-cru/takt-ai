// Package uninstall provides the target-scoped managed-file uninstall screen.
package uninstall

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	modelpkg "github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/common"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/styles"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// State identifies the visible uninstall step.
type State int

const (
	StateTargets State = iota
	StateConfirmation
	StateResult
)

// SupportedTargets are the managed targets that can be selected for uninstall.
func SupportedTargets() []modelpkg.AgentID {
	return []modelpkg.AgentID{modelpkg.AgentClaudeCode, modelpkg.AgentOpenCode, modelpkg.AgentCodex}
}

// Model owns only uninstall interaction state. It emits lifecycle action messages;
// runtime.Adapter owns the setup and filesystem work.
type Model struct {
	RootDir string

	state         State
	cursor        int
	targets       []modelpkg.AgentID
	pending       runtime.ActionRequest
	result        runtime.ActionResult
	err           error
	busy          bool
	frame         int
	backRequested bool
	confirmation  common.Confirmation
	keymap        keys.KeyMap
}

// New creates an uninstall screen rooted at rootDir.
func New(rootDir string) Model {
	return Model{RootDir: rootDir, keymap: keys.Default()}
}

// Init has no startup work.
func (Model) Init() tea.Cmd { return nil }

// State returns the visible uninstall step.
func (model Model) State() State { return model.state }

// Targets returns the explicitly selected uninstall targets.
func (model Model) Targets() []modelpkg.AgentID {
	return append([]modelpkg.AgentID(nil), model.targets...)
}

// BackRequested reports that the parent route should return to its previous screen.
func (model Model) BackRequested() bool { return model.backRequested }

// Update handles interaction and action results without performing lifecycle work.
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if result, ok := message.(runtime.ActionResultMsg); ok && model.busy && sameRequest(result.Request, model.pending) {
		model.busy = false
		model.result = result.Result
		model.err = result.Err
		model.state = StateResult
		return model, nil
	}
	if tick, ok := message.(runtime.TickMsg); ok {
		if !model.busy {
			return model, nil
		}
		model.frame = tick.Frame
		return model, runtime.Tick(tick.Frame)
	}

	switch model.state {
	case StateTargets:
		return model.updateTargets(message)
	case StateConfirmation:
		return model.updateConfirmation(message)
	case StateResult:
		return model.updateResult(message)
	default:
		return model, nil
	}
}

func (model Model) updateTargets(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return model, nil
	}
	count := len(SupportedTargets()) + 1 // The final row is Continue.
	switch {
	case model.keymap.Up.Matches(key):
		model.cursor = (model.cursor + count - 1) % count
	case model.keymap.Down.Matches(key) || model.keymap.NextSection.Matches(key):
		model.cursor = (model.cursor + 1) % count
	case model.keymap.Confirm.Matches(key):
		if model.cursor == count-1 {
			if len(model.targets) > 0 {
				model.state = StateConfirmation
				model.cursor = 0
				model.confirmation = common.Confirmation{Prompt: "Remove the selected managed configuration?"}
			}
			return model, nil
		}
		model.toggle(SupportedTargets()[model.cursor])
	case model.keymap.Back.Matches(key):
		model.backRequested = true
	}
	return model, nil
}

func (model Model) updateConfirmation(message tea.Msg) (tea.Model, tea.Cmd) {
	if model.busy {
		return model, nil
	}
	if decision := model.confirmation.Update(message); decision != common.DecisionNone {
		return model.applyDecision(decision)
	}

	key, ok := message.(tea.KeyMsg)
	if !ok {
		return model, nil
	}
	switch {
	case model.keymap.Up.Matches(key) || model.keymap.Down.Matches(key) || model.keymap.NextSection.Matches(key):
		model.cursor = 1 - model.cursor
	case model.keymap.Confirm.Matches(key):
		if model.cursor == 0 {
			return model.updateConfirmation(common.AcceptConfirmation{})
		}
		return model.updateConfirmation(common.CancelConfirmation{})
	case model.keymap.Back.Matches(key):
		return model.updateConfirmation(common.CancelConfirmation{})
	}
	return model, nil
}

func (model Model) applyDecision(decision common.Decision) (tea.Model, tea.Cmd) {
	if decision == common.DecisionCanceled {
		model.state = StateTargets
		model.cursor = len(SupportedTargets())
		return model, nil
	}
	model.pending = runtime.ActionRequest{
		Action:  runtime.ActionUninstall,
		RootDir: model.RootDir,
		Targets: model.Targets(),
	}
	model.busy = true
	return model, tea.Batch(func() tea.Msg { return model.pending }, runtime.Tick(0))
}

func (model Model) updateResult(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyMsg)
	if !ok {
		return model, nil
	}
	if model.keymap.Confirm.Matches(key) || model.keymap.Back.Matches(key) {
		model.state = StateTargets
		model.cursor = len(SupportedTargets())
	}
	return model, nil
}

func (model *Model) toggle(target modelpkg.AgentID) {
	for index, selected := range model.targets {
		if selected == target {
			model.targets = append(model.targets[:index], model.targets[index+1:]...)
			return
		}
	}
	model.targets = append(model.targets, target)
}

func sameRequest(left, right runtime.ActionRequest) bool {
	if left.Action != right.Action || left.RootDir != right.RootDir || len(left.Targets) != len(right.Targets) {
		return false
	}
	for index := range left.Targets {
		if left.Targets[index] != right.Targets[index] {
			return false
		}
	}
	return true
}

// View renders the active uninstall step.
func (model Model) View() string {
	switch model.state {
	case StateConfirmation:
		return model.confirmationView()
	case StateResult:
		return model.shell("return", model.resultBody())
	default:
		return model.shell("select", model.targetsBody())
	}
}

func (model Model) shell(enterLabel, body string) string {
	return ui.Shell(ui.Frame{
		Body:       body,
		Keys:       model.keymap,
		Extra:      []keys.Binding{model.keymap.Back},
		EnterLabel: enterLabel,
	})
}

func (model Model) targetsBody() string {
	items := make([]ui.Item, 0, len(SupportedTargets()))
	for _, target := range SupportedTargets() {
		items = append(items, ui.Item{Label: string(target), Checked: slices.Contains(model.targets, target)})
	}
	var view strings.Builder
	view.WriteString(styles.TitleStyle.Render("Uninstall managed configuration"))
	view.WriteString("\n\n")
	view.WriteString(styles.SubtextStyle.Render("Select the targets to remove."))
	view.WriteString("\n\n")
	view.WriteString(ui.CheckList(items, model.cursor, 0))
	view.WriteString(ui.Options([]string{"Continue"}, model.cursor-len(items), 0))
	return view.String()
}

func (model Model) confirmationView() string {
	var view strings.Builder
	view.WriteString(styles.TitleStyle.Render("Confirm uninstall"))
	view.WriteString("\n\n")
	view.WriteString(styles.WarningStyle.Render("This removes Takt-managed configuration for:"))
	for _, target := range model.targets {
		view.WriteString("\n")
		view.WriteString(styles.UnselectedStyle.Render("  • " + string(target)))
	}
	view.WriteString("\n\n")
	view.WriteString(styles.SubtextStyle.Render("Locally modified managed files may be preserved."))
	if model.busy {
		view.WriteString("\n\n")
		view.WriteString(theme.Caption.Render(ui.Spinner(model.frame) + " "))
		view.WriteString(styles.WarningStyle.Render("Removing managed configuration..."))
		return view.String()
	}
	view.WriteString("\n\n")
	view.WriteString(ui.Options([]string{"Uninstall", "Cancel"}, model.cursor, 0))
	return ui.Shell(ui.Frame{Body: view.String(), Keys: model.keymap, EnterLabel: "confirm"})
}

func (model Model) resultBody() string {
	var view strings.Builder
	view.WriteString(styles.TitleStyle.Render("Uninstall result"))
	view.WriteString("\n\n")
	if model.err != nil {
		view.WriteString(styles.ErrorStyle.Render(theme.Icon.Cross + " Uninstall failed: " + model.err.Error()))
	} else {
		view.WriteString(styles.SuccessStyle.Render(theme.Icon.Check + " Uninstall complete"))
		view.WriteString("\n")
		view.WriteString(styles.UnselectedStyle.Render(fmt.Sprintf("Removed: %s", paths(model.result.Removed))))
		view.WriteString("\n")
		view.WriteString(styles.UnselectedStyle.Render(fmt.Sprintf("Preserved: %s", paths(model.result.Preserved))))
	}
	return view.String()
}

func paths(items []string) string {
	if len(items) == 0 {
		return "none"
	}
	return strings.Join(items, ", ")
}
