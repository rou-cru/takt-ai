package tui

import (
	"bytes"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/install"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

const (
	// installedMenuEntries counts the menu rows of an installed root, Quit included.
	installedMenuEntries = 6
	// minWidth and minHeight are the smallest terminal the shell supports.
	minWidth  = 60
	minHeight = 20
	// tinyWidth and tinyHeight are below that minimum.
	tinyWidth  = 30
	tinyHeight = 8
	// runTimeout bounds the end-to-end program run.
	runTimeout = 10 * time.Second
)

var (
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyUp    = tea.KeyPressMsg{Code: tea.KeyUp}
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
)

// titledStub is a flow that names its task, as the real flows do.
type titledStub struct{ stubFlow }

func (titledStub) Title() string { return "Install · Review" }

func (s titledStub) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, cmd := s.stubFlow.Update(msg)
	s.stubFlow = next.(stubFlow)
	return s, cmd
}

// installedApp builds the menu over a root with a recorded installation.
func installedApp(t *testing.T) Model {
	t.Helper()
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setup.PlanRequest{}); err != nil {
		t.Fatal(err)
	}
	return New(root)
}

func screenText(m Model) string { return ansi.Strip(m.View().Content) }

func TestInstalledMenuShowsEveryOperationAndOpensEachRoute(t *testing.T) {
	menu := screenText(installedApp(t))
	for _, label := range []string{ui.TextMenuConfigure, ui.TextMenuAssignModels, ui.TextMenuCheckDrift, ui.TextMenuUninstall, ui.TextMenuDiagnostics, ui.TextMenuQuit} {
		if !strings.Contains(menu, label) {
			t.Errorf("installed menu missing %q:\n%s", label, menu)
		}
	}

	wantRoutes := []Route{RouteInstall, RouteModels, RouteDrift, RouteUninstall, RouteDiagnostics}
	for index, route := range wantRoutes {
		t.Run(string(route), func(t *testing.T) {
			m := installedApp(t)
			for range index {
				m, _ = step(t, m, keyDown)
			}
			m, _ = step(t, m, keyEnter)
			if m.CurrentRoute() != route || len(m.stack) != 1 {
				t.Fatalf("route = %v depth = %d, want %v opened", m.CurrentRoute(), len(m.stack), route)
			}
			m, _ = step(t, m, ui.BackMsg{})
			if m.CurrentRoute() != RouteMenu || len(m.stack) != 0 {
				t.Errorf("route = %v depth = %d, want back on the menu", m.CurrentRoute(), len(m.stack))
			}
		})
	}
}

func TestMenuCursorWrapsAndQuitEntryQuits(t *testing.T) {
	m := installedApp(t)
	m, _ = step(t, m, keyUp)
	if m.cursor != installedMenuEntries-1 {
		t.Fatalf("cursor after up from the top = %d, want it to wrap to Quit", m.cursor)
	}
	if _, cmd := step(t, m, keyEnter); !isQuit(cmd) {
		t.Error("Enter on Quit did not quit")
	}
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.cursor != 0 {
		t.Errorf("cursor after j from Quit = %d, want it to wrap to the top", m.cursor)
	}
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	if m.cursor != installedMenuEntries-1 {
		t.Errorf("cursor after k from the top = %d, want it to wrap to Quit", m.cursor)
	}
	if _, cmd := step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); !isQuit(cmd) {
		t.Error("ctrl+c on the menu did not quit")
	}
	if _, cmd := step(t, m, struct{}{}); cmd != nil {
		t.Error("an unknown message on the menu produced a command")
	}
	if got := m.flowRun(); got.Busy() {
		t.Error("the menu reports a running action")
	}
}

func TestMenuClampsCursorWhenItemsShrink(t *testing.T) {
	m := New(t.TempDir()) // fresh root: Install and Quit only
	m.cursor = installedMenuEntries
	m, _ = step(t, m, keyDown)
	if m.cursor > 1 {
		t.Errorf("cursor = %d, want it clamped into the two visible entries", m.cursor)
	}
}

func TestMenuShowsIncompleteOperationNotice(t *testing.T) {
	root := t.TempDir()
	if _, err := setup.BeginOperation(root, "install"); err != nil {
		t.Fatal(err)
	}
	record, found := setup.IncompleteOperation(root)
	if !found {
		t.Fatal("test setup left no incomplete operation record")
	}
	if got := screenText(New(root)); !strings.Contains(got, "did not finish") || !strings.Contains(got, record.Notice(root)[1]) {
		t.Errorf("menu = %q, want the incomplete-operation notice", got)
	}
}

func TestMenuAdaptsToTerminalSize(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 200, Height: 60}, {Width: minWidth, Height: minHeight}} {
		m, _ := step(t, New(t.TempDir()), size)
		if got := screenText(m); !strings.Contains(got, ui.TextMenuInstall) || !strings.Contains(got, ui.TextMenuQuit) {
			t.Errorf("menu at %dx%d lost its entries:\n%s", size.Width, size.Height, got)
		}
	}
	tiny, _ := step(t, New(t.TempDir()), tea.WindowSizeMsg{Width: tinyWidth, Height: tinyHeight})
	if got := screenText(tiny); !strings.Contains(got, "too small") {
		t.Errorf("menu at %dx%d = %q, want the terminal-too-small notice", tinyWidth, tinyHeight, got)
	}
}

func TestMonoModeSkipsCanvasPainting(t *testing.T) {
	theme.SetMode(theme.ModeMono)
	t.Cleanup(func() { theme.SetMode(theme.ModeColor) })
	v := New(t.TempDir()).View()
	if v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Errorf("mono view painted bg=%v fg=%v, want no canvas colors", v.BackgroundColor, v.ForegroundColor)
	}
	if !v.AltScreen {
		t.Error("mono view dropped the alternate screen")
	}
}

func TestGuardOverlayNamesTheTaskAndNavigates(t *testing.T) {
	m := withStack(titledStub{newStub(true)})
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if m.guard.state != guardOpen {
		t.Fatal("ctrl+b on a dirty flow did not open the guard")
	}
	if got := m.guardTitle(); got != "Install · "+ui.TextGuardUnappliedWord {
		t.Errorf("guardTitle() = %q, want the task name with the unapplied marker", got)
	}
	got := screenText(m)
	for _, want := range []string{ui.TextGuardUnappliedWord, ui.TextGuardDiscardBody, ui.TextGuardKeepEditing, ui.TextGuardDiscard} {
		if !strings.Contains(got, want) {
			t.Errorf("guard view missing %q:\n%s", want, got)
		}
	}

	m, _ = step(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"})
	if m.guard.cursor != 1 {
		t.Fatalf("guard cursor after j = %d, want Discard", m.guard.cursor)
	}
	m, _ = step(t, m, tea.KeyPressMsg{Code: 'k', Text: "k"})
	m, _ = step(t, m, keyDown)
	m, _ = step(t, m, keyUp)
	if m.guard.cursor != 0 {
		t.Errorf("guard cursor after down, up = %d, want Keep editing", m.guard.cursor)
	}
	if _, cmd := step(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"}); cmd != nil {
		t.Error("an unrelated key produced a guard command")
	}
	// Paste never reaches the flow while the guard is open.
	flow := titledStub{newStub(true)}
	guarded := withStack(flow)
	guarded, _ = step(t, guarded, ui.BackMsg{})
	step(t, guarded, tea.PasteMsg{Content: "pasted"})
	if flow.count(func(msg tea.Msg) bool { _, ok := msg.(tea.PasteMsg); return ok }) != 0 {
		t.Error("paste reached the flow behind the guard")
	}
}

func TestGuardTitleFallsBackForUntitledFlows(t *testing.T) {
	m := withStack(newStub(true))
	m, _ = step(t, m, ui.BackMsg{})
	if got := m.guardTitle(); got != ui.TextGuardUnappliedWord {
		t.Errorf("guardTitle() = %q, want %q", got, ui.TextGuardUnappliedWord)
	}
}

func TestPasteReachesTheForegroundFlowOrIsDroppedOnTheMenu(t *testing.T) {
	flow := newStub(false)
	m := withStack(flow)
	step(t, m, tea.PasteMsg{Content: "hello"})
	if flow.count(func(msg tea.Msg) bool { p, ok := msg.(tea.PasteMsg); return ok && p.Content == "hello" }) != 1 {
		t.Error("paste did not reach the foreground flow")
	}
	if _, cmd := step(t, New(t.TempDir()), tea.PasteMsg{Content: "hello"}); cmd != nil {
		t.Error("paste on the menu produced a command")
	}
}

func TestOpenModelsReplacesTheInstallScreen(t *testing.T) {
	m := installedApp(t)
	m, _ = step(t, m, keyEnter)
	if m.CurrentRoute() != RouteInstall {
		t.Fatalf("route = %v, want install", m.CurrentRoute())
	}
	m, _ = step(t, m, install.OpenModelsMsg{})
	if m.CurrentRoute() != RouteModels || len(m.stack) != 1 {
		t.Errorf("route = %v depth = %d, want the models flow alone on the stack", m.CurrentRoute(), len(m.stack))
	}
}

func TestClosingWithAnEmptyStackReturnsToMenu(t *testing.T) {
	m := New(t.TempDir())
	m.route = RouteInstall
	m, _ = step(t, m, ui.BackMsg{})
	if m.CurrentRoute() != RouteMenu {
		t.Errorf("route = %v, want the menu", m.CurrentRoute())
	}
}

func TestActionMessagesOnTheMenuAndForeignProgress(t *testing.T) {
	m := New(t.TempDir())
	if _, cmd := step(t, m, runtime.ActionRequest{ID: 5}); cmd == nil {
		t.Error("a request with no active flow was not executed")
	}
	if m.actionCancel != nil {
		t.Error("New() model already holds an action context")
	}

	flow := newStub(false)
	m = withStack(flow)
	m, _ = step(t, m, runtime.ActionRequest{ID: 9})
	isProgress := func(msg tea.Msg) bool { _, ok := msg.(runtime.ActionProgressMsg); return ok }
	m, _ = step(t, m, runtime.ActionProgressMsg{Request: runtime.ActionRequest{ID: 10}})
	if flow.count(isProgress) != 0 {
		t.Error("progress for a foreign request reached the flow")
	}
	step(t, m, runtime.ActionProgressMsg{Request: runtime.ActionRequest{ID: 9}})
	if flow.count(isProgress) != 1 {
		t.Error("progress for the running request did not reach the flow")
	}
}

func TestRunReportsMissingHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")
	if err := Run(strings.NewReader(""), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "resolve home directory") {
		t.Errorf("Run() error = %v, want a home directory error", err)
	}
}

func TestRunQuitsFromTheMenu(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() { done <- Run(strings.NewReader("q"), &out) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	case <-time.After(runTimeout):
		t.Fatal("Run() did not return after q")
	}
	if !strings.Contains(out.String(), "\x1b[?1049h") {
		t.Errorf("program output = %q, want the alternate screen entered", out.String())
	}
}
