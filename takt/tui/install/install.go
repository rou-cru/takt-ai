// Package install provides the focused install selection flow.
package install

import (
	"cmp"
	"fmt"
	"maps"
	"os/exec"
	"path"
	"slices"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// Step identifies the currently visible install screen.
type Step int

// Steps are the install flow's screens, in walk order.
const (
	// StepComponents is the optional component checklist, opened from review
	// through Personalize.
	StepComponents Step = iota
	// StepConflicts lets the user choose, per conflicting file, whether to
	// keep their own version or restore Takt's; it is reached only when a
	// conflict needs a decision.
	StepConflicts
	// StepReview is the commitment point: scope, change categories and the
	// Install / Apply changes action. There is no separate confirmation.
	StepReview
	// StepResult is the post-install result screen.
	StepResult
)

const (
	// conflictChoiceCount is the keep/restore choice pair for each conflict.
	conflictChoiceCount = 2
)

// OpenModelsMsg signals the user chose "Assign models" from the post-install
// result screen and wants the standalone model-assignment flow.
type OpenModelsMsg struct{}

// lookPath is the executable lookup seam (same pattern as doctor.go and
// setup.ProviderRuntime): tests stub it to fake an OpenCode on PATH.
var lookPath = exec.LookPath

// openCodeOnPath reports whether the OpenCode CLI is on PATH. Its absence is
// a notice, never a block: installing configuration needs no binary.
func openCodeOnPath() bool {
	_, err := lookPath("opencode")
	return err == nil
}

// componentPurpose returns the user-facing purpose for a component.
func componentPurpose(id model.ComponentID) string {
	return catalog.ComponentPurpose(id)
}

const (
	// actionPersonalize returns to the component checklist to fix choices.
	actionPersonalize = ui.TextActionPersonalize
	// actionInstall commits a fresh installation.
	actionInstall = ui.TextActionInstall
	// actionApply commits configuration changes.
	actionApply = ui.TextActionApplyChanges
	// actionRetry re-reviews the same choices after a failure.
	actionRetry = ui.TextActionRetry
	// actionReview re-reviews the actual files after a stop.
	actionReview = ui.TextActionReviewAgain
	// actionKeep leaves the current state after a partial result.
	actionKeep = ui.TextActionKeepCurrent
	// actionAssign opens the standalone model-assignment flow.
	actionAssign = ui.TextActionAssignModels
	// actionMenu returns to the menu without changing anything.
	actionMenu = ui.TextActionBackToMenu
	// actionQuit exits the program.
	actionQuit = ui.TextActionQuit
)

// Model is a standalone Bubble Tea install flow.
type Model struct {
	rootDir       string
	configuring   bool
	scroll        int
	width, height int
	step          Step
	cursor        int
	focus         ui.Section
	notice        string
	setupCustom   bool
	components    []model.ComponentID
	plan          runtime.InstallPlan
	previewErr    error
	restorePaths  []string
	run           runtime.Run
	outcome       runtime.ActionResultMsg
	keymap        keys.KeyMap
	initial       initialSelection
}

// initialSelection is the draft baseline for Dirty.
type initialSelection struct {
	components []model.ComponentID
}

// New creates an install flow for rootDir.
func New(rootDir string) Model {
	m := Model{rootDir: rootDir, keymap: keys.Default(), run: runtime.NewRun()}
	allComponents, err := setup.AllComponents()
	if err != nil {
		m.notice = fmt.Sprintf("Component catalog unavailable: %s", err)
		return m
	}
	m.components = allComponents
	if installed, err := setup.LoadInstalledConfig(rootDir); err == nil {
		m.configuring = true
		m.components, _ = setup.ValidateComponents(installed.Components)
		m.setupCustom = true
	} else if !openCodeOnPath() {
		m.notice = ui.TextOpenCodeNotFound
	}
	m.initial = initialSelection{components: append([]model.ComponentID(nil), m.components...)}
	// The flow opens on the prepared plan: review (or the files needing a
	// decision first) is the starting point, Personalize the way to change it.
	m.step = m.preparePlan()
	m.cursor = m.cursorFor(m.step)
	return m
}

// Init has no startup work.
func (Model) Init() tea.Cmd { return nil }

// Run exposes the flow's action state so the shell gates quit and cancel.
func (m Model) Run() runtime.Run { return m.run }

// Step returns the visible screen.
func (m Model) Step() Step { return m.step }

// Dirty reports unapplied selections that differ from the flow's baseline.
func (m Model) Dirty() bool {
	if m.step == StepResult || m.run.Busy() {
		return false
	}
	return !sameSet(m.components, m.initial.components) || len(m.restorePaths) > 0
}

// sameSet ignores order.
func sameSet[T comparable](left, right []T) bool {
	if len(left) != len(right) {
		return false
	}
	for _, item := range left {
		if !slices.Contains(right, item) {
			return false
		}
	}
	return true
}

// Update advances the flow or emits an install action from the review.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = message.Width, message.Height
	case runtime.ActionResultMsg:
		if message, ok := m.run.Result(message); ok {
			m.outcome = message
			return m.apply(eventResult)
		}
	case runtime.ActionProgressMsg:
		if updated, ok := m.run.AcceptProgress(message); ok {
			m.run = updated
		}
	case runtime.CancelRequest:
		m.run = m.run.Cancel(message)
	case spinner.TickMsg:
		// Same-screen animation only: no navigation, no table event.
		var cmd tea.Cmd
		m.run, cmd = m.run.Tick(message)
		return m, cmd
	case tea.KeyPressMsg:
		return m.key(message)
	}
	return m, nil
}

// key handles same-screen interaction and maps Confirm and Back to table events.
func (m Model) key(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.run.Busy() {
		return m, nil
	}
	if m.scrollKey(message) {
		return m, nil
	}
	return m.actionKey(message)
}

func (m *Model) scrollKey(message tea.KeyPressMsg) bool {
	scroll, ok := ui.Scroll(m.scroll, m.height, message.String())
	if ok {
		m.scroll = scroll
	}
	return ok
}

func (m Model) actionKey(message tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.step == StepConflicts && m.keymap.Confirm.Matches(message) {
		// PR-UX-15: Enter records the file's choice and moves to the next
		// file; on the last one it advances, unless a kept file cannot work.
		m.choose()
		file := m.cursor / conflictChoiceCount
		if file+1 < len(m.decisions()) {
			m.cursor = (file + 1) * conflictChoiceCount
			if slices.Contains(m.restorePaths, m.decisions()[file+1].Path) {
				m.cursor++
			}
			return m, nil
		}
		return m.confirm()
	}
	if m.step == StepReview || m.step == StepResult {
		if ui.NudgeHorizontal(&m.cursor, m.rows(), m.keymap, message) {
			return m, nil
		}
		switch {
		case m.keymap.Confirm.Matches(message):
			return m.confirm()
		case m.keymap.Back.Matches(message):
			return m.back()
		default:
			return m, nil
		}
	}
	if m.keymap.Toggle.Matches(message) {
		return m.toggleKey()
	}
	switch {
	case m.keymap.Up.Matches(message):
		m.moveCursor(-1)
	case m.keymap.Down.Matches(message):
		m.moveCursor(1)
	case m.keymap.Confirm.Matches(message):
		return m.confirm()
	case m.keymap.Back.Matches(message):
		return m.back()
	}
	return m, nil
}

func (m Model) toggleKey() (tea.Model, tea.Cmd) {
	if m.isChecklist() && m.focus == ui.SectionBody {
		m.toggle()
	}
	return m, nil
}

// isChecklist reports whether the step is a Space-toggled checklist.
func (m Model) isChecklist() bool {
	return m.step == StepComponents || m.step == StepConflicts
}

// rows returns the number of cursor positions: checklist rows, setup choices
// or the footer actions of review and result.
func (m Model) rows() int {
	switch m.step {
	case StepComponents:
		return m.customChoiceRows()
	case StepConflicts:
		return conflictChoiceCount * len(m.decisions())
	case StepReview:
		return len(m.reviewActions())
	case StepResult:
		return len(m.resultActions())
	}
	return 0
}

// moveCursor moves the cursor within the step's rows.
func (m *Model) moveCursor(delta int) {
	rows := m.rows()
	if rows > 0 {
		m.cursor = ui.MoveCursor(m.cursor, rows, delta)
	}
}

// toggle clones before ui.Toggle.
func (m *Model) toggle() {
	switch m.step {
	case StepComponents:
		choices := m.applicableComponents()
		if m.cursor < len(choices) {
			m.components = ui.Toggle(slices.Clone(m.components), choices[m.cursor])
			return
		}
	}
}

// choose applies the focused single-selector row.
func (m *Model) choose() {
	decisions := m.decisions()
	if m.cursor/2 >= len(decisions) {
		return
	}
	path := decisions[m.cursor/2].Path
	m.restorePaths = slices.DeleteFunc(slices.Clone(m.restorePaths), func(p string) bool { return p == path })
	if m.cursor%2 == 1 {
		m.restorePaths = append(m.restorePaths, path)
	}
}

// decisions are the conflicts the user must resolve, skipping files
// unrelated to the operation or already accepted unchanged.
func (m Model) decisions() []setup.ConflictEntry {
	return slices.DeleteFunc(slices.Clone(m.plan.Conflicts), func(c setup.ConflictEntry) bool {
		return c.Impact == setup.ImpactUnrelated || c.Accepted
	})
}

// keptConflicts are the conflicts whose on-disk version the operation keeps.
func (m Model) keptConflicts() []setup.ConflictEntry {
	return slices.DeleteFunc(slices.Clone(m.plan.Conflicts), func(c setup.ConflictEntry) bool { return slices.Contains(m.restorePaths, c.Path) })
}

// blocked returns the first kept file that cannot work with the operation,
// which is never applied as valid.
func (m Model) blocked() (setup.ConflictEntry, bool) {
	for _, conflict := range m.keptConflicts() {
		if conflict.Impact == setup.ImpactIncompatible {
			return conflict, true
		}
	}
	return setup.ConflictEntry{}, false
}

// kept reports whether path is a conflict whose on-disk version is kept.
func (m Model) kept(path string) bool {
	return slices.ContainsFunc(m.keptConflicts(), func(c setup.ConflictEntry) bool { return c.Path == path })
}

// uncertainKept are the kept files whose capability Takt cannot guarantee.
func (m Model) uncertainKept() []setup.ConflictEntry {
	return slices.DeleteFunc(m.keptConflicts(), func(c setup.ConflictEntry) bool { return c.Impact != setup.ImpactUncertain })
}

const (
	// eventConfirm applies the current step's primary action.
	eventConfirm = "confirm"
	// eventBack returns to the previous step.
	eventBack = "back"
	// eventResult shows the install outcome.
	eventResult = "result"
)

// installTransitions declares every screen transition of the install flow;
// toggling and cursor movement stay outside it.
func installTransitions() ui.Table[Step, Model] {
	return ui.Table[Step, Model]{
		{From: StepComponents, Event: eventConfirm}: func(m *Model) (Step, tea.Cmd) {
			return m.preparePlan(), nil
		},
		{From: StepConflicts, Event: eventConfirm}: installConflictsConfirm,
		{From: StepReview, Event: eventConfirm}:    installReviewConfirm,
		{From: StepReview, Event: eventResult}: func(m *Model) (Step, tea.Cmd) {
			m.run = m.run.End()
			return StepResult, nil
		},
		{From: StepResult, Event: eventConfirm}: installResultConfirm,
		// Back from the checklist returns to review with the choices kept.
		{From: StepComponents, Event: eventBack}: func(m *Model) (Step, tea.Cmd) {
			return m.preparePlan(), nil
		},
		{From: StepConflicts, Event: eventBack}: installConflictsBack,
		{From: StepReview, Event: eventBack}:    installReviewBack,
		{From: StepResult, Event: eventBack}: func(m *Model) (Step, tea.Cmd) {
			return StepResult, func() tea.Msg { return ui.BackMsg{} }
		},
	}
}

func installConflictsConfirm(m *Model) (Step, tea.Cmd) {
	if _, blocked := m.blocked(); blocked {
		return StepConflicts, nil
	}
	return StepReview, nil
}
func installReviewConfirm(m *Model) (Step, tea.Cmd) {
	if m.reviewActions()[m.cursor] == actionPersonalize {
		m.setupCustom = true
		return StepComponents, nil
	}
	return StepReview, m.start()
}
func installResultConfirm(m *Model) (Step, tea.Cmd) {
	switch m.resultActions()[m.cursor] {
	case actionRetry, actionReview:
		return m.preparePlan(), nil
	case actionAssign:
		return StepResult, func() tea.Msg { return OpenModelsMsg{} }
	case actionMenu, actionKeep:
		return StepResult, func() tea.Msg { return ui.BackMsg{} }
	case actionQuit:
		return StepResult, tea.Quit
	}
	return StepResult, nil
}
func installReviewBack(m *Model) (Step, tea.Cmd) {
	if len(m.decisions()) > 0 && m.previewErr == nil {
		return StepConflicts, nil
	}
	return StepReview, func() tea.Msg { return ui.BackMsg{} }
}

// installConflictsBack returns to the checklist when Personalize led here,
// otherwise leaves the flow: conflicts are then its first step.
func installConflictsBack(m *Model) (Step, tea.Cmd) {
	if m.setupCustom && !m.configuring {
		return StepComponents, nil
	}
	return StepConflicts, func() tea.Msg { return ui.BackMsg{} }
}

// confirm advances the flow for the confirm action.
func (m Model) confirm() (tea.Model, tea.Cmd) { return m.apply(eventConfirm) }

// back moves the flow back one step.
func (m Model) back() (tea.Model, tea.Cmd) { return m.apply(eventBack) }

// apply runs one table rule and, on a step change, places the cursor on the
// step's current value: the chosen setup mode, or the review's commit action.
func (m Model) apply(event string) (tea.Model, tea.Cmd) {
	previous := m.step
	next, cmd, _ := installTransitions().Apply(&m, m.step, event)
	if next != previous {
		m.step, m.scroll, m.focus = next, 0, ui.SectionBody
		m.cursor = m.cursorFor(next)
	}
	return m, cmd
}

// cursorFor places the cursor on a step's current value when it is entered:
// the review's commit action, or the first row elsewhere.
func (m Model) cursorFor(step Step) int {
	if step == StepReview {
		return len(m.reviewActions()) - 1
	}
	return 0
}

// preparePlan resolves the plan without changing anything.
func (m *Model) preparePlan() Step {
	m.plan, m.previewErr = runtime.Adapter{}.PreviewPlan(runtime.PreviewRequest{
		Action:     runtime.ActionInstall,
		RootDir:    m.rootDir,
		Components: m.componentNames(),
	})
	decisions := m.decisions()
	m.restorePaths = slices.DeleteFunc(slices.Clone(m.restorePaths), func(path string) bool {
		return !slices.ContainsFunc(decisions, func(c setup.ConflictEntry) bool { return c.Path == path })
	})
	if m.previewErr == nil && len(decisions) > 0 {
		return StepConflicts
	}
	return StepReview
}

// start emits the install request once.
func (m *Model) start() tea.Cmd {
	request := runtime.ActionRequest{
		ID:         runtime.NextID(),
		Action:     runtime.ActionInstall,
		RootDir:    m.rootDir,
		Components: m.componentNames(),
	}
	for _, conflict := range m.keptConflicts() {
		request.PreservePaths = append(request.PreservePaths, conflict.Path)
	}
	for _, conflict := range m.uncertainKept() {
		request.AcceptedRisks = append(request.AcceptedRisks, setup.RiskAcceptance{Path: conflict.Path, SHA256: conflict.SHA256, Impact: conflict.Impact})
	}
	m.run = m.run.Start(request)
	return func() tea.Msg { return request }
}

// reviewActions lists the review screen's commit actions in order.
func (m Model) reviewActions() []string {
	if m.previewErr != nil {
		return []string{actionPersonalize}
	}
	if m.configuring {
		return []string{actionPersonalize, actionApply}
	}
	return []string{actionPersonalize, actionInstall}
}

// resultActions lists the result screen's actions for the current outcome.
func (m Model) resultActions() []string {
	if m.outcome.Err != nil {
		return []string{actionRetry, actionMenu, actionQuit}
	}
	// No rollback exists: a stop offers keeping the current state or a fresh
	// review of the actual files.
	if m.outcome.Result.Partial() {
		return []string{actionReview, actionKeep, actionQuit}
	}
	if m.outcome.Result.Cancelled() {
		return []string{actionReview, actionMenu, actionQuit}
	}
	return []string{actionAssign, actionMenu, actionQuit}
}

// applicableComponents lists the optional components the user can choose.
func (m Model) applicableComponents() []model.ComponentID {
	return []model.ComponentID{model.ComponentContext7}
}

// componentNames returns the selected component names.
func (m Model) componentNames() []string {
	names := []string{}
	for _, c := range m.applicableComponents() {
		if !m.setupCustom || slices.Contains(m.components, c) {
			names = append(names, string(c))
		}
	}
	return names
}

// customChoiceRows counts the component checklist rows.
func (m Model) customChoiceRows() int {
	return len(m.applicableComponents())
}

// Title identifies the task and current step for the stable header region.
func (m Model) Title() string {
	task := ui.TextTitleInstall
	if m.configuring {
		task = ui.TextTitleConfigure
	}
	step := map[Step]string{StepComponents: ui.TextStepComponents, StepConflicts: ui.TextStepExisting, StepReview: ui.TextStepReview, StepResult: ui.TextStepResult}[m.step]
	if m.run.Busy() {
		step = map[bool]string{false: ui.TextStepInstalling, true: ui.TextStepApplying}[m.configuring]
	}
	return task + " · " + step
}

// View renders the current screen with a visible step and keyboard guidance.
func (m Model) View() tea.View {
	frame := ui.Frame{Header: m.Title(), Width: m.width, Height: m.height, Scroll: m.scroll}
	switch {
	case m.run.Busy():
		operation := map[bool]string{false: ui.TextBusyInstallation, true: ui.TextBusyConfiguration}[m.configuring]
		frame.Body, frame.Footer = ui.Busy(operation, m.run.SpinView(), m.run.ProgressView()), ui.BusyFooter(m.run.CancelRequested)
	case m.step == StepConflicts:
		frame.Body = m.conflictsBody()
	case m.step == StepComponents:
		frame.Body = m.componentsBody()
	case m.step == StepReview:
		actions := m.reviewActions()
		frame.Body, frame.Footer = m.reviewBody(), ui.FooterActions(ui.Actions(actions...), m.cursor, true)
	case m.step == StepResult:
		frame.Body, frame.Footer = m.resultBody(), ui.FooterActions(ui.Actions(m.resultActions()...), m.cursor, true)
	}
	return tea.NewView(ui.Shell(frame))
}

// componentsBody renders the optional component checklist.
func (m Model) componentsBody() string {
	choices := m.applicableComponents()
	items := make([]ui.Item, 0, len(choices)+1)
	for _, component := range choices {
		name, description, _ := strings.Cut(componentPurpose(component), " — ")
		items = append(items, ui.Item{Label: name, Description: description, Checked: slices.Contains(m.components, component)})
	}
	return theme.Label.Render(ui.TextComponentsIntro) + "\n\n" +
		ui.CheckList(items, m.cursor, m.focus == ui.SectionBody)
}

// conflictSources explains where each conflicting file came from.
var conflictSources = map[string]string{
	"pre-existing": ui.TextConflictPreexisting,
	"user-edited":  ui.TextConflictEdited,
	"missing":      ui.TextConflictMissing,
}

// conflictsBody explains each undecided file and offers its keep/restore
// choice; decided files are listed once, collapsed.
func (m Model) conflictsBody() string {
	var b strings.Builder
	b.WriteString(theme.Label.Render(ui.TextConflictsIntro) + "\n")
	for index, conflict := range m.decisions() {
		b.WriteString(m.conflictLines(index, conflict))
	}
	var accepted, unrelated []string
	for _, conflict := range m.plan.Conflicts {
		switch {
		case conflict.Impact == setup.ImpactUnrelated:
			unrelated = append(unrelated, conflict.Path)
		case conflict.Accepted:
			accepted = append(accepted, conflict.Path+" ("+conflict.Affects+")")
		}
	}
	if len(accepted) > 0 {
		b.WriteString("\n")
		section(&b, ui.TextPreviouslyKeptTitle, accepted)
	}
	if len(unrelated) > 0 {
		b.WriteString("\n")
		section(&b, ui.TextNotAffectedTitle, unrelated)
	}
	// A kept file that cannot work holds the flow here; say why.
	if conflict, blocked := m.blocked(); blocked {
		b.WriteString("\n" + theme.DangerText.Render(fmt.Sprintf(ui.TextKeepCannotWorkFmt, conflict.Path, conflict.Alternative)))
	}
	return b.String()
}

func (m Model) conflictLines(index int, conflict setup.ConflictEntry) string {
	text := "\n" + theme.Strong.Render(conflict.Path) + "\n" + theme.Label.Render(ui.TextConflictSource+conflictSources[conflict.Reason]) + "\n" + theme.Label.Render(ui.TextConflictAffects+conflict.Affects) + "\n"
	if conflict.Impact == setup.ImpactIncompatible {
		text += theme.DangerText.Render(ui.TextConflictKnown+conflict.Consequence) + "\n" + theme.Label.Render(ui.TextConflictAlternative+conflict.Alternative) + "\n"
	} else {
		text += theme.WarningText.Render(ui.TextConflictUncertain+conflict.Consequence) + "\n" + theme.Label.Render(ui.TextConflictRecommend) + "\n"
	}
	cursor := -1
	if m.cursor/conflictChoiceCount == index {
		cursor = m.cursor % conflictChoiceCount
	}
	chosen := 0
	if slices.Contains(m.restorePaths, conflict.Path) {
		chosen = 1
	}
	return text + ui.Selector([]string{ui.TextKeepMine, ui.TextRestoreTakt}, cursor, chosen, cursor >= 0)
}

// reviewBody states what the install sets up in the user's terms (agents,
// skills, MCP servers, integrations) and which of their files it touches, as
// a title and two groups; an unresolvable plan offers no commit action.
func (m Model) reviewBody() string {
	if m.previewErr != nil {
		return m.reviewError()
	}
	var b strings.Builder
	if m.notice != "" {
		b.WriteString(ui.Status(ui.StateWarning, m.notice) + "\n\n")
	}
	added := len(slices.DeleteFunc(slices.Clone(m.plan.Add), m.kept))
	updated := len(slices.DeleteFunc(slices.Clone(m.plan.Modify), m.kept))
	if added+updated == 0 {
		b.WriteString(theme.Label.Render(ui.TextNothingToChange) + "\n\n")
	}
	title := ui.TextReviewInstallTitle
	if m.configuring {
		title = ui.TextReviewApplyTitle
	}
	b.WriteString(theme.Title.Render(title) + "\n" + theme.Caption.Render(ui.TextReviewScope) + "\n\n")
	var groups []string
	if installs := m.installRows(); len(installs) > 0 {
		groups = append(groups, ui.Group(ui.TextGroupInstalls, installs))
	}
	if disk := m.diskRows(added, updated); len(disk) > 0 {
		groups = append(groups, ui.Group(ui.TextGroupDisk, disk))
	}
	b.WriteString(strings.Join(groups, "\n"))
	return b.String()
}

// installRows counts what the install sets up, numbers aligned on their last
// digit, with names and model groups as muted details; zero counts are
// omitted.
func (m Model) installRows() []ui.Row {
	summary := m.plan.Summary
	agents := len(summary.Agents)
	width := len(fmt.Sprint(max(agents, summary.Skills, len(summary.MCPServers), len(summary.Integrations))))
	var rows []ui.Row
	if agents > 0 {
		rows = agentRows(summary.Agents, width)
	}
	if summary.Skills > 0 {
		rows = append(rows, ui.Row{Lead: counted(summary.Skills, width, ui.TextSkillOne, ui.TextSkillMany)})
	}
	if len(summary.MCPServers) > 0 {
		rows = append(rows, ui.Row{
			Lead:   counted(len(summary.MCPServers), width, ui.TextMCPServerOne, ui.TextMCPServerMany),
			Detail: strings.Join(summary.MCPServers, ui.TextListSeparator),
		})
	}
	if len(summary.Integrations) > 0 {
		rows = append(rows, ui.Row{
			Lead:   counted(len(summary.Integrations), width, ui.TextIntegrationOne, ui.TextIntegrationMany),
			Detail: strings.Join(summary.Integrations, ui.TextListSeparator),
		})
	}
	return rows
}

// agentRows counts the agents by role and groups them by the model they run
// on, one continuation row per model.
func agentRows(agents []runtime.AgentModel, width int) []ui.Row {
	orchestrators, byModel := 0, map[string]int{}
	for _, agent := range agents {
		if agent.Role == model.RoleOrchestrator {
			orchestrators++
		}
		byModel[agent.Model]++
	}
	rows := []ui.Row{{
		Lead:   counted(len(agents), width, ui.TextAgentOne, ui.TextAgentMany),
		Detail: fmt.Sprintf(ui.TextAgentRolesFmt, orchestrators, len(agents)-orchestrators),
	}}
	models := slices.SortedFunc(maps.Keys(byModel), func(a, b string) int {
		return cmp.Or(byModel[b]-byModel[a], strings.Compare(a, b))
	})
	for _, name := range models {
		label := name
		if label == "" {
			label = ui.TextInheritedModel
		}
		detail := fmt.Sprintf(ui.TextAgentModelsFmt, byModel[name], label)
		if len(models) == 1 {
			detail = fmt.Sprintf(ui.TextAgentModelsAllFmt, label)
		}
		rows = append(rows, ui.Row{Detail: detail})
	}
	return rows
}

// diskRows states the file counts, then each of the user's configuration
// files the install touches, then what stays as it is, is left out, or is
// uncertain; each optional part appears only with content.
func (m Model) diskRows(added, updated int) []ui.Row {
	var rows []ui.Row
	var counts []string
	if added > 0 {
		counts = append(counts, counted(added, 0, ui.TextNewFileOne, ui.TextNewFileMany))
	}
	if updated > 0 {
		counts = append(counts, fmt.Sprintf(ui.TextUpdatedCountFmt, updated))
	}
	if len(counts) > 0 {
		rows = append(rows, ui.Row{Lead: strings.Join(counts, ui.TextListSeparator)})
	}
	for _, config := range m.configChanges() {
		rows = append(rows, ui.Row{Lead: path.Base(config.path), Detail: config.note})
	}
	rows = append(rows, labeledRows(ui.TextFieldKept, m.preserveLines())...)
	rows = append(rows, labeledRows(ui.TextFieldNotInstalled, m.removalLines())...)
	return append(rows, labeledRows(ui.TextFieldUncertain, m.uncertainLines())...)
}

// counted writes n and its noun in proper number, the number padded to width
// so a column of counts aligns on its last digit.
func counted(n, width int, one, many string) string {
	noun := many
	if n == 1 {
		noun = one
	}
	return fmt.Sprintf("%*d %s", width, n, noun)
}

// labeledRows puts label as the lead of the first line and continues the
// rest under it.
func labeledRows(label string, lines []string) []ui.Row {
	rows := make([]ui.Row, len(lines))
	for index, line := range lines {
		rows[index] = ui.Row{Detail: line}
	}
	if len(rows) > 0 {
		rows[0].Lead = label
	}
	return rows
}

// configChange is one of the user's configuration files the install
// changes, and whether their own settings survive.
type configChange struct {
	path, note string
}

// configChanges lists the user's configuration files the install changes,
// skipping the ones kept as they are.
func (m Model) configChanges() []configChange {
	var changes []configChange
	for _, config := range m.plan.Summary.Configs {
		if m.kept(config.Path) {
			continue
		}
		note := ui.TextConfigUpdated
		switch {
		case slices.Contains(m.restorePaths, config.Path):
			note = strings.TrimSpace(strings.Trim(ui.TextReplacesYours, " ()"))
		case config.Merged:
			note = ui.TextConfigMerged
		}
		changes = append(changes, configChange{path: config.Path, note: note})
	}
	return changes
}

func (m Model) reviewError() string {
	return ui.Status(ui.StateFailed, ui.TextCannotPrepareIntro+m.previewErr.Error()) + "\n\n" + theme.Label.Render(ui.TextNothingChangedRetry)
}

func (m Model) removalLines() []string {
	lines := make([]string, len(m.plan.Removals))
	for index, removal := range m.plan.Removals {
		lines[index] = fmt.Sprintf(ui.TextRemovalFmt, removal.Component, removal.Reason)
	}
	return lines
}

// preserveLines lists the files left as they are, and why.
func (m Model) preserveLines() []string {
	var preserve []string
	for _, conflict := range m.keptConflicts() {
		note := ui.TextYourVersionKept
		switch {
		case conflict.Impact == setup.ImpactUnrelated:
			note = ui.TextNotAffectedNote
		case conflict.Accepted:
			note = ui.TextPreviouslyKeptNote
		}
		preserve = append(preserve, conflict.Path+note)
	}
	return preserve
}

func (m Model) uncertainLines() []string {
	uncertain := m.uncertainKept()
	lines := make([]string, len(uncertain))
	for index, conflict := range uncertain {
		lines[index] = conflict.Consequence
	}
	return lines
}

// section renders a titled group with one indented line per item.
func section(b *strings.Builder, title string, lines []string) {
	b.WriteString(theme.Title.Render(title) + "\n")
	for _, line := range lines {
		b.WriteString(theme.Label.Render("  "+line) + "\n")
	}
	b.WriteString("\n")
}

// resultBody reports what happened, what changed, availability, and when
// changes take effect, in that order.
func (m Model) resultBody() string {
	if err := m.outcome.Err; err != nil {
		return m.resultError(err)
	}
	if m.outcome.Result.Cancelled() {
		return runtime.CancelledBody(m.outcome.Result) + "\n\n" + theme.Label.Render(ui.TextReviewAgainNote)
	}
	return m.resultSuccess()
}

func (m Model) resultError(err error) string {
	retry := ui.TextRetryInstall
	if m.configuring {
		retry = ui.TextRetryApply
	}
	return ui.Status(ui.StateFailed, err.Error()) + "\n\n" + theme.Label.Render(retry)
}
func (m Model) resultSuccess() string {
	harness := ui.OpenCodeLabel
	var b strings.Builder
	message := fmt.Sprintf(ui.TextInstalledForFmt, harness)
	if m.configuring {
		message = fmt.Sprintf(ui.TextUpdatedForFmt, harness)
	}
	b.WriteString(ui.Status(ui.StateSuccess, message) + "\n")
	if note := runtime.LateCancelNote(m.outcome.Result); note != "" {
		b.WriteString(note + "\n")
	}
	result := m.outcome.Result
	counts := fmt.Sprintf(ui.TextFilesChangedFmt, len(result.Changed), len(result.Unchanged))
	if len(result.Preserved) > 0 {
		counts += fmt.Sprintf(ui.TextPreservedFmt, len(result.Preserved))
	}
	b.WriteString(theme.Label.Render(counts+".") + "\n\n")
	if result.Verify != nil {
		b.WriteString(ui.Verification(*result.Verify) + "\n\n")
	}
	if result.Verify == nil {
		b.WriteString(ui.Status(ui.StateWarning, ui.TextNotReady) + "\n\n")
	}
	if len(result.Incomplete) > 0 {
		b.WriteString(ui.Status(ui.StateWarning, ui.TextIncompleteWorkIntro) + "\n")
		b.WriteString(theme.Title.Render(ui.TextIncompleteWorkTitle))
		for _, item := range result.Incomplete {
			b.WriteString("\n  " + theme.Label.Render(item))
		}
		b.WriteString("\n\n")
	}
	// Kept drift is a functional warning, never an operation failure.
	if uncertain := m.uncertainKept(); len(uncertain) > 0 {
		names := make([]string, len(uncertain))
		for index, conflict := range uncertain {
			names[index] = conflict.Affects
		}
		b.WriteString(ui.Status(ui.StateWarning, fmt.Sprintf(ui.TextCannotGuaranteeFmt, strings.Join(names, ", "))) + "\n")
		b.WriteString(theme.Label.Render(ui.TextUseDrift) + "\n\n")
	}
	// A completed reload already applied the changes; only otherwise do they
	// wait for OpenCode's next start (PR-UX-24).
	if !result.ReloadAttempted || result.ReloadError != nil {
		b.WriteString(theme.Label.Render(fmt.Sprintf(ui.TextTakeEffectFmt, harness)))
	}
	return strings.TrimRight(b.String(), "\n")
}
