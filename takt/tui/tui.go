// Package tui composes Takt's interactive lifecycle flows.
package tui

import (
	"charm.land/lipgloss/v2"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/diagnostics"
	"github.com/rou-cru/takt-ai/takt/tui/drift"
	"github.com/rou-cru/takt-ai/takt/tui/install"
	"github.com/rou-cru/takt-ai/takt/tui/models"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/styles"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/tui/uninstall"
)

// Route identifies a screen in the top-level application.
type Route string

// Routes are the closed set of top-level screens.
const (
	// RouteMenu is the main menu screen.
	RouteMenu Route = "menu"
	// RouteInstall is the install flow screen.
	RouteInstall Route = "install"
	// RouteDiagnostics shows reusable functional checks.
	RouteDiagnostics Route = "diagnostics"
	// RouteDrift shows read-only drift inspection against the ownership manifest.
	RouteDrift Route = "drift"
	// RouteUninstall is the uninstall flow screen.
	RouteUninstall Route = "uninstall"
	// RouteModels is the model-reassignment flow screen.
	RouteModels Route = "models"
)

// menuItem is one main-menu entry and the route it opens.
type menuItem struct {
	route Route
	label string
}

var menuItems = []menuItem{
	{RouteInstall, ui.TextMenuInstall},
	{RouteModels, ui.TextMenuAssignModels},
	{RouteDrift, ui.TextMenuCheckDrift},
	{RouteUninstall, ui.TextMenuUninstall},
	{RouteDiagnostics, ui.TextMenuDiagnostics},
}

// visibleMenu exposes only operations that can actually run.
func (m Model) visibleMenu() []menuItem {
	installed := setup.IsInstalled(m.root)
	if !installed {
		return menuItems[:1]
	}
	items := append([]menuItem(nil), menuItems...)
	items[0].label = ui.TextMenuConfigure
	return items
}

// Model owns navigation and the action boundary shared by lifecycle flows.
type Model struct {
	root   string
	cursor int
	route  Route
	stack  []tea.Model
	routes []Route
	// pendingID is the ID of the request whose execution this model started.
	pendingID uint64
	// actionCancel stops the running action's context; nil when idle.
	actionCancel context.CancelFunc
	adapter      runtime.Adapter
	width        int
	height       int
	guard        guard
}

// guard is the controller-owned pending-change overlay: open
// while asking Keep editing / Discard changes; quit records whether Discard
// exits the app (ctrl+c) or pops the active flow (ctrl+b, back).
type guard struct {
	state  guardState
	cursor int
	quit   bool
}

type guardState int

const (
	guardClosed guardState = iota
	guardOpen
)

const (
	eventClose         = "close"
	eventReplaceModels = "replace-models"
	eventGuardBack     = "guard-back"
	eventGuardQuit     = "guard-quit"
	eventGuardKeep     = "guard-keep"
	eventGuardDiscard  = "guard-discard"
)

var guardOptions = []string{ui.TextGuardKeepEditing, ui.TextGuardDiscard}

// active returns the top of the stack, or nil at the menu.
func (m Model) active() tea.Model {
	if len(m.stack) == 0 {
		return nil
	}
	return m.stack[len(m.stack)-1]
}

// push opens a new independent screen above the current one.
func (m *Model) push(route Route, screen tea.Model) {
	screen, _ = screen.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
	m.stack = append(m.stack, screen)
	m.routes = append(m.routes, route)
}

// pop closes the active screen via the single uniform mechanism, restoring
// the immediately lower model. At depth 1 that is the menu.
func (m *Model) pop() Route {
	if len(m.stack) == 0 {
		return RouteMenu
	}
	m.stack = m.stack[:len(m.stack)-1]
	m.routes = m.routes[:len(m.routes)-1]
	if len(m.routes) == 0 {
		return RouteMenu
	}
	return m.routes[len(m.routes)-1]
}

func openEvent(route Route) string { return "open:" + string(route) }

func routeTransitions() ui.Table[Route, Model] {
	open := func(route Route, screen func(string) tea.Model) func(*Model) (Route, tea.Cmd) {
		return func(m *Model) (Route, tea.Cmd) {
			m.push(route, screen(m.root))
			return route, m.active().Init()
		}
	}
	closeScreen := func(m *Model) (Route, tea.Cmd) { return m.pop(), nil }
	return ui.Table[Route, Model]{
		{From: RouteMenu, Event: openEvent(RouteInstall)}:     open(RouteInstall, func(root string) tea.Model { return install.New(root) }),
		{From: RouteMenu, Event: openEvent(RouteDiagnostics)}: open(RouteDiagnostics, func(root string) tea.Model { return diagnostics.New(root) }),
		{From: RouteMenu, Event: openEvent(RouteDrift)}:       open(RouteDrift, func(root string) tea.Model { return drift.New(root) }),
		{From: RouteMenu, Event: openEvent(RouteUninstall)}:   open(RouteUninstall, func(root string) tea.Model { return uninstall.New(root) }),
		{From: RouteMenu, Event: openEvent(RouteModels)}:      open(RouteModels, func(root string) tea.Model { return models.New(root) }),
		{From: RouteInstall, Event: eventClose}:               closeScreen,
		{From: RouteDiagnostics, Event: eventClose}:           closeScreen,
		{From: RouteDrift, Event: eventClose}:                 closeScreen,
		{From: RouteUninstall, Event: eventClose}:             closeScreen,
		{From: RouteModels, Event: eventClose}:                closeScreen,
		{From: RouteInstall, Event: eventReplaceModels}: func(m *Model) (Route, tea.Cmd) {
			m.stack, m.routes = nil, nil
			m.push(RouteModels, models.New(m.root))
			return RouteModels, m.active().Init()
		},
	}
}

func (m *Model) applyRoute(event string) tea.Cmd {
	next, cmd, _ := routeTransitions().Apply(m, m.route, event)
	m.route = next
	return cmd
}

func guardTransitions() ui.Table[guardState, Model] {
	open := func(quit bool) func(*Model) (guardState, tea.Cmd) {
		return func(m *Model) (guardState, tea.Cmd) {
			m.guard.quit = quit
			return guardOpen, nil
		}
	}
	return ui.Table[guardState, Model]{
		{From: guardClosed, Event: eventGuardBack}: open(false),
		{From: guardClosed, Event: eventGuardQuit}: open(true),
		{From: guardOpen, Event: eventGuardKeep}: func(m *Model) (guardState, tea.Cmd) {
			m.guard.cursor, m.guard.quit = 0, false
			return guardClosed, nil
		},
		{From: guardOpen, Event: eventGuardDiscard}: func(m *Model) (guardState, tea.Cmd) {
			quit := m.guard.quit
			m.guard.cursor, m.guard.quit = 0, false
			return guardClosed, m.discard(quit)
		},
	}
}

func (m *Model) applyGuard(event string) tea.Cmd {
	next, cmd, _ := guardTransitions().Apply(m, m.guard.state, event)
	m.guard.state = next
	return cmd
}

// New creates the top-level TUI rooted at root.
func New(root string) Model {
	return Model{root: root, route: RouteMenu, adapter: runtime.NewAdapter(), width: ui.DefaultWidth, height: ui.DefaultHeight}
}

// Run starts the TUI with explicit streams.
func Run(input io.Reader, output io.Writer) error {
	root, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	// AltScreen is declared on the view, not the program; see Model.View.
	_, err = tea.NewProgram(New(root), tea.WithInput(input), tea.WithOutput(output)).Run()
	return err
}

// Init has no startup work.
func (Model) Init() tea.Cmd { return nil }

// CurrentRoute returns the active route.
func (m Model) CurrentRoute() Route { return m.route }

// flowRun reports the active flow's action state; zero when none is running.
func (m Model) flowRun() runtime.Run {
	if runner, ok := m.active().(runtime.Runner); ok {
		return runner.Run()
	}
	return runtime.Run{}
}

// Update routes input to the visible screen and action messages through runtime.
func (m Model) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := message.(tea.KeyPressMsg); ok && m.guard.state == guardOpen {
		return m.updateGuard(key)
	}
	if paste, ok := message.(tea.PasteMsg); ok {
		return m.paste(paste)
	}
	return m.dispatch(message)
}

func (m Model) dispatch(message tea.Msg) (tea.Model, tea.Cmd) {
	switch message := message.(type) {
	case runtime.ActionRequest, runtime.ActionResultMsg, runtime.CancelRequest:
		return m.updateAction(message)
	case ui.BackMsg:
		return m.leave(false)
	case install.OpenModelsMsg:
		return m, m.applyRoute(eventReplaceModels)
	case tea.WindowSizeMsg:
		return m.resize(message)
	case tea.KeyPressMsg:
		if next, cmd, handled := m.globalKey(message); handled {
			return next, cmd
		}
	}

	if m.active() == nil {
		return m.updateMenu(message)
	}
	next, command := m.active().Update(message)
	m.stack[len(m.stack)-1] = next
	return m, command
}

func (m Model) paste(message tea.PasteMsg) (tea.Model, tea.Cmd) {
	if m.guard.state == guardOpen {
		return m, nil
	}
	return m.forward(message)
}
func (m Model) forward(message tea.Msg) (tea.Model, tea.Cmd) {
	if m.active() == nil {
		return m.updateMenu(message)
	}
	next, command := m.active().Update(message)
	m.stack[len(m.stack)-1] = next
	return m, command
}

func (m Model) resize(message tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = message.Width, message.Height
	for i, screen := range m.stack {
		m.stack[i], _ = screen.Update(message)
	}
	return m, nil
}

// globalKey handles the chords every flow shares: ctrl+c cancels a running
// action or leaves, and ctrl+b goes back.
func (m Model) globalKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd, bool) {
	if m.active() == nil {
		return m, nil, false
	}
	var next tea.Model
	var cmd tea.Cmd
	switch key.String() {
	case "ctrl+c":
		if m.flowRun().Busy() {
			next, cmd = m.cancel()
		} else {
			next, cmd = m.leave(true)
		}
	case "ctrl+b":
		next, cmd = m.leave(false)
	default:
		return m, nil, false
	}
	return next, cmd, true
}

// leave pops the active flow (or quits) unless it holds unapplied changes,
// in which case the guard asks first. Nothing navigates while an action runs.
func (m Model) leave(quit bool) (tea.Model, tea.Cmd) {
	if m.flowRun().Busy() {
		return m, nil
	}
	if flow, ok := m.active().(ui.Dirtier); ok && flow.Dirty() {
		event := eventGuardBack
		if quit {
			event = eventGuardQuit
		}
		return m, m.applyGuard(event)
	}
	return m, m.discard(quit)
}

func (m *Model) discard(quit bool) tea.Cmd {
	if quit {
		return tea.Quit
	}
	return m.applyRoute(eventClose)
}

// cancel turns ctrl+c during an action into one cooperative cancellation
// request; repeated presses add nothing.
func (m Model) cancel() (tea.Model, tea.Cmd) {
	run := m.flowRun()
	if run.CancelRequested {
		return m, nil
	}
	next, cmd := m.active().Update(runtime.CancelRequest{ID: run.Request.ID})
	m.stack[len(m.stack)-1] = next
	if m.actionCancel != nil {
		m.actionCancel()
		m.actionCancel = nil
	}
	return m, cmd
}

func (m Model) updateGuard(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		m.guard.cursor = ui.MoveCursor(m.guard.cursor, len(guardOptions), -1)
	case "down", "j":
		m.guard.cursor = ui.MoveCursor(m.guard.cursor, len(guardOptions), 1)
	case "esc":
		return m, m.applyGuard(eventGuardKeep)
	case "enter":
		if m.guard.cursor == 1 {
			return m, m.applyGuard(eventGuardDiscard)
		}
		return m, m.applyGuard(eventGuardKeep)
	}
	return m, nil
}

// guardView renders the pending-change overlay.
func (m Model) guardView() string {
	body := theme.Label.Render(ui.TextGuardDiscardBody) + "\n\n" + ui.Options(guardOptions, m.guard.cursor, true)
	return ui.Shell(ui.Frame{Header: m.guardTitle(), Body: body, Width: m.width, Height: m.height})
}

// guardTitle names the interrupted task.
func (m Model) guardTitle() string {
	titled, ok := m.active().(interface{ Title() string })
	if !ok {
		return ui.TextGuardUnappliedWord
	}
	task, _, _ := strings.Cut(titled.Title(), " · ")
	return task + " · " + ui.TextGuardUnappliedWord
}

func (m Model) updateAction(message tea.Msg) (tea.Model, tea.Cmd) {
	var actionCommand tea.Cmd
	switch message := message.(type) {
	case runtime.ActionRequest:
		// The executor owns the running context; the active flow owns the
		// busy state and receives the request it emitted.
		ctx, cancel := context.WithCancel(context.Background())
		m.pendingID, m.actionCancel = message.ID, cancel
		// First animation frame for the flow's busy marker; the flow
		// chains further frames itself while busy. A zero TickMsg is a
		// valid first tick: no ID means no owner to reject.
		actionCommand = tea.Batch(m.adapter.Command(ctx, message), func() tea.Msg { return spinner.TickMsg{} })
	case runtime.ActionResultMsg:
		// A result this model did not request never ends the running one:
		// only the matching ID releases its context.
		if message.Request.ID == m.pendingID && m.actionCancel != nil {
			m.actionCancel() // release the finished action's context
			m.actionCancel = nil
		}
	}
	if m.active() == nil {
		return m, actionCommand
	}
	nextFlow, flowCommand := m.active().Update(message)
	m.stack[len(m.stack)-1] = nextFlow
	return m, tea.Batch(actionCommand, flowCommand)
}

// updateMenu drives the home menu.
func (m Model) updateMenu(message tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := message.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	items := m.visibleMenu()
	count := len(items) + 1
	m.cursor = min(m.cursor, count-1)
	switch key.String() {
	case "up", "k":
		m.cursor = (m.cursor + count - 1) % count
	case "down", "j":
		m.cursor = (m.cursor + 1) % count
	case "q", "ctrl+c":
		return m, tea.Quit
	case "enter":
		if m.cursor == len(items) {
			return m, tea.Quit
		}
		item := items[m.cursor]
		return m, m.applyRoute(openEvent(item.route))
	}
	return m, nil
}

// View renders the menu or the active lifecycle flow.
func (m Model) View() tea.View {
	if m.guard.state == guardOpen {
		return m.declare(m.guardView())
	}
	if active := m.active(); active != nil {
		v := active.View()
		v.AltScreen = true
		v.MouseMode = tea.MouseModeNone
		v.ReportFocus = false
		m.paintCanvas(&v)
		return v
	}
	return m.declare(ui.Shell(ui.Frame{Body: m.menuBody(), Width: m.width, Height: m.height, Home: true}))
}

// declare stamps the controller-owned terminal fields onto a content view.
func (m Model) declare(content string) tea.View {
	v := tea.NewView(content)
	v.AltScreen = true
	m.paintCanvas(&v)
	return v
}

// paintCanvas sets the terminal backgrounds through the view.
func (Model) paintCanvas(v *tea.View) {
	if theme.Mono() {
		return
	}
	v.BackgroundColor = theme.Canvas
	v.ForegroundColor = theme.TextPrimary
}

// menuBody renders the logo beside the task list.
func (m Model) menuBody() string {
	labels := []string{}
	for _, item := range m.visibleMenu() {
		labels = append(labels, item.label)
	}
	labels = append(labels, ui.TextMenuQuit)
	menu := ui.Options(labels, min(m.cursor, len(labels)-1), true)
	notice := m.incompleteNotice()

	logo := styles.RenderLogo()
	inner := ui.InnerWidth(m.width)
	logoWidth, logoHeight := lipgloss.Width(logo), lipgloss.Height(logo)
	menuWidth := lipgloss.Width(menu)

	const gap = "  "
	const minMenuHeight = 4
	bodyHeight := ui.BodyHeight(m.height)
	showLogo := bodyHeight <= 0 || bodyHeight > logoHeight+minMenuHeight
	twoColumn := inner > 0 && logoWidth+lipgloss.Width(gap)+menuWidth <= inner

	// Left-aligned, not centered: Shell's wrapBody infers each line's hanging
	// indent from its own leading whitespace (the focus gutter convention), so
	// centering here would read as an oversized indent and wrap the menu.
	switch {
	case !showLogo:
		return notice + menu
	case twoColumn:
		return notice + lipgloss.JoinHorizontal(lipgloss.Top, logo, gap, menu)
	default:
		return notice + logo + "\n\n" + menu
	}
}

// incompleteNotice warns that a mutating operation ended abruptly.
func (m Model) incompleteNotice() string {
	record, found := setup.IncompleteOperation(m.root)
	if !found {
		return ""
	}
	lines := record.Notice(m.root)
	return ui.Status(ui.StateWarning, lines[0]) + "\n" + theme.Label.Render(lines[1]) + "\n\n"
}
