package tui_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui"
	"github.com/rou-cru/takt-ai/takt/tui/install"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

const (
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

// installedRoot records an installation in a fresh root.
func installedRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setup.PlanRequest{}); err != nil {
		t.Fatal(err)
	}
	return root
}

// installedApp builds the menu over a root with a recorded installation.
func installedApp(t *testing.T) tui.Model { return tui.New(installedRoot(t)) }

// onMenu reports whether the home menu is on screen with no flow open.
func onMenu(m tui.Model) bool {
	return m.CurrentRoute() == tui.RouteMenu && strings.Contains(screenText(m), ui.TextMenuQuit)
}

func TestInstalledMenuShowsEveryOperationAndOpensEachRoute(t *testing.T) {
	menu := screenText(installedApp(t))
	for _, label := range []string{ui.TextMenuConfigure, ui.TextMenuAssignModels, ui.TextMenuCheckDrift, ui.TextMenuUninstall, ui.TextMenuDiagnostics, ui.TextMenuQuit} {
		if !strings.Contains(menu, label) {
			t.Errorf("installed menu missing %q:\n%s", label, menu)
		}
	}

	wantRoutes := []tui.Route{tui.RouteInstall, tui.RouteModels, tui.RouteDrift, tui.RouteUninstall, tui.RouteDiagnostics}
	for index, route := range wantRoutes {
		t.Run(string(route), func(t *testing.T) {
			m := installedApp(t)
			for range index {
				m, _ = step(t, m, keyDown)
			}
			m, _ = step(t, m, keyEnter)
			if m.CurrentRoute() != route || onMenu(m) {
				t.Fatalf("route = %v, want %v opened over the menu", m.CurrentRoute(), route)
			}
			// One step back reaches the menu: the route opened a single flow.
			m, _ = step(t, m, ui.BackMsg{})
			if !onMenu(m) {
				t.Errorf("route = %v, want back on the menu", m.CurrentRoute())
			}
		})
	}
}

func TestMenuCursorWrapsAndQuitEntryQuits(t *testing.T) {
	m := installedApp(t)
	// Up from the top wraps to Quit.
	m, _ = step(t, m, keyUp)
	if _, cmd := step(t, m, keyEnter); !isQuit(cmd) {
		t.Fatal("Enter after up from the top did not quit")
	}
	// j from Quit wraps to the first operation.
	if opened, _ := step(t, step1(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"}), keyEnter); opened.CurrentRoute() != tui.RouteInstall {
		t.Errorf("Enter after j from Quit opened %v, want install", opened.CurrentRoute())
	}
	// k from the top wraps back to Quit.
	m = step1(t, step1(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"}), tea.KeyPressMsg{Code: 'k', Text: "k"})
	if _, cmd := step(t, m, keyEnter); !isQuit(cmd) {
		t.Error("Enter after k from the top did not quit")
	}
	if _, cmd := step(t, m, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}); !isQuit(cmd) {
		t.Error("ctrl+c on the menu did not quit")
	}
	if _, cmd := step(t, m, struct{}{}); cmd != nil {
		t.Error("an unknown message on the menu produced a command")
	}
}

// step1 is step without its command.
func step1(t *testing.T, m tui.Model, msg tea.Msg) tui.Model {
	t.Helper()
	next, _ := step(t, m, msg)
	return next
}

func TestMenuClampsCursorWhenItemsShrink(t *testing.T) {
	root := installedRoot(t)
	m := step1(t, tui.New(root), keyUp) // on Quit, the last of six entries
	if err := os.Remove(filepath.Join(root, setup.InstalledConfigFilename)); err != nil {
		t.Fatal(err)
	}
	// Uninstalled: Install and Quit only. The cursor clamps onto Quit, so
	// down wraps to Install instead of leaving the visible entries.
	m = step1(t, m, keyDown)
	if opened, cmd := step(t, m, keyEnter); isQuit(cmd) || opened.CurrentRoute() != tui.RouteInstall {
		t.Errorf("Enter after the menu shrank opened %v, want install", opened.CurrentRoute())
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
	if got := screenText(tui.New(root)); !strings.Contains(got, "did not finish") || !strings.Contains(got, record.Notice(root)[1]) {
		t.Errorf("menu = %q, want the incomplete-operation notice", got)
	}
}

func TestMenuAdaptsToTerminalSize(t *testing.T) {
	for _, size := range []tea.WindowSizeMsg{{Width: 200, Height: 60}, {Width: minWidth, Height: minHeight}} {
		m, _ := step(t, tui.New(t.TempDir()), size)
		if got := screenText(m); !strings.Contains(got, ui.TextMenuInstall) || !strings.Contains(got, ui.TextMenuQuit) {
			t.Errorf("menu at %dx%d lost its entries:\n%s", size.Width, size.Height, got)
		}
	}
	tiny, _ := step(t, tui.New(t.TempDir()), tea.WindowSizeMsg{Width: tinyWidth, Height: tinyHeight})
	if got := screenText(tiny); !strings.Contains(got, "too small") {
		t.Errorf("menu at %dx%d = %q, want the terminal-too-small notice", tinyWidth, tinyHeight, got)
	}
}

func TestMonoModeSkipsCanvasPainting(t *testing.T) {
	theme.SetMode(theme.ModeMono)
	t.Cleanup(func() { theme.SetMode(theme.ModeColor) })
	v := tui.New(t.TempDir()).View()
	if v.BackgroundColor != nil || v.ForegroundColor != nil {
		t.Errorf("mono view painted bg=%v fg=%v, want no canvas colors", v.BackgroundColor, v.ForegroundColor)
	}
	if !v.AltScreen {
		t.Error("mono view dropped the alternate screen")
	}
}

func TestGuardOverlayNamesTheTaskAndNavigates(t *testing.T) {
	ctrlB := tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	m := step1(t, tui.WithStack(titledStub{newStub(true)}), ctrlB)
	if !guardOpen(m) {
		t.Fatal("ctrl+b on a dirty flow did not open the guard")
	}
	got := screenText(m)
	// The header shows the step after the task: the unapplied marker.
	for _, want := range []string{ui.TextGuardUnappliedWord, ui.TextGuardDiscardBody, ui.TextGuardKeepEditing, ui.TextGuardDiscard} {
		if !strings.Contains(got, want) {
			t.Errorf("guard view missing %q:\n%s", want, got)
		}
	}

	// j selects Discard: Enter pops the flow.
	if discarded := step1(t, step1(t, m, tea.KeyPressMsg{Code: 'j', Text: "j"}), keyEnter); !onMenu(discarded) {
		t.Fatalf("Enter after j left route %v, want Discard to pop to the menu", discarded.CurrentRoute())
	}
	// k, down, up end on Keep editing: Enter keeps the flow.
	for _, key := range []tea.KeyPressMsg{{Code: 'j', Text: "j"}, {Code: 'k', Text: "k"}, keyDown, keyUp} {
		m = step1(t, m, key)
	}
	if _, cmd := step(t, m, tea.KeyPressMsg{Code: 'x', Text: "x"}); cmd != nil {
		t.Error("an unrelated key produced a guard command")
	}
	if kept := step1(t, m, keyEnter); guardOpen(kept) || kept.CurrentRoute() != tui.RouteInstall {
		t.Errorf("Enter after down, up left guard=%v route=%v, want Keep editing", guardOpen(kept), kept.CurrentRoute())
	}
	// Paste never reaches the flow while the guard is open.
	flow := titledStub{newStub(true)}
	guarded := tui.WithStack(flow)
	guarded, _ = step(t, guarded, ui.BackMsg{})
	step(t, guarded, tea.PasteMsg{Content: "pasted"})
	if flow.count(func(msg tea.Msg) bool { _, ok := msg.(tea.PasteMsg); return ok }) != 0 {
		t.Error("paste reached the flow behind the guard")
	}
}

func TestGuardTitleFallsBackForUntitledFlows(t *testing.T) {
	m := step1(t, tui.WithStack(newStub(true)), ui.BackMsg{})
	if got := screenText(m); !strings.Contains(got, ui.TextGuardUnappliedWord) {
		t.Errorf("guard view = %q, want the %q title", got, ui.TextGuardUnappliedWord)
	}
}

func TestPasteReachesTheForegroundFlowOrIsDroppedOnTheMenu(t *testing.T) {
	flow := newStub(false)
	m := tui.WithStack(flow)
	step(t, m, tea.PasteMsg{Content: "hello"})
	if flow.count(func(msg tea.Msg) bool { p, ok := msg.(tea.PasteMsg); return ok && p.Content == "hello" }) != 1 {
		t.Error("paste did not reach the foreground flow")
	}
	if _, cmd := step(t, tui.New(t.TempDir()), tea.PasteMsg{Content: "hello"}); cmd != nil {
		t.Error("paste on the menu produced a command")
	}
}

func TestOpenModelsReplacesTheInstallScreen(t *testing.T) {
	m := installedApp(t)
	m, _ = step(t, m, keyEnter)
	if m.CurrentRoute() != tui.RouteInstall {
		t.Fatalf("route = %v, want install", m.CurrentRoute())
	}
	m, _ = step(t, m, install.OpenModelsMsg{})
	if m.CurrentRoute() != tui.RouteModels {
		t.Fatalf("route = %v, want models", m.CurrentRoute())
	}
	// The models flow replaced install: one step back reaches the menu.
	if m, _ = step(t, m, ui.BackMsg{}); !onMenu(m) {
		t.Errorf("route = %v, want the models flow alone on the stack", m.CurrentRoute())
	}
}

func TestClosingWithAnEmptyStackReturnsToMenu(t *testing.T) {
	m, _ := step(t, tui.WithStack(), ui.BackMsg{})
	if m.CurrentRoute() != tui.RouteMenu {
		t.Errorf("route = %v, want the menu", m.CurrentRoute())
	}
}

func TestActionMessagesOnTheMenuAndForeignProgress(t *testing.T) {
	if _, cmd := step(t, tui.New(t.TempDir()), runtime.ActionRequest{ID: 5}); cmd == nil {
		t.Error("a request with no active flow was not executed")
	}

	flow := newStub(false)
	m := tui.WithStack(flow)
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
	if err := tui.Run(strings.NewReader(""), &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "resolve home directory") {
		t.Errorf("Run() error = %v, want a home directory error", err)
	}
}

func TestRunQuitsFromTheMenu(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	done := make(chan error, 1)
	var out bytes.Buffer
	go func() { done <- tui.Run(strings.NewReader("q"), &out) }()
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
