package tui_test

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/tui"
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

func step(t *testing.T, m tui.Model, msg tea.Msg) (tui.Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(tui.Model), cmd
}

func screenText(m tui.Model) string { return ansi.Strip(m.View().Content) }

// guardOpen reports whether the pending-change overlay is on screen.
func guardOpen(m tui.Model) bool { return strings.Contains(screenText(m), ui.TextGuardDiscardBody) }

func isKey(msg tea.Msg) bool { _, ok := msg.(tea.KeyPressMsg); return ok }

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestResizeReachesEveryStackedModelOtherMessagesOnlyForeground(t *testing.T) {
	bottom, top := newStub(false), newStub(false)
	m := tui.WithStack(bottom, top)
	m, _ = step(t, m, tea.WindowSizeMsg{Width: 100, Height: 40})
	_, _ = step(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"})

	isSize := func(msg tea.Msg) bool { _, ok := msg.(tea.WindowSizeMsg); return ok }
	if bottom.count(isSize) != 1 || top.count(isSize) != 1 {
		t.Fatalf("resize reached bottom=%d top=%d, want 1 each", bottom.count(isSize), top.count(isSize))
	}
	if bottom.count(isKey) != 0 || top.count(isKey) != 1 {
		t.Fatalf("key reached bottom=%d top=%d, want 0/1", bottom.count(isKey), top.count(isKey))
	}
}

func TestCtrlCOnDirtyFlowOpensGuard(t *testing.T) {
	m := tui.WithStack(newStub(true))
	m, cmd := step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if isQuit(cmd) || !guardOpen(m) {
		t.Fatal("dirty ctrl+c did not open the guard")
	}

	// Keep editing (Enter on the first option) preserves the flow.
	m, cmd = step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if isQuit(cmd) || guardOpen(m) || m.CurrentRoute() != tui.RouteInstall {
		t.Fatalf("keep editing: quit=%v guard=%v route=%v", isQuit(cmd), guardOpen(m), m.CurrentRoute())
	}

	// Esc also keeps editing.
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	m, cmd = step(t, m, tea.KeyPressMsg{Code: tea.KeyEscape})
	if isQuit(cmd) || guardOpen(m) || m.CurrentRoute() != tui.RouteInstall {
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
	m := tui.WithStack(newStub(true))
	m, _ = step(t, m, ui.BackMsg{})
	if !guardOpen(m) {
		t.Fatal("BackMsg from a dirty flow bypassed the guard")
	}
	m, _ = step(t, m, tea.KeyPressMsg{Code: tea.KeyDown})
	m, cmd := step(t, m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if isQuit(cmd) || m.CurrentRoute() != tui.RouteMenu {
		t.Fatalf("discard after back: quit=%v route=%v", isQuit(cmd), m.CurrentRoute())
	}
}

func TestCtrlBOnCleanFlowPops(t *testing.T) {
	bottom, top := newStub(false), newStub(false)
	m := tui.WithStack(bottom, top)
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if guardOpen(m) || m.CurrentRoute() != tui.RouteInstall {
		t.Fatalf("clean ctrl+b: guard=%v route=%v, want popped to the lower flow", guardOpen(m), m.CurrentRoute())
	}
	step(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"})
	if bottom.count(isKey) != 1 || top.count(isKey) != 0 {
		t.Fatalf("after pop a key reached bottom=%d top=%d, want the lower flow only", bottom.count(isKey), top.count(isKey))
	}
	_, cmd := step(t, tui.WithStack(newStub(false)), tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !isQuit(cmd) {
		t.Fatal("clean ctrl+c did not quit")
	}
}

func TestCtrlCWhileBusyRequestsCancellationOnce(t *testing.T) {
	flow := newStub(true)
	m := tui.WithStack(flow)
	m, _ = step(t, m, runtime.ActionRequest{ID: 7}) // command deliberately not run
	for range 3 {
		var cmd tea.Cmd
		m, cmd = step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
		if isQuit(cmd) || guardOpen(m) {
			t.Fatal("ctrl+c while busy quit or opened the guard")
		}
	}
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	m, _ = step(t, m, ui.BackMsg{})
	if m.CurrentRoute() != tui.RouteInstall || guardOpen(m) {
		t.Fatal("navigation took effect while busy")
	}
	isCancel := func(msg tea.Msg) bool { req, ok := msg.(runtime.CancelRequest); return ok && req.ID == 7 }
	if flow.count(isCancel) != 1 {
		t.Fatalf("cancellation forwarded %d times, want once", flow.count(isCancel))
	}
	// Once the result arrives the flow is idle again: ctrl+c guards its changes.
	m, _ = step(t, m, runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: 7}})
	if m, _ = step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); !guardOpen(m) {
		t.Fatal("result did not reset busy/cancel state")
	}
}

// The first animation frame for a busy action respects TAKT_NO_ANIMATION:
// the adapter command always runs, but the manual spinner tick is only
// batched alongside it when animation is enabled. tea.Batch of a single
// non-nil command returns that command directly rather than wrapping it, so
// hasTick handles both the wrapped and unwrapped shapes.
func TestUpdateActionSpinnerTickRespectsNoAnimation(t *testing.T) {
	hasTick := func(cmd tea.Cmd) bool {
		msg := cmd()
		batch, ok := msg.(tea.BatchMsg)
		if !ok {
			_, isTick := msg.(spinner.TickMsg)
			return isTick
		}
		for _, sub := range batch {
			if _, isTick := sub().(spinner.TickMsg); isTick {
				return true
			}
		}
		return false
	}

	m := tui.WithStack(newStub(false))
	_, cmd := step(t, m, runtime.ActionRequest{ID: 42})
	if !hasTick(cmd) {
		t.Fatal("animated action command did not batch a spinner tick")
	}

	t.Setenv("TAKT_NO_ANIMATION", "1")
	m2 := tui.WithStack(newStub(false))
	_, cmd2 := step(t, m2, runtime.ActionRequest{ID: 43})
	if hasTick(cmd2) {
		t.Fatal("TAKT_NO_ANIMATION action command still batched a spinner tick")
	}
}

// A result the controller's runtime never requested is ignored.
func TestStaleActionResultIsIgnored(t *testing.T) {
	ctrlC := tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	m := tui.WithStack(newStub(false))
	m, _ = step(t, m, runtime.ActionRequest{ID: 11})
	m, _ = step(t, m, runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: 12}})
	// A clean flow quits on ctrl+c only when no action runs.
	if _, cmd := step(t, m, ctrlC); isQuit(cmd) {
		t.Fatal("a foreign result ended the running action")
	}
	m, _ = step(t, m, runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: 11}})
	if _, cmd := step(t, m, ctrlC); !isQuit(cmd) {
		t.Fatal("the matching result did not end the running action")
	}
}

// The controller owns the terminal-level view fields (PR-TUI-13): alt
// screen, no mouse, no focus reporting, and the painted dark backgrounds.
// Flows declare content only.
func TestControllerDeclaresTerminalViewFields(t *testing.T) {
	// Restore the mode the original environment selects, not the temporary one.
	prior := theme.DetectMode()
	t.Cleanup(func() { theme.SetMode(prior) })
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	theme.SetMode(theme.ModeColor)
	for _, m := range []tui.Model{tui.New(t.TempDir()), tui.WithStack(newStub(false))} {
		v := m.View()
		if !v.AltScreen || v.MouseMode != tea.MouseModeNone || v.ReportFocus {
			t.Fatalf("controller must own alt/mouse/focus: %+v", v)
		}
		if v.BackgroundColor != theme.Canvas || v.ForegroundColor != theme.TextPrimary {
			t.Fatalf("controller must paint the dark canvas: bg=%v fg=%v", v.BackgroundColor, v.ForegroundColor)
		}
	}
}

// The program's reported color profile selects the palette: hints only seed
// the first frame.
func TestColorProfileSelectsThePaletteMode(t *testing.T) {
	t.Cleanup(func() { theme.SetMode(theme.ModeColor) })
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	m := tui.New(t.TempDir())
	for _, step := range []struct {
		profile colorprofile.Profile
		want    color.Color
	}{
		{colorprofile.ANSI256, theme.Dark256.TextPrimary},
		{colorprofile.ANSI, lipgloss.NoColor{}},
		{colorprofile.TrueColor, theme.Dark.TextPrimary},
	} {
		m.Update(tea.ColorProfileMsg{Profile: step.profile})
		if theme.TextPrimary != step.want {
			t.Errorf("after %v: text.primary = %v, want %v", step.profile, theme.TextPrimary, step.want)
		}
	}
}

// NO_COLOR forces monochrome whatever its value: the reported profile never
// colors the surface back.
func TestColorProfileKeepsNoColorMonochrome(t *testing.T) {
	t.Cleanup(func() { theme.SetMode(theme.ModeColor) })
	t.Setenv("NO_COLOR", "0")
	m := tui.New(t.TempDir())
	m.Update(tea.ColorProfileMsg{Profile: colorprofile.TrueColor})
	if !theme.Mono() {
		t.Fatalf("NO_COLOR=0 with a true-color profile rendered color: text.primary = %v", theme.TextPrimary)
	}
}
