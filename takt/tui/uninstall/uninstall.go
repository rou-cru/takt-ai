// Package uninstall provides the target-scoped managed-file uninstall screen.
package uninstall

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// State identifies the visible uninstall step.
type State int

// States are the uninstall flow's screens, in walk order.
const (
	// StateModified asks what to do with each externally modified managed
	// file. Skipped when the preview finds none.
	StateModified State = iota
	// StateEngram asks what to do with the Engram database, which lives
	// outside Takt's footprint. Leave is preselected: the safe default with
	// zero behavior change.
	StateEngram
	// StateReview is the commitment point: scope, removals, kept content and
	// the Uninstall action. There is no second confirmation.
	StateReview
	// StateResult is the post-uninstall result screen.
	StateResult
	// StateNotInstalled short-circuits the flow: there is nothing to
	// uninstall.
	StateNotInstalled
)

// incompleteLineParts is the path and error split in an incomplete result.
const incompleteLineParts = 2

// Review footer actions, in order.
const (
	// Index 0 commits the reviewed uninstall: any cursor that is not
	// actionBack selects it.
	_ = iota
	// actionBack returns to the Engram question without uninstalling.
	actionBack
)

// Choice indices follow their corresponding display lists.
const (
	choiceKeep = iota
	choiceRemove
)
const (
	engramLeave = iota
	engramRetain
	engramRemove
)
const (
	resultBack = iota
	resultQuit
)

var modifiedChoices = []string{ui.TextKeepMine, ui.TextUninstallRemoveOpt}
var engramChoices = []ui.Item{
	{Label: ui.TextEngramLeave, Description: ui.TextEngramLeaveDesc},
	{Label: ui.TextEngramRetain, Description: ui.TextEngramRetainDesc},
	{Label: ui.TextEngramRemove, Description: ui.TextEngramRemoveDesc},
}
var reviewActions = []ui.FooterAction{{Label: ui.TextActionUninstall, Danger: true}, {Label: ui.TextActionBack}}

// resultActions lists the result screen's footer actions in order.
var resultActions = []string{ui.TextActionBackToMenu, ui.TextActionQuit}

// Model owns only uninstall interaction state. It emits lifecycle action
// messages; runtime.Adapter owns the uninstall itself as one busy operation,
// including the retention handoff for modified files.
type Model struct {
	// RootDir is the installation directory the screen operates on.
	RootDir string

	// state is the visible uninstall step.
	state State
	// cursor is the focused row or footer action.
	cursor int
	// focus selects the checklist body or its footer action.
	focus ui.Section
	// modified lists the user-edited managed files of the preview.
	modified []string
	// decisions maps each decided path to true (keep) or false (remove).
	decisions map[string]bool
	// preview holds the planned removals the review shows.
	preview setup.UninstallResult
	// previewErr reports a plan that could not be prepared.
	previewErr error
	// engram is the StateEngram answer (engramLeave, engramRetain or
	// engramRemove); the zero value is Leave, the safe default with zero
	// behavior change.
	engram int

	// run holds the in-flight uninstall request and its busy/cancel state.
	run runtime.Run
	// result carries the completed uninstall outcome.
	result runtime.ActionResult
	// err reports an uninstall that did not complete.
	err error

	// keymap is the shared navigation bindings.
	keymap keys.KeyMap
	// width and height size the rendered screen.
	width  int
	height int
}

// New creates an uninstall screen rooted at rootDir.
func New(rootDir string) Model {
	model := Model{RootDir: rootDir, keymap: keys.Default(), run: runtime.NewRun(), decisions: map[string]bool{}}
	if !setup.IsInstalled(rootDir) {
		model.state = StateNotInstalled
		return model
	}
	model.state = model.preparePreview()
	return model
}

// preparePreview plans the uninstall and returns the first screen: the
// modified-file questions when the preview found any, the Engram question
// otherwise.
func (model *Model) preparePreview() State {
	model.preview, model.previewErr = runtime.Adapter{}.PreviewUninstall(model.RootDir)
	if model.previewErr != nil {
		return StateEngram
	}
	for _, path := range model.preview.Preserved {
		if model.preview.PreservedReasons[path] == "user-edited" {
			model.modified = append(model.modified, path)
		}
	}
	if len(model.modified) > 0 {
		return StateModified
	}
	return StateEngram
}

// Init has no startup work.
func (Model) Init() tea.Cmd { return nil }

// Run exposes the flow's action state so the shell gates quit and cancel.
func (m Model) Run() runtime.Run { return m.run }

// State returns the visible uninstall step.
func (model Model) State() State { return model.state }

// Dirty reports answers that have not been applied yet.
func (model Model) Dirty() bool {
	if model.state == StateResult || model.state == StateNotInstalled {
		return false
	}
	return len(model.decisions) > 0 || model.engram != engramLeave
}

// Update handles interaction and action results without performing lifecycle work.
func (model Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case tea.WindowSizeMsg:
		model.width, model.height = message.Width, message.Height
		return model, nil
	case runtime.CancelRequest:
		model.run = model.run.Cancel(message)
		return model, nil
	case spinner.TickMsg:
		// Same-screen animation only: no navigation, no table event.
		var cmd tea.Cmd
		model.run, cmd = model.run.Tick(message)
		return model, cmd
	case runtime.ActionResultMsg:
		if message, ok := model.run.Result(message); ok {
			model.result, model.err = message.Result, message.Err
			return model.applyTransition(eventResult)
		}
		return model, nil
	}

	return model.updateKey(message)
}

func (model Model) updateKey(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyPressMsg)
	if !ok || model.run.Busy() {
		return model, nil
	}
	switch model.state {
	case StateNotInstalled:
		if model.keymap.Back.Matches(key) || model.keymap.Confirm.Matches(key) {
			return model.applyTransition(eventBack)
		}
	case StateModified:
		return model.updateModified(key)
	case StateEngram:
		return model.updateEngram(key)
	case StateReview, StateResult:
		return model.updateActions(key)
	}
	return model, nil
}

// retentionChoices splits decided files into kept and removed paths.
func (model Model) retentionChoices() setup.RetentionChoices {
	choices := setup.RetentionChoices{}
	for _, path := range model.modified {
		keep, decided := model.decisions[path]
		switch {
		case !decided:
		case keep:
			choices.Keep = append(choices.Keep, path)
		default:
			choices.Remove = append(choices.Remove, path)
		}
	}
	return choices
}

// allDecided reports whether every modified file has a choice.
func (model Model) allDecided() bool {
	for _, path := range model.modified {
		if _, ok := model.decisions[path]; !ok {
			return false
		}
	}
	return true
}

// engramChoice maps the StateEngram answer onto the runtime request: Leave
// is the safe default, Retain and Remove are explicit only.
func (model Model) engramChoice() string {
	switch model.engram {
	case engramRetain:
		return lifecycle.EngramRetain
	case engramRemove:
		return lifecycle.EngramRemove
	}
	return lifecycle.EngramLeave
}

const (
	// eventConfirm applies the current step's primary action.
	eventConfirm = "confirm"
	// eventBack returns to the previous screen.
	eventBack = "back"
	// eventResult shows the uninstall outcome.
	eventResult = "result"
)

// transitions is the uninstall flow's declarative screen-transition table:
// every (State, event) combination the flow accepts maps to one rule here.
func transitions() ui.Table[State, Model] {
	return ui.Table[State, Model]{
		{From: StateNotInstalled, Event: eventBack}: func(model *Model) (State, tea.Cmd) {
			return StateNotInstalled, ui.Back
		},
		{From: StateNotInstalled, Event: eventConfirm}: func(model *Model) (State, tea.Cmd) {
			return StateNotInstalled, ui.Back
		},
		{From: StateModified, Event: eventConfirm}: func(model *Model) (State, tea.Cmd) {
			if !model.allDecided() {
				return StateModified, nil
			}
			model.cursor, model.focus = model.engram, ui.SectionBody
			return StateEngram, nil
		},
		{From: StateModified, Event: eventBack}: func(model *Model) (State, tea.Cmd) {
			return StateModified, ui.Back
		},
		{From: StateEngram, Event: eventConfirm}: func(model *Model) (State, tea.Cmd) {
			if model.previewErr != nil {
				return StateEngram, nil
			}
			// The destructive action is never the default: focus starts on Back.
			model.cursor, model.focus = actionBack, ui.SectionFooter
			return StateReview, nil
		},
		{From: StateEngram, Event: eventBack}: func(model *Model) (State, tea.Cmd) {
			model.cursor, model.focus = 0, ui.SectionBody
			if len(model.modified) > 0 {
				return StateModified, nil
			}
			return StateEngram, ui.Back
		},
		{From: StateReview, Event: eventConfirm}: uninstallReviewConfirm,
		{From: StateReview, Event: eventBack}: func(model *Model) (State, tea.Cmd) {
			return model.reviewBack()
		},
		{From: StateReview, Event: eventResult}: func(model *Model) (State, tea.Cmd) {
			model.run = model.run.End()
			model.cursor, model.focus = resultBack, ui.SectionFooter
			if model.err == nil && !model.result.Cancelled() {
				model.decisions = map[string]bool{}
				model.engram = engramLeave
			}
			return StateResult, nil
		},
		{From: StateResult, Event: eventConfirm}: uninstallResultConfirm,
		{From: StateResult, Event: eventBack}: func(model *Model) (State, tea.Cmd) {
			return StateResult, ui.Back
		},
	}
}

func uninstallReviewConfirm(model *Model) (State, tea.Cmd) {
	if model.cursor == actionBack {
		return model.reviewBack()
	}
	choices := model.retentionChoices()
	request := runtime.ActionRequest{ID: runtime.NextID(), Action: runtime.ActionUninstall, RootDir: model.RootDir, RetainPaths: choices.Keep, RemovePaths: choices.Remove, EngramChoice: model.engramChoice()}
	model.run = model.run.Start(request)
	return StateReview, func() tea.Msg { return request }
}
func uninstallResultConfirm(model *Model) (State, tea.Cmd) {
	if model.cursor == resultQuit {
		return StateResult, tea.Quit
	}
	return StateResult, ui.Back
}

// reviewBack returns to the Engram question to fix answers.
func (model *Model) reviewBack() (State, tea.Cmd) {
	model.cursor, model.focus = 0, ui.SectionBody
	return StateEngram, nil
}

// applyTransition applies one table rule to move between screens.
func (model Model) applyTransition(event string) (tea.Model, tea.Cmd) {
	next, cmd, _ := transitions().Apply(&model, model.state, event)
	model.state = next
	return model, cmd
}

// updateModified: each file has two rows (Keep my version, Remove). Enter
// records the focused row's choice and moves to the next undecided file; once
// every file is decided it advances (PR-UX-15: no Continue button).
func (model Model) updateModified(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	rows := len(modifiedChoices) * len(model.modified)
	switch {
	case model.keymap.Up.Matches(key):
		model.cursor = ui.MoveCursor(model.cursor, rows, -1)
	case model.keymap.Down.Matches(key):
		model.cursor = ui.MoveCursor(model.cursor, rows, 1)
	case model.keymap.Confirm.Matches(key):
		file := model.cursor / len(modifiedChoices)
		model.decisions[model.modified[file]] = model.cursor%len(modifiedChoices) == choiceKeep
		if next, ok := model.nextUndecided(file); ok {
			model.cursor = next * len(modifiedChoices)
			return model, nil
		}
		return model.applyTransition(eventConfirm)
	case model.keymap.Back.Matches(key):
		return model.applyTransition(eventBack)
	}
	return model, nil
}

// nextUndecided finds the first undecided file after from, wrapping around.
func (model Model) nextUndecided(from int) (int, bool) {
	for step := 1; step <= len(model.modified); step++ {
		index := (from + step) % len(model.modified)
		if _, decided := model.decisions[model.modified[index]]; !decided {
			return index, true
		}
	}
	return 0, false
}

// updateEngram: Leave is preselected (safe default); Enter chooses the
// focused answer and advances to review.
func (model Model) updateEngram(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case model.keymap.Up.Matches(key):
		model.cursor = ui.MoveCursor(model.cursor, len(engramChoices), -1)
	case model.keymap.Down.Matches(key):
		model.cursor = ui.MoveCursor(model.cursor, len(engramChoices), 1)
	case model.keymap.Confirm.Matches(key):
		model.engram = model.cursor
		return model.applyTransition(eventConfirm)
	case model.keymap.Back.Matches(key):
		return model.applyTransition(eventBack)
	}
	return model, nil
}

// updateActions drives footer-only screens (review, result).
func (model Model) updateActions(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	count := len(reviewActions)
	if model.state == StateResult {
		count = len(resultActions)
	}
	switch {
	case ui.NudgeHorizontal(&model.cursor, count, model.keymap, key):
	case model.keymap.Confirm.Matches(key):
		return model.applyTransition(eventConfirm)
	case model.keymap.Back.Matches(key):
		return model.applyTransition(eventBack)
	}
	return model, nil
}

// Title identifies the flow and current step for the stable header region.
func (model Model) Title() string {
	switch model.state {
	case StateModified:
		return ui.TextUninstallModifiedTitle
	case StateEngram:
		return ui.TextUninstallEngramTitle
	case StateReview:
		return ui.TextUninstallReviewTitle
	case StateResult:
		return ui.TextUninstallResultTitle
	default:
		return ui.TextUninstallTitle
	}
}

// View renders the active uninstall step.
func (model Model) View() tea.View {
	frame := ui.Frame{Header: model.Title(), Width: model.width, Height: model.height}
	model.fillFrame(&frame)
	return tea.NewView(ui.Shell(frame))
}

func (model Model) fillFrame(frame *ui.Frame) {
	switch {
	case model.run.Busy():
		frame.Body, frame.Footer = ui.Busy(ui.TextUninstallBusy, model.run.SpinView(), model.run.ProgressView()), ui.BusyFooter(model.run.CancelRequested)
	case model.state == StateNotInstalled:
		frame.Body = theme.Label.Render(setup.NotInstalledMessage)
		frame.Footer = ui.FooterActions(ui.Actions(ui.TextActionBackToMenu), 0, true)
	case model.state == StateModified:
		model.modifiedFrame(frame)
	case model.state == StateEngram:
		model.engramFrame(frame)
	case model.state == StateReview:
		frame.Body = model.reviewBody()
		frame.Footer = ui.FooterActions(reviewActions, model.cursor, true)
	case model.state == StateResult:
		model.resultFrame(frame)
	}
}

func (model Model) modifiedFrame(frame *ui.Frame) {
	frame.Body = model.modifiedBody()
}

// engramFrame asks the Engram question; a plan that could not be prepared is
// reported here, and choosing does not advance until it can.
func (model Model) engramFrame(frame *ui.Frame) {
	frame.Body = model.engramBody()
	if model.previewErr != nil {
		frame.Body += "\n\n" + ui.Status(ui.StateFailed, ui.TextUninstallCannotPlan+model.previewErr.Error())
	}
}

func (model Model) resultFrame(frame *ui.Frame) {
	frame.Body = model.resultBody()
	actions := resultActions
	if model.err == nil && model.result.Partial() {
		actions = []string{ui.TextActionKeepCurrent, ui.TextActionQuit}
	}
	frame.Footer = ui.FooterActions(ui.Actions(actions...), model.cursor, true)
}

// modifiedBody renders each modified file with its keep/remove choice.
func (model Model) modifiedBody() string {
	var view strings.Builder
	view.WriteString(theme.Caption.Render(ui.TextUninstallModifiedIntro))
	view.WriteString("\n")
	for index, path := range model.modified {
		cursor := -1
		if model.cursor/len(modifiedChoices) == index {
			cursor = model.cursor % len(modifiedChoices)
		}
		chosen := -1
		if keep, decided := model.decisions[path]; decided {
			chosen = choiceRemove
			if keep {
				chosen = choiceKeep
			}
		}
		view.WriteString("\n" + theme.Strong.Render(path) + "\n")
		view.WriteString(ui.Selector(modifiedChoices, cursor, chosen, cursor >= 0))
	}
	view.WriteString("\n" + theme.Caption.Render(ui.TextUninstallKeptMoveIntro+model.retainedHint()+ui.TextUninstallKeptMoveOutro))
	return view.String()
}

// retainedHint names the retention directory before its timestamp exists.
func (model Model) retainedHint() string {
	return filepath.Join(model.RootDir, setup.RetainedDirName, "<date and time>") + string(filepath.Separator)
}

// engramBody asks the neutral retention question: valuable data is asked
// about, never inferred from the uninstall request itself.
func (model Model) engramBody() string {
	var view strings.Builder
	view.WriteString(theme.Caption.Render(ui.TextEngramQuestion))
	view.WriteString("\n" + ui.SelectorDescribed(engramChoices, model.cursor, model.engram))
	return view.String()
}

// reviewBody shows scope, removals, kept content, and the Uninstall action.
func (model Model) reviewBody() string {
	choices := model.retentionChoices()
	var keptInPlace []string
	for _, path := range model.preview.Preserved {
		if _, decided := model.decisions[path]; !decided {
			keptInPlace = append(keptInPlace, path+ui.TextReasonSep+preserveReasonLabel(model.preview.PreservedReasons[path]))
		}
	}
	removeCount := len(model.preview.Removed) + len(choices.Remove)

	var view strings.Builder
	view.WriteString(theme.Label.Render(fmt.Sprintf(ui.TextScopeFmt, ui.OpenCodeLabel)))
	view.WriteString("\n\n" + section(fmt.Sprintf(ui.TextUninstallRemoveFmt, files(removeCount)),
		append(slices.Clone(model.preview.Removed), suffixed(choices.Remove, ui.TextModifiedSuffix)...)))
	view.WriteString("\n" + theme.Label.Render(ui.TextUninstallMemoryLine))
	model.reviewKeptSections(&view, keptInPlace, choices.Keep)
	if len(choices.Keep) > 0 || model.engram == engramRetain {
		view.WriteString("\n\n" + theme.Label.Render(ui.TextUninstallRetainedData+model.retainedHint()))
	}
	view.WriteString("\n\n" + theme.WarningText.Render(ui.TextUninstallNoBackup))
	switch model.engram {
	case engramRemove:
		view.WriteString("\n" + theme.Caption.Render(ui.TextUninstallEngramChose))
	case engramRetain:
		view.WriteString("\n" + theme.Caption.Render(ui.TextUninstallEngramRetains))
	default:
		view.WriteString("\n" + theme.Caption.Render(ui.TextUninstallEngramStays))
	}
	return view.String()
}

func (model Model) reviewKeptSections(view *strings.Builder, keptInPlace, kept []string) {
	if len(model.preview.Restored) > 0 {
		view.WriteString("\n\n" + section(ui.TextUninstallRestoreTitle, model.preview.Restored))
	}
	if len(keptInPlace) > 0 {
		view.WriteString("\n\n" + section(ui.TextUninstallKeepInPlace, keptInPlace))
	}
	if len(kept) > 0 {
		view.WriteString("\n\n" + section(ui.TextUninstallKeepMine, kept))
	}
}

// resultBody reports what was removed, restored, kept, or left incomplete.
func (model Model) resultBody() string {
	var view strings.Builder
	result := model.result
	if model.err == nil && result.Cancelled() {
		return runtime.CancelledBody(result) + "\n\n" + theme.Label.Render(ui.TextUninstallAgainNote)
	}
	if model.err != nil && len(result.Removed)+len(result.Restored)+len(result.Retained) == 0 {
		view.WriteString(ui.Status(ui.StateFailed, ui.TextUninstallFailedIntro+model.err.Error()))
		return view.String()
	}

	// Work done before a failure is still reported, with the failure as
	// incomplete cleanup.
	incomplete := slices.Clone(result.Incomplete)
	if model.err != nil {
		incomplete = append(incomplete, model.err.Error())
	}
	names := ui.OpenCodeLabel
	if len(incomplete) > 0 {
		view.WriteString(ui.Status(ui.StatePartial, fmt.Sprintf(ui.TextUninstallPartialFmt, names)))
	} else {
		view.WriteString(ui.Status(ui.StateSuccess, fmt.Sprintf(ui.TextUninstallOkFmt, names)))
	}
	if note := runtime.LateCancelNote(result); note != "" {
		view.WriteString("\n" + note)
	}

	view.WriteString("\n\n" + model.resultFilesBody())
	if len(incomplete) > 0 {
		view.WriteString("\n\n" + section(ui.TextUninstallIncomplete, incomplete))
	}
	view.WriteString("\n\n" + theme.Label.Render(fmt.Sprintf(ui.TextUninstallTakeEffectFmt, names)))
	return view.String()
}

// resultFilesBody reports completed file changes and retained data locations.
func (model Model) resultFilesBody() string {
	var view strings.Builder
	result := model.result
	view.WriteString(section(fmt.Sprintf(ui.TextUninstallRemovedFmt, files(len(result.Removed))), result.Removed))
	if len(result.Restored) > 0 {
		view.WriteString("\n\n" + section(ui.TextUninstallRestoreTitle, result.Restored))
	}
	if keptInPlace := model.resultKeptInPlace(); len(keptInPlace) > 0 {
		view.WriteString("\n\n" + section(ui.TextUninstallKeepInPlace, keptInPlace))
	}
	if len(result.Retained) > 0 {
		view.WriteString("\n\n" + section(ui.TextUninstallKeptChoice, result.Retained))
		view.WriteString("\n\n" + theme.Label.Render(ui.TextUninstallRetainedIn+result.RetainedDir))
	}
	if result.RetainedMemory != "" {
		view.WriteString("\n\n" + theme.Label.Render(ui.TextUninstallMemoryKept+result.RetainedMemory))
	}
	return view.String()
}

// resultKeptInPlace excludes paths with a removal, retention, or cleanup failure
// from the preserved list so each path is reported with its actual outcome.
func (model Model) resultKeptInPlace() []string {
	result := model.result
	decided := slices.Concat(result.Retained, result.Removed)
	for _, line := range result.Incomplete {
		decided = append(decided, strings.SplitN(line, ": ", incompleteLineParts)[0])
	}
	var kept []string
	for _, path := range result.Preserved {
		if !slices.Contains(decided, path) {
			kept = append(kept, path+ui.TextReasonSep+preserveReasonLabel(model.preview.PreservedReasons[path]))
		}
	}
	return kept
}

// section renders a label followed by one indented line per item.
func section(label string, items []string) string {
	lines := []string{theme.Label.Render(label)}
	for _, item := range items {
		lines = append(lines, theme.Label.Render("  "+item))
	}
	return strings.Join(lines, "\n")
}

// suffixed appends the same note to every item.
func suffixed(items []string, suffix string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item+suffix)
	}
	return out
}

// files phrases a file count for section titles.
func files(count int) string {
	if count == 1 {
		return ui.TextOneFile
	}
	return fmt.Sprintf(ui.TextFilesFmt, count)
}

// preserveReasonLabel explains why PreviewUninstall keeps a file in place.
func preserveReasonLabel(reason string) string {
	switch reason {
	case "pre-existing":
		return ui.TextReasonPreexisting
	case "takt-additions":
		return ui.TextReasonAdditions
	}
	return reason
}
