package tui_test

import (
	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/tui"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"strings"
	"testing"
)

func TestFreshMenuHidesUnavailableOperations(t *testing.T) {
	app := tui.New(t.TempDir())
	for range 8 {
		app, _ = update(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	app, _ = update(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if app.CurrentRoute() != tui.RouteInstall {
		t.Fatal("hidden items remain navigable")
	}
}

func TestActionResultAndNavigation(t *testing.T) {
	app := tui.New(t.TempDir())
	app, _ = update(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	// The install flow opens on review with Install focused.
	app, cmd := update(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	request := cmd().(runtime.ActionRequest)
	app, cmd = update(app, request)
	if cmd == nil {
		t.Fatal("request not deferred")
	}
	app, _ = update(app, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{Action: runtime.ActionInstall}})
	app, _ = update(app, tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if app.CurrentRoute() != tui.RouteMenu {
		t.Fatal("did not return to menu")
	}
}

func TestQuitIsNavigation(t *testing.T) {
	_, cmd := update(tui.New(t.TempDir()), tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("missing quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("not quit")
	}
}

func update(app tui.Model, msg tea.Msg) (tui.Model, tea.Cmd) {
	next, cmd := app.Update(msg)
	return next.(tui.Model), cmd
}

func TestChildReceivesInitialSizeAndResultHomeReallyReturns(t *testing.T) {
	app := tui.New(t.TempDir())
	app, _ = update(app, tea.WindowSizeMsg{Width: 100, Height: 30})
	app, _ = update(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	assertSize := func() {
		t.Helper()
		lines := strings.Split(app.View().Content, "\n")
		if len(lines) > 30 {
			t.Fatalf("child exceeded terminal height: %d", len(lines))
		}
	}
	assertSize()
	// The install flow opens on review with Install focused.
	app, cmd := update(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	request := cmd().(runtime.ActionRequest)
	app, _ = update(app, request)
	app, _ = update(app, runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{Action: runtime.ActionInstall}})
	assertSize()
	// The result actions are horizontal: move from model assignment to Back.
	app, _ = update(app, tea.KeyPressMsg{Code: tea.KeyRight})
	app, cmd = update(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Home did not emit navigation")
	}
	app, _ = update(app, cmd())
	if app.CurrentRoute() != tui.RouteMenu {
		t.Fatal("Home returned to install instead of menu")
	}
	assertSize()
}
