// Package modelpicker edits one harness's specialist model drafts.
package modelpicker

import (
	"fmt"
	"strings"
	"sync"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// phase names the visible picker step.
type phase int

const (
	// phaseAssignments lists the specialists with their effective models.
	phaseAssignments phase = iota
	// phaseModels lists the model choices for one specialist.
	phaseModels
)

const (
	// assignmentListOverhead reserves the heading and spacing above assignments.
	assignmentListOverhead = 4
	// modelListOverhead reserves headings, search, and status rows above models.
	modelListOverhead = 6
)

const (
	// eventOpenModels opens the model list for the focused specialist.
	eventOpenModels = "open-models"
	// eventChooseModel confirms the focused model choice.
	eventChooseModel = "choose-model"
	// eventBack returns to the previous picker step.
	eventBack = "back"
)

// Model is the specialist model/effort draft.
type Model struct {
	agents      []model.CanonicalSubAgent
	overrides   map[string]model.ModelAssignment
	base        map[string]model.ModelAssignment
	keymap      keys.KeyMap
	phase       phase
	focus       ui.Section
	cursor      int
	selected    string
	modelCursor int
	search      string

	// Available holds the discovered model options for targets without a
	// fixed catalog; the caller runs discovery itself and fills these in.
	Available []string
	// LoadErr reports a failed model discovery.
	LoadErr error
	// Loading marks model discovery still running.
	Loading bool
	// Height sizes the visible list window.
	Height int
}

// openCodeInstances lists the deployed OpenCode specialist instances once; the
// embedded catalog never changes at runtime.
var openCodeInstances = sync.OnceValue(func() []model.CanonicalSubAgent {
	agents := []model.CanonicalSubAgent{}
	loaded, err := catalog.LoadPackages()
	if err != nil {
		// A broken catalog leaves the picker empty rather than failing to open.
		return agents
	}
	for _, def := range loaded.Agents {
		for _, instance := range def.Instances {
			agents = append(agents, model.CanonicalSubAgent{Name: instance})
		}
	}
	return agents
})

// New creates a picker starting from no overrides.
func New() Model {
	return Model{
		keymap:    keys.Default(),
		agents:    openCodeInstances(),
		overrides: make(map[string]model.ModelAssignment),
	}
}

// Preload starts the draft from the installed overrides.
func (p *Model) Preload(overrides map[string]model.ModelAssignment) {
	p.overrides = make(map[string]model.ModelAssignment, len(overrides))
	p.base = make(map[string]model.ModelAssignment, len(overrides))
	for id, assignment := range overrides {
		p.overrides[id] = assignment
		p.base[id] = assignment
	}
}

// Update handles one key within the picker.
func (p *Model) Update(key tea.KeyPressMsg) (done, back bool) {
	switch p.phase {
	case phaseModels:
		p.updateModels(key)
		return false, false
	}

	if section, switched := ui.SwitchSection(p.focus, p.keymap, key); switched {
		p.focus = section
		return false, false
	}
	switch {
	case p.keymap.Up.Matches(key):
		if p.focus == ui.SectionBody {
			p.cursor = ui.MoveCursor(p.cursor, len(p.agents), -1)
		}
	case p.keymap.Down.Matches(key):
		if p.focus == ui.SectionBody {
			p.cursor = ui.MoveCursor(p.cursor, len(p.agents), 1)
		}
	case p.keymap.Confirm.Matches(key):
		if p.focus == ui.SectionFooter {
			// An unavailable action never activates.
			return p.Changes() > 0, false
		}
		p.applyTransition(eventOpenModels)
	case p.keymap.Back.Matches(key):
		return false, true
	}
	return false, false
}

// transitions declares the picker flow between assignments and models.
func transitions() ui.Table[phase, Model] {
	return ui.Table[phase, Model]{
		{From: phaseAssignments, Event: eventOpenModels}: func(p *Model) (phase, tea.Cmd) {
			name := p.agents[p.cursor].Name
			if name != p.selected {
				// The filter belongs to one specialist's edit session.
				p.search = ""
			}
			p.selected = name
			p.modelCursor = max(0, p.currentModelIndex(p.models()))
			return phaseModels, nil
		},
		{From: phaseModels, Event: eventChooseModel}: func(p *Model) (phase, tea.Cmd) {
			models := p.models()
			if p.Loading || len(models) == 0 {
				return phaseModels, nil
			}
			assignment := p.current()
			if chosen := models[p.modelCursor]; chosen != assignment.Model {
				// Variants belong to their model. Preserve an existing variant
				// when confirming that model, not when choosing a different one.
				assignment = model.ModelAssignment{Model: chosen}
			}
			p.save(assignment)
			p.focus = ui.SectionBody
			return phaseAssignments, nil
		},
		{From: phaseModels, Event: eventBack}: func(*Model) (phase, tea.Cmd) {
			return phaseAssignments, nil
		},
	}
}

// applyTransition moves the picker to the next phase for one event.
func (p *Model) applyTransition(event string) {
	next, _, _ := transitions().Apply(p, p.phase, event)
	p.phase = next
}

// updateModels types the search filter or moves within the model list.
func (p *Model) updateModels(key tea.KeyPressMsg) {
	if p.searchKey(key) {
		return
	}
	models := p.models()
	switch {
	case p.keymap.Up.Matches(key):
		p.modelCursor = ui.MoveCursor(p.modelCursor, len(models), -1)
	case p.keymap.Down.Matches(key):
		p.modelCursor = ui.MoveCursor(p.modelCursor, len(models), 1)
	case p.keymap.Confirm.Matches(key):
		p.applyTransition(eventChooseModel)
	case p.keymap.Back.Matches(key):
		p.applyTransition(eventBack)
	}
}

func (p *Model) searchKey(key tea.KeyPressMsg) bool {
	if !p.Searching() {
		return false
	}
	if key.Mod != 0 {
		return p.clearSearchKey(key)
	}
	if key.Code == tea.KeySpace {
		p.search, p.modelCursor = p.search+" ", 0
		return true
	}
	if key.Code == tea.KeyBackspace {
		return p.backspaceSearch()
	}
	if key.Text != "" && key.Code >= ' ' && key.Code < tea.KeyExtended {
		p.search, p.modelCursor = p.search+key.Text, 0
		return true
	}
	return false
}

func (p *Model) clearSearchKey(key tea.KeyPressMsg) bool {
	if key.Code == 'u' || key.Code == 'U' {
		p.search, p.modelCursor = "", 0
	}
	return true
}
func (p *Model) backspaceSearch() bool {
	if runes := []rune(p.search); len(runes) > 0 {
		p.search, p.modelCursor = string(runes[:len(runes)-1]), 0
	}
	return true
}

// Paste enters pasted text into the search filter.
func (p *Model) Paste(text string) {
	if !p.Searching() || text == "" {
		return
	}
	p.search, p.modelCursor = p.search+text, 0
}

// save records one specialist's choice and returns to the list.
func (p *Model) save(assignment model.ModelAssignment) {
	if assignment.Model == p.inheritLabel() {
		assignment = model.ModelAssignment{}
	}
	if assignment.Model == "" {
		delete(p.overrides, p.selected)
	} else {
		p.overrides[p.selected] = assignment
	}
}

// Changes counts specialists whose draft assignment differs from the installed baseline.
func (p Model) Changes() int {
	count := 0
	for id, assignment := range p.overrides {
		if base, ok := p.base[id]; !ok || base != assignment {
			count++
		}
	}
	for id := range p.base {
		if _, ok := p.overrides[id]; !ok {
			count++
		}
	}
	return count
}

// current is the selected specialist's effective draft assignment.
func (p Model) current() model.ModelAssignment {
	// No catalog defaults: a missing override inherits the harness model.
	return p.overrides[p.selected]
}

// inheritLabel names the choice that keeps the harness default.
func (p Model) inheritLabel() string {
	return fmt.Sprintf(ui.TextPickerInheritFmt, ui.OpenCodeLabel)
}

// models lists the filtered choices, keeping the harness default as a
// choice when discovery fails.
func (p Model) models() []string {
	return filterModels(append([]string{p.inheritLabel()}, p.Available...), p.search)
}

// filterModels keeps the choices matching query, case-insensitively; an empty
// query keeps every choice.
func filterModels(all []string, query string) []string {
	if strings.TrimSpace(query) == "" {
		return all
	}
	needle := strings.ToLower(query)
	out := make([]string, 0, len(all))
	for _, id := range all {
		if strings.Contains(strings.ToLower(id), needle) {
			out = append(out, id)
		}
	}
	return out
}

// currentModelIndex locates the effective model in the list, or -1 when absent.
func (p Model) currentModelIndex(models []string) int {
	current := p.current().Model
	if current == "" {
		current = p.inheritLabel()
	}
	for index, id := range models {
		if id == current {
			return index
		}
	}
	return -1
}

// Detail names the specialist being edited, or "" on the assignment list.
func (p Model) Detail() string {
	if p.phase == phaseAssignments {
		return ""
	}
	return p.selected
}

// Searching reports that the search field owns printable keys.
func (p Model) Searching() bool {
	return p.phase == phaseModels && !p.Loading && p.LoadErr == nil
}

// Frame returns the active phase's body and footer; the caller adds header and size.
func (p Model) Frame() ui.Frame {
	switch p.phase {
	case phaseModels:
		return ui.Frame{Body: p.modelsView()}
	}
	return ui.Frame{Body: p.assignmentsView(), Footer: p.footer()}
}

// footer renders Apply changes, unavailable with its reason while the draft
// matches the installed baseline.
func (p Model) footer() string {
	action := ui.FooterAction{Label: ui.TextActionApplyChanges}
	if p.Changes() == 0 {
		action.Unavailable = ui.TextPickerNoChanges
	}
	return ui.FooterActions([]ui.FooterAction{action}, 0, p.focus == ui.SectionFooter)
}

// assignmentsView renders the specialist list with effective assignments.
func (p Model) assignmentsView() string {
	var b strings.Builder
	b.WriteString(theme.Label.Render(ui.TextPickerChoose))
	b.WriteString("\n\n")
	rows := make([]string, len(p.agents))
	for index, agent := range p.agents {
		assignment := p.overrides[agent.Name]
		rows[index] = displayAgentName(agent.Name) + ui.TextPickerSep + p.assignmentLabel(assignment)
	}
	b.WriteString(p.options(rows, p.cursor, p.focus == ui.SectionBody, assignmentListOverhead))
	return b.String()
}

// displayAgentName presents internal catalog IDs as concise user-facing names.
func displayAgentName(name string) string {
	if name == "takt" {
		return name
	}
	return strings.TrimPrefix(name, "takt-")
}

// assignmentLabel describes one effective assignment: the model, or
// inheritance from the harness default.
func (p Model) assignmentLabel(assignment model.ModelAssignment) string {
	if assignment.Model == "" {
		return fmt.Sprintf(ui.TextPickerInheritFmt, ui.OpenCodeLabel)
	}
	return assignment.Model
}

// modelsView renders the model list with search, loading, and failure states.
func (p Model) modelsView() string {
	var b strings.Builder
	b.WriteString(theme.Label.Render(ui.TextPickerCurrent + p.assignmentLabel(p.current())))
	b.WriteString("\n\n")
	if p.Loading {
		b.WriteString(ui.Status(ui.StatePending, fmt.Sprintf(ui.TextPickerLoadingFmt, ui.OpenCodeLabel)))
		return b.String()
	}
	if p.LoadErr != nil {
		b.WriteString(ui.Status(ui.StateWarning, fmt.Sprintf(ui.TextPickerLoadFailFmt, ui.OpenCodeLabel, p.LoadErr.Error())))
		b.WriteString("\n\n")
	}
	if p.Searching() {
		b.WriteString(theme.Focus.Render(theme.Icon.FieldBar) + theme.Label.Render(ui.TextPickerSearchIntro+p.search+"_"))
		b.WriteString("\n\n")
	}
	models := p.models()
	if len(models) == 0 {
		b.WriteString(theme.Label.Render(fmt.Sprintf(ui.TextPickerNoMatchFmt, p.search)))
		return b.String()
	}
	b.WriteString(p.options(models, p.modelCursor, true, modelListOverhead))
	return b.String()
}

// SparseOverrides returns the draft overrides, or nil when there are none.
func (p Model) SparseOverrides() map[string]model.ModelAssignment {
	if len(p.overrides) == 0 {
		return nil
	}
	result := make(map[string]model.ModelAssignment, len(p.overrides))
	for name, assignment := range p.overrides {
		result[name] = assignment
	}
	return result
}

// options keeps the cursor visible in long lists and shows position when
// they overflow.
func (p Model) options(items []string, cursor int, focused bool, overhead int) string {
	height := p.Height
	if height <= 0 {
		height = 14
	}
	count := max(1, height-overhead)
	start := max(0, cursor-count+1)
	end := min(len(items), start+count)
	out := ui.Options(items[start:end], cursor-start, focused)
	if len(items) > count {
		out += theme.Caption.Render(fmt.Sprintf(ui.TextPickerPosFmt, cursor+1, len(items)))
	}
	return out
}
