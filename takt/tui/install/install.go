// Package install provides the focused install selection flow.
package install

import (
	"fmt"
	"slices"
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

// Step identifies the currently visible install screen.
type Step int

const (
	StepTargets Step = iota
	StepClaudeModels
	StepCodexModels
	StepSetupChoice
	StepComponents
	StepReview
	StepConfirmation
	StepResult
	StepCanceled
)

var supportedTargets = []model.AgentID{
	model.AgentClaudeCode,
	model.AgentCodex,
	model.AgentOpenCode,
}

// setupChoices are the setup modes offered after the model steps.
var setupChoices = []string{"Default", "Custom"}

// componentChoices lists the optional artifact-only components in canonical
// order. setup.ValidateComponents is the planning-side gate for the same
// names; ComponentEngram needs runtime actions and skills deploy
// unconditionally, so neither is offered.
var componentChoices = []model.ComponentID{
	model.ComponentContext7,
	model.ComponentPermission,
	model.ComponentTheme,
	model.ComponentClaudeTheme,
	model.ComponentOpenCodeTaktLogo,
}

var spaceToggle = keys.Binding{Keys: []string{" "}, Key: "space", Description: "toggle"}

var claudeModels = []model.ClaudeModelAlias{
	model.ClaudeModelFable,
	model.ClaudeModelOpus,
	model.ClaudeModelSonnet,
	model.ClaudeModelHaiku,
}

var codexEfforts = []string{"low", "medium", "high"}

// SupportedTargets returns the targets this flow can install.
func SupportedTargets() []model.AgentID { return append([]model.AgentID(nil), supportedTargets...) }

// ClaudeModels returns the canonical Claude model aliases displayed by the flow.
func ClaudeModels() []model.ClaudeModelAlias {
	return append([]model.ClaudeModelAlias(nil), claudeModels...)
}

// CodexModels returns the canonical Codex model IDs displayed by the flow.
func CodexModels() []string { return model.CodexAvailableModels() }

// CodexEfforts returns the effort levels accepted by the canonical Codex validation.
func CodexEfforts() []string { return append([]string(nil), codexEfforts...) }

// Model is a standalone Bubble Tea install flow. Execution remains at the runtime
// boundary: this model emits ActionRequest and never plans or writes files.
type Model struct {
	rootDir      string
	step         Step
	cursor       int
	targets      []model.AgentID
	setupCustom  bool
	components   []model.ComponentID
	busy         bool
	frame        int
	presentation common.Presentation
	keymap       keys.KeyMap
}

// New creates an install flow for rootDir.
func New(rootDir string) Model { return Model{rootDir: rootDir, keymap: keys.Default()} }

// Init has no startup work.
func (Model) Init() tea.Cmd { return nil }

// Step returns the visible screen.
func (m Model) Step() Step { return m.step }

// Targets returns the explicitly selected install targets.
func (m Model) Targets() []model.AgentID { return append([]model.AgentID(nil), m.targets...) }

// Update advances the flow or emits an install action after explicit confirmation.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case runtime.ActionResultMsg:
		return m.result(message), nil
	case runtime.TickMsg:
		if !m.busy {
			return m, nil
		}
		m.frame = message.Frame
		return m, runtime.Tick(message.Frame)
	case common.AcceptConfirmation:
		if m.step == StepConfirmation && !m.busy {
			m.busy = true
			request := runtime.ActionRequest{
				Action:     runtime.ActionInstall,
				RootDir:    m.rootDir,
				Targets:    m.Targets(),
				Components: m.componentNames(),
			}
			return m, tea.Batch(func() tea.Msg { return request }, runtime.Tick(0))
		}
	case common.CancelConfirmation:
		if m.step == StepConfirmation {
			m.step = StepReview
		}
		return m, nil
	case tea.KeyMsg:
		return m.key(message)
	}
	return m, nil
}

func (m Model) key(message tea.Msg) (tea.Model, tea.Cmd) {
	switch {
	case m.keymap.Up.Matches(message):
		if rows := m.cursorRows(); rows > 0 {
			m.cursor = ui.MoveCursor(m.cursor, rows, -1)
		}
	case m.keymap.Down.Matches(message):
		if rows := m.cursorRows(); rows > 0 {
			m.cursor = ui.MoveCursor(m.cursor, rows, 1)
		}
	case spaceToggle.Matches(message):
		if m.step == StepComponents && m.cursor < len(componentChoices) {
			m.components = common.Toggle(m.components, componentChoices[m.cursor])
		}
	case m.keymap.Confirm.Matches(message):
		return m.confirm()
	case m.keymap.Back.Matches(message):
		m.back()
	}
	return m, nil
}

// cursorRows returns the number of selectable rows on cursor-driven steps.
func (m Model) cursorRows() int {
	switch m.step {
	case StepTargets:
		return len(supportedTargets) + 1
	case StepSetupChoice:
		return len(setupChoices)
	case StepComponents:
		return len(componentChoices) + 1
	default:
		return 0
	}
}

func (m Model) confirm() (tea.Model, tea.Cmd) {
	switch m.step {
	case StepTargets:
		if m.cursor < len(supportedTargets) {
			m.targets = common.Toggle(m.targets, supportedTargets[m.cursor])
		} else if len(m.targets) > 0 {
			m.step, m.cursor = m.nextModelStep(), 0
		}
	case StepClaudeModels, StepCodexModels:
		m.step, m.cursor = m.nextModelStep(), 0
	case StepSetupChoice:
		m.setupCustom = m.cursor == 1
		m.step, m.cursor = StepReview, 0
		if m.setupCustom {
			m.step = StepComponents
		}
	case StepComponents:
		if m.cursor < len(componentChoices) {
			m.components = common.Toggle(m.components, componentChoices[m.cursor])
		} else {
			m.step, m.cursor = StepReview, 0
		}
	case StepReview:
		m.step = StepConfirmation
	case StepConfirmation:
		return m.Update(common.AcceptConfirmation{})
	case StepResult, StepCanceled:
		m.step, m.cursor, m.setupCustom, m.components = StepTargets, 0, false, nil
	}
	return m, nil
}

// componentNames returns the selected component names in canonical order, or
// nil for the default setup.
func (m Model) componentNames() []string {
	if !m.setupCustom {
		return nil
	}
	names := make([]string, 0, len(m.components))
	for _, component := range componentChoices {
		if slices.Contains(m.components, component) {
			names = append(names, string(component))
		}
	}
	return names
}

func (m Model) nextModelStep() Step {
	if m.step <= StepTargets && slices.Contains(m.targets, model.AgentClaudeCode) {
		return StepClaudeModels
	}
	if m.step <= StepClaudeModels && slices.Contains(m.targets, model.AgentCodex) {
		return StepCodexModels
	}
	return StepSetupChoice
}

// prevModelStep returns the screen before the setup choice.
func (m Model) prevModelStep() Step {
	if slices.Contains(m.targets, model.AgentCodex) {
		return StepCodexModels
	}
	if slices.Contains(m.targets, model.AgentClaudeCode) {
		return StepClaudeModels
	}
	return StepTargets
}

func (m *Model) back() {
	switch m.step {
	case StepClaudeModels, StepCanceled:
		m.step = StepTargets
	case StepCodexModels:
		if slices.Contains(m.targets, model.AgentClaudeCode) {
			m.step = StepClaudeModels
		} else {
			m.step = StepTargets
		}
	case StepSetupChoice:
		m.step = m.prevModelStep()
	case StepComponents, StepReview:
		m.step = StepSetupChoice
	case StepConfirmation:
		if !m.busy {
			m.step = StepReview
		}
	case StepResult:
		m.step = StepReview
	case StepTargets:
		m.step = StepCanceled
	}
	m.cursor = 0
}

func (m Model) result(message runtime.ActionResultMsg) Model {
	m.step = StepResult
	m.busy = false
	if message.Err != nil {
		m.presentation = common.Failure(message.Err)
		return m
	}
	m.presentation = common.Success(fmt.Sprintf("Install complete: %d changed, %d unchanged.", len(message.Result.Changed), len(message.Result.Unchanged)))
	return m
}

// View renders the current screen with a visible step and keyboard guidance.
func (m Model) View() string {
	switch m.step {
	case StepTargets:
		return m.shell("toggle", m.targetsBody())
	case StepClaudeModels:
		return m.shell("continue", optionsBody("2/7 · Claude models", "Installation uses the canonical per-agent defaults.", aliases(ClaudeModels())))
	case StepCodexModels:
		return m.shell("continue", optionsBody("3/7 · Codex models", "Installation uses the canonical per-agent defaults.", append(CodexModels(), "Effort: "+strings.Join(CodexEfforts(), ", "))))
	case StepSetupChoice:
		return m.shell("select", m.setupChoiceBody())
	case StepComponents:
		return m.shell("toggle", m.componentsBody(), spaceToggle)
	case StepReview:
		return m.shell("review", m.reviewBody())
	case StepConfirmation:
		if m.busy {
			return m.busyView()
		}
		return m.shell("install", optionsBody("7/7 · Confirm install", "Install Takt for: "+joinTargets(m.targets), nil))
	case StepResult:
		if m.presentation.Err != nil {
			return m.shell("return", styles.ErrorStyle.Render(theme.Icon.Cross+" Install failed: "+m.presentation.Err.Error()))
		}
		return m.shell("return", styles.SuccessStyle.Render(theme.Icon.Check+" "+m.presentation.Message))
	default:
		return m.shell("return", optionsBody("Install cancelled", "No install action was requested.", nil))
	}
}

func (m Model) shell(enterLabel, body string, extra ...keys.Binding) string {
	return common.Shell(m.keymap, enterLabel, body, append([]keys.Binding{m.keymap.Back}, extra...)...)
}

func (m Model) targetsBody() string {
	items := make([]ui.Item, len(supportedTargets))
	for index, target := range supportedTargets {
		items[index] = ui.Item{Label: string(target), Checked: slices.Contains(m.targets, target)}
	}
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("1/7 · Select install targets"))
	b.WriteString("\n\n")
	b.WriteString(ui.CheckList(items, m.cursor, 0))
	b.WriteString(ui.Options([]string{"Continue"}, m.cursor-len(supportedTargets), 0))
	return b.String()
}

func (m Model) setupChoiceBody() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("4/7 · Choose setup"))
	b.WriteString("\n\n")
	b.WriteString(styles.UnselectedStyle.Render("Default installs the canonical stack. Custom adds the optional components you pick."))
	b.WriteString("\n\n")
	b.WriteString(ui.Options(setupChoices, m.cursor, 0))
	return b.String()
}

func (m Model) componentsBody() string {
	items := make([]ui.Item, len(componentChoices))
	for index, component := range componentChoices {
		items[index] = ui.Item{Label: string(component), Checked: slices.Contains(m.components, component)}
	}
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("5/7 · Select optional components"))
	b.WriteString("\n\n")
	b.WriteString(styles.UnselectedStyle.Render("All components are optional; continue with none selected to skip them."))
	b.WriteString("\n\n")
	b.WriteString(ui.CheckList(items, m.cursor, 0))
	b.WriteString(ui.Options([]string{"Continue"}, m.cursor-len(componentChoices), 0))
	return b.String()
}

func (m Model) reviewBody() string {
	details := []string{
		"Setup: " + m.setupSummary(),
		"Claude: canonical defaults (" + strings.Join(aliases(ClaudeModels()), ", ") + ")",
		"Codex: canonical defaults (" + strings.Join(CodexModels(), ", ") + ")",
		"OpenCode: stage default",
	}
	return optionsBody(
		"6/7 · Review install plan",
		"Targets: "+joinTargets(m.targets),
		details,
	)
}

// setupSummary describes the setup choice for the review screen.
func (m Model) setupSummary() string {
	if !m.setupCustom {
		return "default"
	}
	names := m.componentNames()
	if len(names) == 0 {
		return "custom · none"
	}
	return "custom · " + strings.Join(names, ", ")
}

func (m Model) busyView() string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render("7/7 · Installing"))
	b.WriteString("\n\n")
	b.WriteString(styles.UnselectedStyle.Render("Install request sent. Waiting for the result."))
	b.WriteString("\n")
	b.WriteString(theme.Caption.Render(ui.Spinner(m.frame) + " "))
	b.WriteString(styles.SubtextStyle.Render("Waiting for result"))
	return b.String()
}

func optionsBody(title, description string, options []string) string {
	var b strings.Builder
	b.WriteString(styles.TitleStyle.Render(title))
	b.WriteString("\n\n")
	b.WriteString(styles.UnselectedStyle.Render(description))
	if len(options) > 0 {
		b.WriteString("\n\n")
		for _, option := range options {
			b.WriteString(styles.UnselectedStyle.Render("  " + option))
			b.WriteString("\n")
		}
	}
	return b.String()
}

func aliases(values []model.ClaudeModelAlias) []string {
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.String()
	}
	return result
}

func joinTargets(targets []model.AgentID) string {
	values := make([]string, len(targets))
	for index, target := range targets {
		values[index] = string(target)
	}
	return strings.Join(values, ", ")
}
