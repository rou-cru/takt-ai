// Package modelpicker edits one harness's specialist model drafts.
package modelpicker

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
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
	// assignmentListOverhead reserves the position row under the table.
	assignmentListOverhead = 1
	// allAgentsOverhead reserves the All agents row and the gap below it.
	allAgentsOverhead = 2
	// resetHintOverhead reserves the reset hint and the gap above it.
	resetHintOverhead = 2
	// modelListOverhead reserves headings, search, and status rows above models.
	modelListOverhead = 6
	// tabsOverhead reserves the provider tabs row and its blank line.
	tabsOverhead = 2
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
	agents      []specialist
	overrides   map[string]model.ModelAssignment
	base        map[string]model.ModelAssignment
	keymap      keys.KeyMap
	phase       phase
	focus       ui.Section
	cursor      int
	selected    string
	modelCursor int
	search      string
	// tab is the provider id whose models are listed; "" lists every provider.
	tab string

	// Available holds the discovered model references (provider/id); the
	// caller runs discovery itself and fills these in, with Names mapping a
	// reference to the display name OpenCode reports, Info to the metadata it
	// states, and Providers a provider id to its display name.
	Available []string
	Names     map[string]string
	Info      map[string]opencodeapi.Model
	Providers map[string]string
	// LoadErr reports a failed model discovery.
	LoadErr error
	// Loading marks model discovery still running.
	Loading bool
	// Height is the terminal height; the list window derives from it through
	// the shell's own layout so the two never disagree. Width decides whether
	// the detail pane fits beside the list.
	Height, Width int
}

// specialist is one specialist instance as the picker lists it.
type specialist struct {
	// Name is the stable instance id assignments match by.
	Name string
	// Display is the catalog's user-facing name for the instance.
	Display string
	// Group is the logical block the instance is listed under.
	Group catalog.AgentGroup
}

// openCodeInstances lists the deployed OpenCode specialist instances once, in
// presentation order: catalog group, then display name. The embedded catalog
// never changes at runtime.
var openCodeInstances = sync.OnceValue(func() []specialist {
	agents := []specialist{}
	loaded, err := catalog.LoadPackages()
	if err != nil {
		// A broken catalog leaves the picker empty rather than failing to open.
		return agents
	}
	for _, def := range loaded.Agents {
		for _, instance := range def.Instances {
			agents = append(agents, specialist{Name: instance, Display: def.DisplayNames[instance], Group: def.Group})
		}
	}
	slices.SortFunc(agents, func(a, b specialist) int {
		return cmp.Or(
			cmp.Compare(slices.Index(catalog.AgentGroups, a.Group), slices.Index(catalog.AgentGroups, b.Group)),
			cmp.Compare(a.Display, b.Display),
		)
	})
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
			p.cursor = ui.MoveCursor(p.cursor, len(p.agents)+1, -1)
		}
	case p.keymap.Down.Matches(key):
		if p.focus == ui.SectionBody {
			p.cursor = ui.MoveCursor(p.cursor, len(p.agents)+1, 1)
		}
	case p.keymap.Confirm.Matches(key):
		if p.focus == ui.SectionFooter {
			// An unavailable action never activates.
			return p.Changes() > 0, false
		}
		p.applyTransition(eventOpenModels)
	case p.keymap.Reset.Matches(key):
		if p.focus == ui.SectionBody {
			p.assign(p.focusedAgent(), model.ModelAssignment{})
		}
	case p.keymap.Back.Matches(key):
		return false, true
	}
	return false, false
}

// transitions declares the picker flow between assignments and models.
func transitions() ui.Table[phase, Model] {
	return ui.Table[phase, Model]{
		{From: phaseAssignments, Event: eventOpenModels}: func(p *Model) (phase, tea.Cmd) {
			name := p.focusedAgent()
			if name != p.selected {
				// The filter belongs to one specialist's edit session.
				p.search = ""
			}
			p.selected = name
			// Open on the provider of the model already in use, else the first.
			p.tab = ""
			if current := p.current().Model; slices.Contains(p.Available, current) {
				p.tab = providerOf(current)
			}
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
			p.assign(p.selected, assignment)
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
	case p.keymap.Left.Matches(key):
		p.moveTab(-1)
	case p.keymap.Right.Matches(key):
		p.moveTab(1)
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

// allAgents selects the row that assigns one model to every agent at once.
const allAgents = ""

// focusedAgent is the agent under the assignment cursor; row 0 is All agents.
func (p Model) focusedAgent() string {
	if p.cursor > 0 {
		return p.agents[p.cursor-1].Name
	}
	return allAgents
}

// assign records the choice for one agent, or for every agent on the All
// agents row; individual rows stay editable afterwards. An empty assignment
// clears the override, so the agent falls back to the default model.
func (p *Model) assign(name string, assignment model.ModelAssignment) {
	targets := []string{name}
	if name == allAgents {
		targets = targets[:0]
		for _, agent := range p.agents {
			targets = append(targets, agent.Name)
		}
	}
	for _, name := range targets {
		if assignment.Model == "" {
			delete(p.overrides, name)
		} else {
			p.overrides[name] = assignment
		}
	}
}

// Change is one agent whose draft model differs from the installed one.
type Change struct {
	Agent, From, To string
}

// Pending lists the agents whose draft model differs from the installed
// baseline, in roster order, as their user-facing labels.
func (p Model) Pending() []Change {
	var changes []Change
	for _, agent := range p.agents {
		before, after := p.base[agent.Name], p.overrides[agent.Name]
		if before != after {
			changes = append(changes, Change{Agent: p.displayName(agent.Name), From: p.assignmentLabel(before), To: p.assignmentLabel(after)})
		}
	}
	return changes
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

// current is the selected agent's effective draft assignment; for All
// agents, the assignment they share, or a "different models" marker.
func (p Model) current() model.ModelAssignment {
	// No catalog defaults: a missing override inherits the harness model.
	if p.selected != allAgents {
		return p.overrides[p.selected]
	}
	shared, ok := p.sharedAssignment()
	if !ok {
		return model.ModelAssignment{Model: ui.TextPickerMixed}
	}
	return shared
}

// sharedAssignment is the assignment every agent has, if they all agree.
func (p Model) sharedAssignment() (model.ModelAssignment, bool) {
	if len(p.agents) == 0 {
		return model.ModelAssignment{}, true
	}
	shared := p.overrides[p.agents[0].Name]
	for _, agent := range p.agents[1:] {
		if p.overrides[agent.Name] != shared {
			return model.ModelAssignment{}, false
		}
	}
	return shared, true
}

// models lists the active tab's models that match the search.
func (p Model) models() []string {
	return p.modelsIn(p.activeTab())
}

// modelsIn lists one provider's models that match the search; the tab counts
// and the list position read the same set, so they always agree.
func (p Model) modelsIn(provider string) []string {
	refs := slices.DeleteFunc(slices.Clone(p.Available), func(ref string) bool { return providerOf(ref) != provider })
	return filterModels(refs, p.search, p.Names)
}

// label is a model's display name, or its reference when OpenCode gives none.
func (p Model) label(ref string) string {
	if name := p.Names[ref]; name != "" {
		return name
	}
	return ref
}

// filterModels keeps the choices matching query, case-insensitively; an empty
// query keeps every choice.
func filterModels(all []string, query string, names map[string]string) []string {
	if strings.TrimSpace(query) == "" {
		return all
	}
	needle := strings.ToLower(query)
	out := make([]string, 0, len(all))
	for _, id := range all {
		if strings.Contains(strings.ToLower(id), needle) || strings.Contains(strings.ToLower(names[id]), needle) {
			out = append(out, id)
		}
	}
	return out
}

// currentModelIndex locates the effective model in the list, or -1 when absent.
func (p Model) currentModelIndex(models []string) int {
	return slices.Index(models, p.current().Model)
}

// Detail names the specialist being edited, or "" on the assignment list.
func (p Model) Detail() string {
	if p.phase == phaseAssignments {
		return ""
	}
	if p.selected == allAgents {
		return ui.TextPickerAllAgents
	}
	return p.displayName(p.selected)
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

// assignmentsView is the agent | model table, led by the All agents row;
// a • marks agents whose model changes when applied, and a rule separates
// the catalog's groups.
func (p Model) assignmentsView() string {
	width := lipgloss.Width(ui.TextPickerAllAgents)
	for _, agent := range p.agents {
		width = max(width, lipgloss.Width(p.displayName(agent.Name)))
	}
	row := func(name, marker, value string) string {
		return name + strings.Repeat(" ", width-lipgloss.Width(name)+columnGap) + marker + value
	}
	// The default reads muted: it is the absence of an assignment.
	value := func(assignment model.ModelAssignment) string {
		if assignment.Model == "" {
			return theme.Caption.Render(defaultLabel())
		}
		return p.modelLabel(assignment.Model)
	}
	allValue := theme.Caption.Render(ui.TextPickerMixed)
	if shared, ok := p.sharedAssignment(); ok {
		allValue = value(shared)
	}
	rows := make([]string, 0, len(p.agents))
	ruleAt := make([]bool, 0, len(p.agents))
	cursorRow := 0
	for index, agent := range p.agents {
		if index > 0 && agent.Group != p.agents[index-1].Group {
			rows, ruleAt = append(rows, ""), append(ruleAt, true)
		}
		if index == p.cursor-1 {
			cursorRow = len(rows)
		}
		marker := unchangedMark
		if p.overrides[agent.Name] != p.base[agent.Name] {
			marker = ui.TextPickerChangedMark
		}
		rows, ruleAt = append(rows, row(p.displayName(agent.Name), marker, value(p.overrides[agent.Name]))), append(ruleAt, false)
	}
	// A rule is as wide as the table's widest row, so it ends where the table does.
	ruleWidth := lipgloss.Width(row(ui.TextPickerAllAgents, unchangedMark, allValue))
	for index, text := range rows {
		if !ruleAt[index] {
			ruleWidth = max(ruleWidth, lipgloss.Width(text))
		}
	}
	rule := theme.Caption.Render(strings.Repeat(groupRule, ruleWidth))
	for index := range rows {
		if ruleAt[index] {
			rows[index] = rule
		}
	}
	focused := p.focus == ui.SectionBody
	// All agents stands apart from the specialists it assigns to.
	allCursor := -1
	if p.cursor == 0 {
		allCursor = 0
	}
	var b strings.Builder
	b.WriteString(ui.Options([]string{row(ui.TextPickerAllAgents, unchangedMark, allValue)}, allCursor, focused) + "\n")
	start, end, overflow := p.windowBounds(len(rows), cursorRow, assignmentListOverhead+allAgentsOverhead+resetHintOverhead)
	// A rule cut off at either edge of the window would separate nothing.
	for start < end && ruleAt[start] {
		start++
	}
	for end > start && ruleAt[end-1] {
		end--
	}
	b.WriteString(strings.TrimSuffix(ui.Options(rows[start:end], cursorRow-start, focused && p.cursor > 0), "\n"))
	if overflow {
		b.WriteString("\n" + theme.Caption.Render(fmt.Sprintf(ui.TextPickerPosFmt, p.cursor+1, len(p.agents)+1)))
	}
	if p.resettable() {
		b.WriteString("\n\n" + theme.Caption.Render(ui.TextPickerResetHint))
	}
	return b.String()
}

// resettable reports whether the focused row has a model to clear.
func (p Model) resettable() bool {
	if name := p.focusedAgent(); name != allAgents {
		return p.overrides[name].Model != ""
	}
	return len(p.overrides) > 0
}

// groupRule is the horizontal rule between agent groups.
const groupRule = "─"

// columnGap separates the agent column from the model column; unchangedMark
// keeps unchanged rows aligned with the changed-row marker.
const (
	columnGap     = 2
	unchangedMark = "  "
)

// displayName is the catalog's user-facing name for an instance id.
func (p Model) displayName(name string) string {
	for _, a := range p.agents {
		if a.Name == name {
			return a.Display
		}
	}
	return name
}

// assignmentLabel describes one effective assignment: the model, or the
// default when none is assigned.
func (p Model) assignmentLabel(assignment model.ModelAssignment) string {
	if assignment.Model == "" {
		return defaultLabel()
	}
	return p.modelLabel(assignment.Model)
}

// modelLabel names a model by its display name without the provider prefix the
// provider tabs already carry. Models from different providers that share a
// name keep their provider so the two stay distinguishable.
func (p Model) modelLabel(ref string) string {
	name := p.shortName(ref)
	for _, other := range p.Available {
		if other != ref && p.shortName(other) == name {
			return name + ui.TextListSeparator + p.providerLabel(providerOf(ref))
		}
	}
	return name
}

// shortName is a model's display name, or the part of its reference after the
// provider when OpenCode gives none.
func (p Model) shortName(ref string) string {
	if name := p.Names[ref]; name != "" {
		return name
	}
	_, id, found := strings.Cut(ref, "/")
	if !found {
		return ref
	}
	return id
}

// defaultLabel names what an agent without an assigned model runs.
func defaultLabel() string {
	return ui.TextPickerDefault
}

// loadFailReason names why the catalog is missing in the user's terms; the
// adapter's error chain is diagnostic detail, not a reason.
func loadFailReason(err error) string {
	if errors.Is(err, opencodeapi.ErrBinaryMissing) {
		return ui.TextPickerNotInstalled
	}
	return ui.TextPickerNoAnswer
}

// modelsView renders the model list with search, loading, and failure states.
func (p Model) modelsView() string {
	var b strings.Builder
	b.WriteString(theme.Label.Render(p.Detail() + " · " + ui.TextPickerCurrent + p.assignmentLabel(p.current())))
	b.WriteString("\n\n")
	if p.Loading {
		b.WriteString(ui.Status(ui.StatePending, fmt.Sprintf(ui.TextPickerLoadingFmt, ui.OpenCodeLabel)))
		return b.String()
	}
	if p.LoadErr != nil {
		b.WriteString(ui.Status(ui.StateWarning, fmt.Sprintf(ui.TextPickerLoadFailFmt, ui.OpenCodeLabel, loadFailReason(p.LoadErr))))
		b.WriteString("\n\n")
	}
	overhead := modelListOverhead
	if tabs := p.tabsView(ui.ContentWidth(p.width())); tabs != "" {
		b.WriteString(tabs + "\n\n")
		overhead += tabsOverhead
	}
	if p.Searching() {
		b.WriteString(theme.Focus.Render(theme.Icon.FieldBar) + theme.Label.Render(ui.TextPickerSearchIntro+p.search) + searchCursor())
		b.WriteString("\n\n")
	}
	models := p.models()
	if len(models) == 0 {
		// A failed discovery already said why there is nothing to list.
		if len(p.Available) > 0 {
			b.WriteString(theme.Label.Render(fmt.Sprintf(ui.TextPickerNoMatchFmt, p.search)))
		}
		return strings.TrimRight(b.String(), "\n")
	}
	listWidth, paneWidth := p.columns()
	list, overflow := p.window(p.modelRows(models, listWidth-lipgloss.Width(theme.Icon.Cursor)), p.modelCursor, true, overhead)
	b.WriteString(p.withDetail(list, listWidth, paneWidth, models[min(p.modelCursor, len(models)-1)]))
	if overflow {
		b.WriteString("\n" + theme.Caption.Render(fmt.Sprintf(ui.TextPickerPosFmt, p.modelCursor+1, len(models))))
	}
	return b.String()
}

// width is the terminal width, defaulting like the shell does.
func (p Model) width() int {
	if p.Width <= 0 {
		return ui.DefaultWidth
	}
	return p.Width
}

// columns splits the panel into the list and the detail pane. The list is as
// wide as the longest name of any provider, so switching tabs never moves the
// divider, and the pane keeps at least detailMinWidth; a terminal too narrow
// for both gets the list alone (paneWidth 0).
func (p Model) columns() (listWidth, paneWidth int) {
	content := ui.ContentWidth(p.width())
	chrome := 2*paneGap + lipgloss.Width(paneDivider)
	for _, ref := range p.Available {
		listWidth = max(listWidth, lipgloss.Width(p.label(ref)))
	}
	listWidth += lipgloss.Width(theme.Icon.Cursor)
	limit := content - chrome - detailMinWidth
	if limit < listMinWidth {
		return min(listWidth, content), 0
	}
	listWidth = min(listWidth, limit)
	return listWidth, content - chrome - listWidth
}

// withDetail sets the focused model's detail pane beside the list, the
// divider as tall as the taller of the two.
func (p Model) withDetail(list string, listWidth, paneWidth int, ref string) string {
	if paneWidth == 0 {
		return list
	}
	detail := p.detailView(ref, paneWidth)
	gap := strings.Repeat(" ", paneGap)
	height := max(lipgloss.Height(list), lipgloss.Height(detail))
	divider := strings.TrimSuffix(strings.Repeat(theme.Caption.Render(paneDivider)+"\n", height), "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(listWidth).Render(list), gap, divider, gap, lipgloss.NewStyle().Width(paneWidth).Render(detail))
}

// modelRows shows each model's name, cut to the list column; everything else
// about a model lives in the detail pane, never in the row.
func (p Model) modelRows(models []string, width int) []string {
	rows := make([]string, len(models))
	for index, ref := range models {
		rows[index] = ansi.Truncate(p.label(ref), max(1, width), tabMore)
	}
	return rows
}

// searchCursor marks where typing lands: a focus-colored block, or an
// underscore when color is off.
func searchCursor() string {
	if theme.Mono() {
		return "_"
	}
	return theme.ButtonFocus.Render(" ")
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

// window keeps the cursor visible in long lists, reporting whether the list
// overflows so the caller can show the position.
func (p Model) window(items []string, cursor int, focused bool, overhead int) (string, bool) {
	start, end, overflow := p.windowBounds(len(items), cursor, overhead)
	return strings.TrimSuffix(ui.Options(items[start:end], cursor-start, focused), "\n"), overflow
}

// windowBounds is the slice of total rows that fits the viewport with the
// cursor row last once the list scrolls, and whether the list overflows.
func (p Model) windowBounds(total, cursor, overhead int) (start, end int, overflow bool) {
	height := p.Height
	if height <= 0 {
		height = ui.DefaultHeight
	}
	footer := 0
	if p.phase == phaseAssignments {
		footer = lipgloss.Height(p.footer())
	}
	count := max(1, ui.ContentRows(height, footer)-overhead)
	start = max(0, cursor-count+1)
	return start, min(total, start+count), total > count
}
