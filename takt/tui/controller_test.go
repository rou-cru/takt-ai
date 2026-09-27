package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// stubFlow records every message it receives; dirty drives ui.Dirtier.
type stubFlow struct {
	got   *[]tea.Msg
	dirty bool
	run   runtime.Run
}

func newStub(dirty bool) stubFlow { return stubFlow{got: &[]tea.Msg{}, dirty: dirty} }

func (s stubFlow) Init() tea.Cmd { return nil }
func (s stubFlow) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case runtime.ActionRequest:
		s.run = s.run.Start(msg)
	case runtime.CancelRequest:
		s.run = s.run.Cancel(msg)
	case runtime.ActionResultMsg:
		if _, ok := s.run.Result(msg); ok {
			s.run = s.run.End()
		}
	}
	*s.got = append(*s.got, msg)
	return s, nil
}
func (s stubFlow) View() tea.View { return tea.NewView("stub") }
func (s stubFlow) Dirty() bool    { return s.dirty }

// Run exposes the stub's action state so the shell gates quit and cancel.
func (s stubFlow) Run() runtime.Run { return s.run }

func (s stubFlow) count(match func(tea.Msg) bool) int {
	n := 0
	for _, msg := range *s.got {
		if match(msg) {
			n++
		}
	}
	return n
}

func step(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func withStack(flows ...tea.Model) Model {
	m := New("/nonexistent")
	m.stack = flows
	m.routes = make([]Route, len(flows))
	for i := range m.routes {
		m.routes[i] = RouteInstall
	}
	if len(flows) > 0 {
		m.route = RouteInstall
	}
	return m
}

func TestResizeReachesEveryStackedModelOtherMessagesOnlyForeground(t *testing.T) {
	bottom, top := newStub(false), newStub(false)
	m := withStack(bottom, top)
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	_, _ = step(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"})

	isSize := func(msg tea.Msg) bool { _, ok := msg.(tea.WindowSizeMsg); return ok }
	isKey := func(msg tea.Msg) bool { _, ok := msg.(tea.KeyPressMsg); return ok }
	if bottom.count(isSize) != 1 || top.count(isSize) != 1 {
		t.Fatalf("resize reached bottom=%d top=%d, want 1 each", bottom.count(isSize), top.count(isSize))
	}
	if bottom.count(isKey) != 0 || top.count(isKey) != 1 {
		t.Fatalf("key reached bottom=%d top=%d, want 0/1", bottom.count(isKey), top.count(isKey))
	}
}

func TestCtrlCOnDirtyFlowOpensGuard(t *testing.T) {
	m := withStack(newStub(true))
	m, cmd := step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if isQuit(cmd) || m.guard.state != guardOpen {
		t.Fatalf("dirty ctrl+c did not open the guard: state=%v", m.guard.state)
	}

	// Keep editing (Enter on the first option) preserves the flow.
	m, cmd = step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if isQuit(cmd) || m.guard.state != guardClosed || len(m.stack) != 1 {
		t.Fatalf("keep editing: quit=%v state=%v depth=%d", isQuit(cmd), m.guard.state, len(m.stack))
	}

	// Esc also keeps editing.
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m, cmd = step(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if isQuit(cmd) || m.guard.state != guardClosed || len(m.stack) != 1 {
		t.Fatal("esc in guard did not keep editing")
	}

	// Discard changes quits.
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m, _ = step(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	_, cmd = step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if !isQuit(cmd) {
		t.Fatal("discard after ctrl+c did not quit")
	}
}

func TestBackOnDirtyFlowDiscardPops(t *testing.T) {
	m := withStack(newStub(true))
	m, _ = step(t, m, ui.BackMsg{})
	if m.guard.state != guardOpen {
		t.Fatal("BackMsg from a dirty flow bypassed the guard")
	}
	m, _ = step(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, cmd := step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if isQuit(cmd) || len(m.stack) != 0 || m.CurrentRoute() != RouteMenu {
		t.Fatalf("discard after back: quit=%v depth=%d", isQuit(cmd), len(m.stack))
	}
}

func TestCtrlBOnCleanFlowPops(t *testing.T) {
	m := withStack(newStub(false), newStub(false))
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if m.guard.state != guardClosed || len(m.stack) != 1 {
		t.Fatalf("clean ctrl+b: state=%v depth=%d, want popped to 1", m.guard.state, len(m.stack))
	}
	_, cmd := step(t, withStack(newStub(false)), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !isQuit(cmd) {
		t.Fatal("clean ctrl+c did not quit")
	}
}

func TestCtrlCWhileBusyRequestsCancellationOnce(t *testing.T) {
	flow := newStub(true)
	m := withStack(flow)
	m, _ = step(t, m, runtime.ActionRequest{ID: 7}) // command deliberately not run
	if !m.flowRun().Busy() {
		t.Fatal("runtime not busy after request")
	}
	for range 3 {
		var cmd tea.Cmd
		m, cmd = step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if isQuit(cmd) || m.guard.state != guardClosed {
			t.Fatal("ctrl+c while busy quit or opened the guard")
		}
	}
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m, _ = step(t, m, ui.BackMsg{})
	if len(m.stack) != 1 || m.guard.state != guardClosed {
		t.Fatal("navigation took effect while busy")
	}
	isCancel := func(msg tea.Msg) bool { req, ok := msg.(runtime.CancelRequest); return ok && req.ID == 7 }
	if !m.flowRun().CancelRequested || flow.count(isCancel) != 1 {
		t.Fatalf("CancelRequested=%v, forwarded=%d, want true/1", m.flowRun().CancelRequested, flow.count(isCancel))
	}
	m, _ = step(t, m, runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: 7}})
	if m.flowRun().Busy() || m.flowRun().CancelRequested {
		t.Fatal("result did not reset busy/cancel state")
	}
}

// A result the controller's runtime never requested is ignored.
func TestStaleActionResultIsIgnored(t *testing.T) {
	m := withStack(newStub(false))
	m, _ = step(t, m, runtime.ActionRequest{ID: 11})
	m, _ = step(t, m, runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: 12}})
	if !m.flowRun().Busy() {
		t.Fatal("a foreign result ended the running action")
	}
	m, _ = step(t, m, runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: 11}})
	if m.flowRun().Busy() {
		t.Fatal("the matching result did not end the running action")
	}
}

// The controller owns the terminal-level view fields (PR-TUI-13): alt
// screen, no mouse, no focus reporting, and the painted dark backgrounds.
// Flows declare content only.
func TestControllerDeclaresTerminalViewFields(t *testing.T) {
	for _, m := range []Model{New(t.TempDir()), withStack(newStub(false))} {
		v := m.View()
		if !v.AltScreen || v.MouseMode != tea.MouseModeNone || v.ReportFocus {
			t.Fatalf("controller must own alt/mouse/focus: %+v", v)
		}
		if v.BackgroundColor != theme.Canvas || v.ForegroundColor != theme.TextPrimary {
			t.Fatalf("controller must paint the dark canvas: bg=%v fg=%v", v.BackgroundColor, v.ForegroundColor)
		}
	}
}
