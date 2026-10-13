package tui_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui"
	"github.com/rou-cru/takt-ai/takt/tui/drift"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
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

// recordedAt returns a root recorded as installed by version, run by a build
// of current.
func recordedAt(t *testing.T, version, current string) string {
	t.Helper()
	previous := setup.BuildVersion
	t.Cleanup(func() { setup.BuildVersion = previous })
	root := t.TempDir()
	setup.BuildVersion = version
	if err := setup.RecordInstallation(root, setuputil.TestPlanRequest()); err != nil {
		t.Fatalf("RecordInstallation() error = %v", err)
	}
	setup.BuildVersion = current
	return root
}

// openFromMenu moves down steps menu rows and opens the focused task.
func openFromMenu(app tui.Model, steps int) tui.Model {
	app, _ = update(app, tea.WindowSizeMsg{Width: 200, Height: 50})
	for range steps {
		app, _ = update(app, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	app, _ = update(app, tea.KeyPressMsg{Code: tea.KeyEnter})
	return app
}

// upgradeRow is the Upgrade task's position after Configure, Assign models
// and Check for drift.
const upgradeRow = 3

// TestUpgradeIsATaskOnlyWhenThisBuildIsNewer checks that the menu offers Upgrade only when this
// build is newer than the installation.
func TestUpgradeIsATaskOnlyWhenThisBuildIsNewer(t *testing.T) {
	newer := tui.New(recordedAt(t, "0.0.25", "0.0.27"))
	newer, _ = update(newer, tea.WindowSizeMsg{Width: 200, Height: 50})
	if !strings.Contains(ansi.Strip(newer.View().Content), ui.TextMenuUpgrade) {
		t.Error("menu hides Upgrade although this build is newer")
	}
	if got := openFromMenu(tui.New(recordedAt(t, "0.0.25", "0.0.27")), upgradeRow).CurrentRoute(); got != tui.RouteUpgrade {
		t.Errorf("route = %q, want the upgrade flow", got)
	}
	if got := openFromMenu(tui.New(recordedAt(t, "0.0.27", "0.0.27")), upgradeRow).CurrentRoute(); got != tui.RouteUninstall {
		t.Errorf("route = %q, want Uninstall in Upgrade's place when nothing is newer", got)
	}
}

// TestDriftHandsVersionDriftToUpgrade checks that the drift check hands version drift to the
// upgrade flow.
func TestDriftHandsVersionDriftToUpgrade(t *testing.T) {
	app := openFromMenu(tui.New(recordedAt(t, "0.0.25", "0.0.27")), upgradeRow-1)
	if app.CurrentRoute() != tui.RouteDrift {
		t.Fatalf("route = %q, want drift", app.CurrentRoute())
	}
	app, _ = update(app, drift.OpenUpgradeMsg{})
	if app.CurrentRoute() != tui.RouteUpgrade {
		t.Errorf("route = %q, want the upgrade flow", app.CurrentRoute())
	}
}
