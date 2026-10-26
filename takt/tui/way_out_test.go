package tui_test

import (
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui"
	"github.com/rou-cru/takt-ai/takt/tui/models"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

var (
	wayOutEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	wayOutEsc   = tea.KeyPressMsg{Code: tea.KeyEscape}
	wayOutLeft  = tea.KeyPressMsg{Code: tea.KeyLeft}
	wayOutRight = tea.KeyPressMsg{Code: tea.KeyRight}
	wayOutSpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	wayOutCtrlC = tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	wayOutQ     = tea.KeyPressMsg{Code: 'q', Text: "q"}
)

func wayOutStep(t *testing.T, app tui.Model, msg tea.Msg) (tui.Model, tea.Cmd) {
	t.Helper()
	next, cmd := app.Update(msg)
	return next.(tui.Model), cmd
}

func wayOutScreen(app tui.Model) string { return ansi.Strip(app.View().Content) }

// wayOutSized opens the controller at a fixed terminal size.
func wayOutSized(t *testing.T, app tui.Model) tui.Model {
	t.Helper()
	app, _ = wayOutStep(t, app, tea.WindowSizeMsg{Width: 120, Height: 40})
	return app
}

// wayOutMessages runs a command produced by a key press and flattens batches.
// It is only used on commands that start no lifecycle action.
func wayOutMessages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var all []tea.Msg
		for _, inner := range batch {
			all = append(all, wayOutMessages(inner)...)
		}
		return all
	}
	return []tea.Msg{msg}
}

func wayOutQuits(cmd tea.Cmd) bool {
	return slices.ContainsFunc(wayOutMessages(cmd), func(msg tea.Msg) bool { _, ok := msg.(tea.QuitMsg); return ok })
}

// wayOutPress sends a key and feeds back the Back request it produces, the
// way the program loop would.
func wayOutPress(t *testing.T, app tui.Model, key tea.KeyPressMsg) tui.Model {
	t.Helper()
	app, cmd := wayOutStep(t, app, key)
	if wayOutQuits(cmd) {
		t.Fatalf("key %q quit the program", key.String())
	}
	for _, msg := range wayOutMessages(cmd) {
		if back, ok := msg.(ui.BackMsg); ok {
			app, _ = wayOutStep(t, app, back)
		}
	}
	return app
}

// wayOutSnapshot records every file and its bytes under root.
func wayOutSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		files[path] = string(raw)
		return err
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", root, err)
	}
	return files
}

// From both blocked-model states, Enter on the labeled Back to menu
// action reaches the menu with no mutation.
func TestBlockedModelReturnsToMenu(t *testing.T) {
	// No installation record, and one that fails to load.
	missing, malformed := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(malformed, setup.InstalledConfigFilename), []byte("{"), 0o600); err != nil {
		t.Fatalf("write malformed record: %v", err)
	}
	for name, root := range map[string]string{"no installation": missing, "configuration-load failure": malformed} {
		t.Run(name, func(t *testing.T) {
			before := wayOutSnapshot(t, root)
			app := wayOutSized(t, tui.WithStack(models.New(root)))
			screen := wayOutScreen(app)
			reason := strings.Contains(screen, strings.TrimSpace(ui.TextModelsLoadFail)) || strings.Contains(screen, ui.TextModelsNothing)
			if !reason || !strings.Contains(screen, ui.TextActionBackToMenu) {
				t.Fatalf("blocked-model screen must state why nothing is assignable and offer Back to menu:\n%s", screen)
			}
			app = wayOutPress(t, app, wayOutEnter)
			if app.CurrentRoute() != tui.RouteMenu {
				t.Fatalf("Back to menu left the route at %q, want the menu", app.CurrentRoute())
			}
			if !maps.Equal(before, wayOutSnapshot(t, root)) {
				t.Fatal("leaving the blocked-model screen changed the file tree")
			}
		})
	}
}

// wayOutOpenInstall opens the install flow from a fresh menu, on its review.
func wayOutOpenInstall(t *testing.T) tui.Model {
	t.Helper()
	app := wayOutSized(t, tui.New(t.TempDir()))
	app, _ = wayOutStep(t, app, wayOutEnter)
	return app
}

// A clean exit asks nothing; leaving an unapplied edit offers keep
// editing or discard, and each choice reaches its destination.
func TestPendingChangeGuard(t *testing.T) {
	t.Run("clean exit", func(t *testing.T) {
		app := wayOutPress(t, wayOutOpenInstall(t), wayOutEsc)
		if app.CurrentRoute() != tui.RouteMenu {
			t.Fatalf("clean Esc left the route at %q, want the menu", app.CurrentRoute())
		}
	})

	// Personalize, toggle a component, and return to the review: an
	// unapplied draft.
	dirty := func(t *testing.T) tui.Model {
		t.Helper()
		app := wayOutOpenInstall(t)
		for _, key := range []tea.KeyPressMsg{wayOutLeft, wayOutEnter, wayOutSpace, wayOutEsc} {
			app, _ = wayOutStep(t, app, key)
		}
		return app
	}
	guarded := func(t *testing.T, app tui.Model, key tea.KeyPressMsg) tui.Model {
		t.Helper()
		app = wayOutPress(t, app, key)
		screen := wayOutScreen(app)
		if !strings.Contains(screen, ui.TextGuardKeepEditing) || !strings.Contains(screen, ui.TextGuardDiscard) || app.CurrentRoute() != tui.RouteInstall {
			t.Fatalf("leaving an unapplied edit must stay on the edit and offer keep editing or discard:\n%s", screen)
		}
		return app
	}

	t.Run("keep editing", func(t *testing.T) {
		app := guarded(t, dirty(t), wayOutEsc)
		app = wayOutPress(t, app, wayOutEnter)
		// The draft survives: leaving again still asks.
		guarded(t, app, wayOutEsc)
	})
	t.Run("discard", func(t *testing.T) {
		app := guarded(t, dirty(t), wayOutEsc)
		app, _ = wayOutStep(t, app, wayOutRight)
		app = wayOutPress(t, app, wayOutEnter)
		if app.CurrentRoute() != tui.RouteMenu {
			t.Fatalf("Discard changes left the route at %q, want the menu", app.CurrentRoute())
		}
	})
}

// During mutation Ctrl+C shows cancellation pending, and neither exit
// nor navigation takes effect before a typed result; the result screen then
// offers a labeled exit.
func TestCancellationDuringMutation(t *testing.T) {
	app := wayOutOpenInstall(t)
	app, cmd := wayOutStep(t, app, wayOutEnter)
	request, ok := wayOutMessages(cmd)[0].(runtime.ActionRequest)
	if !ok {
		t.Fatal("the review commit did not emit an action request")
	}
	// The executor command is returned, never run: no file is written.
	app, _ = wayOutStep(t, app, request)

	// wayOutPress fails on a quit, so Ctrl+C may not exit.
	app = wayOutPress(t, app, wayOutCtrlC)
	if app.CurrentRoute() != tui.RouteInstall || !strings.Contains(wayOutScreen(app), ui.TextCancelRequested) {
		t.Fatalf("Ctrl+C during mutation navigated to %q or hid cancellation pending", app.CurrentRoute())
	}

	stopped := runtime.ActionResult{Action: runtime.ActionInstall, Outcome: lifecycle.OutcomeCancelledNothingApplied, CancelRequested: true}
	app, _ = wayOutStep(t, app, runtime.ActionResultMsg{Request: request, Result: stopped})
	if screen := wayOutScreen(app); strings.Contains(screen, ui.TextCancelRequested) || !strings.Contains(screen, ui.TextActionBackToMenu) {
		t.Fatalf("the typed result screen must drop cancellation pending and offer a labeled exit:\n%s", screen)
	}
	app, _ = wayOutStep(t, app, wayOutRight)
	app = wayOutPress(t, app, wayOutEnter)
	if app.CurrentRoute() != tui.RouteMenu {
		t.Fatalf("Back to menu on the result left the route at %q", app.CurrentRoute())
	}
}

// The other screens reach a valid destination through their existing action.
func TestOtherScreensWayOut(t *testing.T) {
	home := wayOutSized(t, tui.New(t.TempDir()))
	if _, cmd := wayOutStep(t, home, wayOutQ); !wayOutQuits(cmd) {
		t.Fatal("Quit on the idle menu does not exit")
	}
}
