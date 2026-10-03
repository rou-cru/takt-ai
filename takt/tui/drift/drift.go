// Package drift shows which managed files differ from their installed
// definition and restores only the files the user selects.
package drift

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// State identifies the visible drift step.
type State int

// States are the drift flow's screens.
const (
	// StateReport is the read-only scan result.
	StateReport State = iota
	// StateSelect is the checklist of files to restore.
	StateSelect
	// StateReview names the files, their consequences and the commitment
	// action; it is the only authorization step.
	StateReview
	// StateResult is the post-correction result screen.
	StateResult
	// StateNotInstalled short-circuits scanning: there is nothing to compare.
	StateNotInstalled
)

// Footer action labels, also used to dispatch Enter.
const (
	// actionSelect opens the checklist of files to restore.
	actionSelect = ui.TextActionSelectFiles
	// actionMenu returns to the menu without changing anything.
	actionMenu = ui.TextActionBackToMenu
	// actionRestore applies the reviewed selection.
	actionRestore = ui.TextActionRestoreSelected
	// actionReselect returns to the selection to fix it.
	actionReselect = ui.TextActionReselect
	// actionRescan rechecks the files after a correction.
	actionRescan = ui.TextActionRescan
	// actionKeep leaves the current state after a partial correction.
	actionKeep = ui.TextActionKeepCurrent
)

// resultMsg carries the drift scan outcome into Update.
type resultMsg struct {
	conflicts        []setup.ConflictEntry
	installedVersion string
	err              error
}

// Model displays one asynchronous drift inspection.
type Model struct {
	root    string
	scanned bool
	result  resultMsg

	state      State
	cursor     int // checklist row
	action     int // footer action
	focus      ui.Section
	scroll     int
	selected   []string
	run        runtime.Run
	correction runtime.ActionResult
	correctErr error
	keymap     keys.KeyMap
	width      int
	height     int
}

// New creates a drift inspection screen rooted at the installation directory.
func New(root string) Model {
	m := Model{root: root, keymap: keys.Default(), run: runtime.NewRun()}
	if !setup.IsInstalled(root) {
		m.state = StateNotInstalled
	}
	return m
}

// State reports the visible step.
func (m Model) State() State { return m.state }

// Init scans for drift without modifying anything on disk; with nothing
// installed there is nothing to compare, so the scan is skipped.
func (m Model) Init() tea.Cmd {
	if m.state == StateNotInstalled {
		return nil
	}
	return func() tea.Msg {
		conflicts, err := (runtime.Adapter{}).ScanDrift(m.root)
		return resultMsg{conflicts: conflicts, installedVersion: setup.InstalledVersion(m.root), err: err}
	}
}

// Run exposes the flow's action state so the shell gates quit and cancel.
func (m Model) Run() runtime.Run { return m.run }

// Update receives the scan result, drives selection/review, and requests
// navigation back.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil
	case runtime.CancelRequest:
		m.run = m.run.Cancel(msg)
		return m, nil
	case runtime.ActionResultMsg:
		if message, ok := m.run.Result(msg); ok {
			m.correction, m.correctErr = message.Result, message.Err
			return m.applyTransition(eventResult)
		}
		return m, nil
	case resultMsg:
		m.scanned = true
		m.result = msg
		return m, nil
	case spinner.TickMsg:
		// Same-screen animation only: no navigation, no table event.
		var cmd tea.Cmd
		m.run, cmd = m.run.Tick(msg)
		return m, cmd
	case tea.KeyPressMsg:
		return m.key(msg)
	}
	return m, nil
}

const (
	// eventSelect opens the restore checklist.
	eventSelect = "select"
	// eventContinue carries the selection to review.
	eventContinue = "continue"
	// eventRestore applies the reviewed selection.
	eventRestore = "restore"
	// eventRescan rechecks the files from a clean state.
	eventRescan = "rescan"
	// eventBack returns to the previous screen.
	eventBack = "back"
	// eventResult shows the correction outcome.
	eventResult = "result"
)

// transitions is the drift flow's declarative screen-transition table: every
// (State, event) combination the flow accepts maps to one rule here.
func transitions() ui.Table[State, Model] {
	back := func(m *Model) (State, tea.Cmd) { return m.state, func() tea.Msg { return ui.BackMsg{} } }
	return ui.Table[State, Model]{
		{From: StateNotInstalled, Event: eventBack}: back,
		{From: StateReport, Event: eventBack}:       back,
		{From: StateReport, Event: eventSelect}: func(m *Model) (State, tea.Cmd) {
			if !m.hasDrift() {
				return StateReport, nil
			}
			m.focus = ui.SectionBody
			return StateSelect, nil
		},
		{From: StateSelect, Event: eventBack}: func(m *Model) (State, tea.Cmd) {
			return StateReport, nil
		},
		{From: StateSelect, Event: eventContinue}: func(m *Model) (State, tea.Cmd) {
			if len(m.selected) == 0 {
				return StateSelect, nil
			}
			return StateReview, nil
		},
		{From: StateReview, Event: eventBack}: func(m *Model) (State, tea.Cmd) {
			return StateSelect, nil
		},
		{From: StateReview, Event: eventRestore}: func(m *Model) (State, tea.Cmd) {
			if !m.sameVersion() {
				return StateReview, nil
			}
			request := runtime.ActionRequest{
				ID:                 runtime.NextID(),
				Action:             runtime.ActionCorrectDrift,
				RootDir:            m.root,
				SelectedDriftPaths: append([]string(nil), m.selected...),
			}
			m.run = m.run.Start(request)
			return StateReview, func() tea.Msg { return request }
		},
		{From: StateReview, Event: eventResult}: func(m *Model) (State, tea.Cmd) {
			m.run = m.run.End()
			return StateResult, nil
		},
		{From: StateResult, Event: eventBack}: back,
		{From: StateResult, Event: eventRescan}: func(m *Model) (State, tea.Cmd) {
			m.scanned, m.result, m.selected, m.cursor = false, resultMsg{}, nil, 0
			return StateReport, m.Init()
		},
	}
}

// applyTransition applies one table rule, resetting the footer action and
// scroll on a screen change.
func (m Model) applyTransition(event string) (tea.Model, tea.Cmd) {
	previous := m.state
	next, cmd, _ := transitions().Apply(&m, m.state, event)
	if next != m.state {
		m.scroll = 0
	}
	m.state = next
	if next != previous {
		m.action = 0
		if next == StateReview {
			// The destructive restore is never the default action.
			m.action = slices.Index(m.actions(), actionReselect)
		}
	}
	return m, cmd
}

// key handles same-screen interaction (movement, toggling, scrolling) and
// maps Enter on the focused action to a table event.
func (m Model) key(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.run.Busy() {
		return m, nil
	}
	if scroll, ok := ui.Scroll(m.scroll, m.height, key.String()); ok {
		m.scroll = scroll
		return m, nil
	}
	if m.keymap.Back.Matches(key) {
		return m.applyTransition(eventBack)
	}
	actions := m.actions()
	if m.state == StateSelect {
		if next, cmd, handled := m.selectKey(key, len(actions) > 0); handled {
			return next, cmd
		}
	} else if len(actions) > 0 && ui.NudgeHorizontal(&m.action, len(actions), m.keymap, key) {
		return m, nil
	}
	if m.keymap.Confirm.Matches(key) && m.action < len(actions) {
		return m.applyTransition(eventFor(actions[m.action]))
	}
	return m, nil
}

// selectKey handles the conflict-selection screen.
func (m Model) selectKey(key tea.KeyPressMsg, hasActions bool) (tea.Model, tea.Cmd, bool) {
	if hasActions {
		if section, switched := ui.SwitchSection(m.focus, m.keymap, key); switched {
			m.focus = section
			return m, nil, true
		}
	}
	if m.focus != ui.SectionBody {
		return m, nil, false
	}
	if ui.Nudge(&m.cursor, len(m.result.conflicts), m.keymap, key) {
		return m, nil, true
	}
	switch {
	case m.keymap.Toggle.Matches(key):
		m.selected = ui.Toggle(m.selected, m.result.conflicts[m.cursor].Path)
	case m.keymap.Confirm.Matches(key):
		// Enter never toggles: it completes the screen.
		next, cmd := m.applyTransition(eventContinue)
		return next, cmd, true
	}
	return m, nil, true
}

// eventFor maps a footer action label to its table event.
func eventFor(action string) string {
	switch action {
	case actionSelect:
		return eventSelect
	case actionRestore:
		return eventRestore
	case actionRescan:
		return eventRescan
	default:
		return eventBack
	}
}

// actions lists the current screen's footer actions in display order.
func (m Model) actions() []string {
	switch m.state {
	case StateNotInstalled:
		return []string{actionMenu}
	case StateReport:
		if m.hasDrift() {
			return []string{actionSelect, actionMenu}
		}
		return []string{actionMenu}
	case StateSelect:
		return nil
	case StateReview:
		return []string{actionRestore, actionReselect}
	case StateResult:
		if m.correction.Partial() && m.correctErr == nil {
			return []string{actionRescan, actionKeep}
		}
		return []string{actionRescan, actionMenu}
	}
	return nil
}

// sameVersion reports whether the installed reference matches this build,
// so correction never restores another version's files.
func (m Model) sameVersion() bool {
	return setup.IsCurrentVersion(m.result.installedVersion)
}

// hasDrift reports a completed scan that found restorable files.
func (m Model) hasDrift() bool {
	return m.scanned && m.result.err == nil && len(m.result.conflicts) > 0
}

// Dirty reports a file selection that has not been applied yet.
func (m Model) Dirty() bool {
	return len(m.selected) > 0 && m.state != StateResult && !m.run.Busy()
}

// Title identifies the flow and current step for the stable header region.
func (m Model) Title() string {
	switch m.state {
	case StateSelect:
		return ui.TextDriftSelectTitle
	case StateReview:
		return ui.TextDriftReviewTitle
	case StateResult:
		return ui.TextDriftResultTitle
	case StateNotInstalled:
		return ui.TextDriftEmptyTitle
	default:
		return ui.TextDriftReportTitle
	}
}

// View renders the active drift step.
func (m Model) View() tea.View {
	if m.run.Busy() {
		return tea.NewView(ui.Shell(ui.Frame{Header: m.Title(), Body: ui.Busy(ui.TextDriftBusy, m.run.SpinView(), m.run.ProgressView()), Footer: ui.BusyFooter(m.run.CancelRequested), Width: m.width, Height: m.height}))
	}
	switch m.state {
	case StateNotInstalled:
		return tea.NewView(m.actionView(theme.Label.Render(setup.NotInstalledMessage)))
	case StateSelect:
		return tea.NewView(m.selectView())
	case StateReview:
		return tea.NewView(m.actionView(m.review()))
	case StateResult:
		return tea.NewView(m.actionView(m.resultBody()))
	default:
		return tea.NewView(m.actionView(m.report()))
	}
}

// actionView frames a static body followed by the screen's labeled actions,
// which own focus.
func (m Model) actionView(body string) string {
	actions := m.actions()
	footer := ui.Actions(actions...)
	for index, action := range footer {
		if action.Label != actionRestore {
			continue
		}
		// Destructive commitment: the restore action renders as danger.
		footer[index].Danger = true
		if !m.sameVersion() {
			footer[index].Unavailable = versionUnavailable
		}
	}
	return ui.Shell(ui.Frame{
		Header: m.Title(),
		Body:   body,
		Footer: ui.FooterActions(footer, m.action, true),
		Width:  m.width,
		Height: m.height,
		Scroll: m.scroll,
	})
}

// report renders the scan outcome: loading, failure, clean, or the drifted list.
func (m Model) report() string {
	switch {
	case !m.scanned:
		return theme.Label.Render(ui.TextDriftChecking)
	case m.result.err != nil:
		return ui.Status(ui.StateFailed, ui.TextDriftCannotCheck+m.result.err.Error())
	case len(m.result.conflicts) == 0:
		return ui.Status(ui.StateSuccess, ui.TextDriftClean) +
			"\n\n" + theme.Caption.Render(ui.TextDriftNoVerify)
	}
	var b strings.Builder
	b.WriteString(ui.Status(ui.StateWarning, count(len(m.result.conflicts))+ui.TextDriftDifferOutro))
	b.WriteString("\n")
	for _, conflict := range m.result.conflicts {
		b.WriteString("\n" + row(conflict))
	}
	return b.String()
}

// selectView renders the restore checklist.
func (m Model) selectView() string {
	items := make([]ui.Item, 0, len(m.result.conflicts))
	for _, conflict := range m.result.conflicts {
		items = append(items, ui.Item{Label: row(conflict), Checked: slices.Contains(m.selected, conflict.Path)})
	}
	body := theme.Label.Render(ui.TextDriftSelectIntro) + "\n\n" +
		ui.CheckList(items, m.cursor, m.focus == ui.SectionBody)
	if len(m.selected) == 0 {
		body += "\n\n" + theme.Label.Render(ui.TextDriftSelectStar)
	}
	return ui.Shell(ui.Frame{Header: m.Title(), Body: body, Width: m.width, Height: m.height, Scroll: m.scroll})
}

// review is the commitment point: scope, restored files, and preserved files.
func (m Model) review() string {
	var b strings.Builder
	b.WriteString(theme.Label.Render(fmt.Sprintf(ui.TextScopeSimpleFmt, count(len(m.selected)))))
	b.WriteString("\n")
	b.WriteString(theme.Label.Render(m.reference()))
	b.WriteString("\n\n" + theme.Title.Render(ui.TextDriftRestoreHead))
	for _, path := range m.selected {
		b.WriteString("\n  " + theme.Label.Render(path))
		b.WriteString("\n    " + theme.Label.Render(consequence(m.reason(path))))
	}
	b.WriteString("\n\n" + theme.Title.Render(ui.TextDriftPreserveHead))
	b.WriteString("\n  " + theme.Label.Render(ui.TextDriftPreserveAll))
	return b.String()
}

// reference names the version-specific definitions a correction restores,
// or states which versions differ.
func (m Model) reference() string {
	installed := m.result.installedVersion
	switch {
	case m.sameVersion():
		return fmt.Sprintf(ui.TextDriftRefFmt, installed)
	case installed == "":
		return ui.TextDriftRefUnknown + setup.BuildVersion + "."
	default:
		return ui.TextDriftRefMismatch + installed + "; this build is " + setup.BuildVersion + "."
	}
}

// resultBody reports what the correction changed and what still differs.
func (m Model) resultBody() string {
	if m.correctErr == nil && m.correction.Cancelled() {
		return runtime.CancelledBody(m.correction) + "\n\n" + theme.Label.Render(ui.TextDriftCheckAgain)
	}
	if m.correctErr != nil {
		return m.failedBody()
	}
	restored, unresolved := m.correction.Changed, m.correction.Unresolved
	var b strings.Builder
	b.WriteString(correctionStatus(restored, unresolved))
	if note := runtime.LateCancelNote(m.correction); note != "" {
		b.WriteString("\n" + note)
	}
	if len(restored) > 0 {
		writePaths(&b, ui.TextDriftRestoredHead, restored)
	}
	if len(unresolved) > 0 {
		b.WriteString("\n\n" + theme.Title.Render(ui.TextDriftUnplanned))
		for _, path := range unresolved {
			b.WriteString("\n  " + theme.Label.Render(path))
			b.WriteString("\n    " + theme.Label.Render(ui.TextDriftLeftAsIs))
		}
	}
	b.WriteString("\n\n" + theme.Label.Render(ui.TextDriftNoFuncVerify))
	if len(restored) > 0 {
		b.WriteString("\n" + theme.Label.Render(ui.TextDriftTakeEffect))
	}
	if m.backedUp(restored) {
		b.WriteString("\n" + theme.Label.Render(ui.TextDriftBackupIntro+setup.BackupDir+"."))
	}
	return b.String()
}

// failedBody reports a correction that ended in error, listing what was
// restored before it failed.
func (m Model) failedBody() string {
	var b strings.Builder
	if changed := m.correction.Changed; len(changed) > 0 {
		// Files were restored before a later step failed: report both.
		b.WriteString(ui.Status(ui.StatePartial, fmt.Sprintf(ui.TextDriftRestoredFmt, count(len(changed)), m.correctErr.Error())))
		writePaths(&b, ui.TextDriftRestoredHead, changed)
	} else {
		// The runtime may fail after some files were written (for example
		// while reapplying Engram content), so nothing is claimed as unchanged.
		b.WriteString(ui.Status(ui.StateFailed, ui.TextDriftFailedIntro+m.correctErr.Error()))
	}
	b.WriteString("\n\n" + theme.Label.Render(ui.TextDriftSeeCurrent))
	return b.String()
}

// correctionStatus is the headline for a correction that returned without error.
func correctionStatus(restored, unresolved []string) string {
	switch {
	case len(unresolved) > 0 && len(restored) > 0:
		return ui.Status(ui.StatePartial, fmt.Sprintf(ui.TextDriftPartialFmt, count(len(restored)), len(unresolved)))
	case len(unresolved) > 0:
		return ui.Status(ui.StateFailed, ui.TextDriftNoPlanned)
	case len(restored) == 0:
		return ui.Status(ui.StateSuccess, ui.TextDriftAlreadyMatch)
	default:
		return ui.Status(ui.StateSuccess, ui.TextDriftRestoredOk+count(len(restored))+ui.TextDriftRestoredOut)
	}
}

// writePaths appends a titled list with one indented line per path.
func writePaths(b *strings.Builder, head string, paths []string) {
	b.WriteString("\n\n" + theme.Title.Render(head))
	for _, path := range paths {
		b.WriteString("\n  " + theme.Label.Render(path))
	}
}

// backedUp reports whether any restored path replaced existing content,
// which setup's priorStateFor copies to setup.BackupDir before overwriting.
func (m Model) backedUp(restored []string) bool {
	for _, path := range restored {
		if reason := m.reason(path); reason == "user-edited" || reason == "pre-existing" {
			return true
		}
	}
	return false
}

// reason returns the recorded drift reason for a path, or empty when unknown.
func (m Model) reason(path string) string {
	for _, conflict := range m.result.conflicts {
		if conflict.Path == path {
			return conflict.Reason
		}
	}
	return ""
}

// consequence states what restoring a file of that reason does, including
// whether a backup copy is kept first.
func consequence(reason string) string {
	switch reason {
	case "missing":
		return ui.TextDriftRecreated
	case "user-edited":
		return ui.TextDriftReplacedEdit
	default:
		return ui.TextDriftReplacedFile
	}
}

// row renders one drift entry with a textual label, path, and explanation.
func row(conflict setup.ConflictEntry) string {
	label, severity, explanation := setup.DriftLabel(conflict.Reason)
	style := theme.WarningText
	if severity == "danger" {
		style = theme.DangerText
	}
	return style.Render("["+label+"]") + " " + theme.Label.Render(conflict.Path+" - "+explanation)
}

// versionUnavailable is why restoring is blocked when the installed version
// differs from this build or is unknown.
const versionUnavailable = ui.TextDriftVersionBlock

// count phrases a file count for scope lines.
func count(n int) string {
	if n == 1 {
		return ui.TextDriftOneManaged
	}
	return fmt.Sprintf(ui.TextDriftManagedFmt, n)
}
