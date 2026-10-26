package tui_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
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
	wayOutCtrlB = tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl}
	wayOutQ     = tea.KeyPressMsg{Code: 'q', Text: "q"}
)

// wayOutKeyHint matches a key bar or movement hint in rendered copy.
var wayOutKeyHint = regexp.MustCompile(`(?i)\b(esc|ctrl\+[a-z]|pgup|pgdn|pgdown)\b`)

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
	for _, msg := range wayOutMessages(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

// wayOutPress sends a key and feeds back the Back request it produces, the
// way the program loop would.
func wayOutPress(t *testing.T, app tui.Model, key tea.KeyPressMsg) tui.Model {
	t.Helper()
	app, cmd := wayOutStep(t, app, key)
	for _, msg := range wayOutMessages(cmd) {
		if _, ok := msg.(tea.QuitMsg); ok {
			t.Fatalf("key %q quit the program", key.String())
		}
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
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[path] = "<dir>"
			return nil
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

func wayOutSameTree(t *testing.T, before, after map[string]string) {
	t.Helper()
	if len(before) != len(after) {
		t.Fatalf("file tree changed: %d entries before, %d after", len(before), len(after))
	}
	for path, content := range before {
		if after[path] != content {
			t.Fatalf("%s changed", path)
		}
	}
}

// wayOutBlockedRoots are the two blocked-model states: no installation record
// and an installation record that fails to load.
func wayOutBlockedRoots(t *testing.T) map[string]string {
	t.Helper()
	missing := t.TempDir()
	malformed := t.TempDir()
	if err := os.WriteFile(filepath.Join(malformed, setup.InstalledConfigFilename), []byte("{"), 0o600); err != nil {
		t.Fatalf("write malformed record: %v", err)
	}
	return map[string]string{"no installation": missing, "configuration-load failure": malformed}
}

// From both blocked-model states, Enter on the labeled Back to menu
// action and Esc each reach the menu with no mutation.
func TestBlockedModelReturnsToMenu(t *testing.T) {
	for name, root := range wayOutBlockedRoots(t) {
		for _, key := range []tea.KeyPressMsg{wayOutEnter, wayOutEsc} {
			t.Run(name+"/"+key.String(), func(t *testing.T) {
				before := wayOutSnapshot(t, root)
				app := wayOutSized(t, tui.WithStack(models.New(root)))
				screen := wayOutScreen(app)
				if !strings.Contains(screen, ui.TextActionBackToMenu) {
					t.Fatalf("blocked-model screen offers no labeled %q action:\n%s", ui.TextActionBackToMenu, screen)
				}
				if !strings.Contains(screen, strings.TrimSpace(ui.TextModelsLoadFail)) && !strings.Contains(screen, ui.TextModelsNothing) {
					t.Fatalf("blocked-model screen does not state why nothing is assignable:\n%s", screen)
				}
				if hint := wayOutKeyHint.FindString(screen); hint != "" {
					t.Fatalf("blocked-model screen shows the key hint %q:\n%s", hint, screen)
				}
				app = wayOutPress(t, app, key)
				if app.CurrentRoute() != tui.RouteMenu {
					t.Fatalf("%s left the route at %q, want the menu", key.String(), app.CurrentRoute())
				}
				if !strings.Contains(wayOutScreen(app), ui.TextMenuQuit) {
					t.Fatal("the menu is not on screen after leaving the blocked-model screen")
				}
				wayOutSameTree(t, before, wayOutSnapshot(t, root))
			})
		}
	}
}

// wayOutOpenInstall opens the install flow from a fresh menu, on its review.
func wayOutOpenInstall(t *testing.T) tui.Model {
	t.Helper()
	app := wayOutSized(t, tui.New(t.TempDir()))
	app, _ = wayOutStep(t, app, wayOutEnter)
	if app.CurrentRoute() != tui.RouteInstall {
		t.Fatalf("route = %q, want the install flow", app.CurrentRoute())
	}
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
		if strings.Contains(wayOutScreen(app), ui.TextGuardDiscardBody) {
			t.Fatal("a clean exit asked about unapplied changes")
		}
	})

	// Personalize, toggle a component, and return to the review: an
	// unapplied draft.
	dirty := func(t *testing.T) tui.Model {
		t.Helper()
		app := wayOutOpenInstall(t)
		app, _ = wayOutStep(t, app, wayOutLeft)
		app, _ = wayOutStep(t, app, wayOutEnter)
		app, _ = wayOutStep(t, app, wayOutSpace)
		app, _ = wayOutStep(t, app, wayOutEsc)
		return app
	}
	guarded := func(t *testing.T, app tui.Model, key tea.KeyPressMsg) tui.Model {
		t.Helper()
		app = wayOutPress(t, app, key)
		screen := wayOutScreen(app)
		for _, want := range []string{ui.TextGuardKeepEditing, ui.TextGuardDiscard} {
			if !strings.Contains(screen, want) {
				t.Fatalf("leaving an unapplied edit with %s does not offer %q:\n%s", key.String(), want, screen)
			}
		}
		if hint := wayOutKeyHint.FindString(screen); hint != "" {
			t.Fatalf("guard shows the key hint %q", hint)
		}
		if app.CurrentRoute() != tui.RouteInstall {
			t.Fatalf("the guard navigated to %q before a choice", app.CurrentRoute())
		}
		return app
	}

	for _, key := range []tea.KeyPressMsg{wayOutEsc, wayOutCtrlB} {
		t.Run("keep editing/"+key.String(), func(t *testing.T) {
			app := guarded(t, dirty(t), key)
			app = wayOutPress(t, app, wayOutEnter)
			if app.CurrentRoute() != tui.RouteInstall || strings.Contains(wayOutScreen(app), ui.TextGuardDiscardBody) {
				t.Fatal("Keep editing did not return to the edit")
			}
			// The draft survives: leaving again still asks.
			guarded(t, app, key)
		})
		t.Run("discard/"+key.String(), func(t *testing.T) {
			app := guarded(t, dirty(t), key)
			app, _ = wayOutStep(t, app, wayOutRight)
			app = wayOutPress(t, app, wayOutEnter)
			if app.CurrentRoute() != tui.RouteMenu {
				t.Fatalf("Discard changes left the route at %q, want the menu", app.CurrentRoute())
			}
		})
	}
}

// During mutation Ctrl+C shows cancellation pending, and neither exit
// nor navigation takes effect before a typed result; the result screen then
// offers a labeled exit.
func TestCancellationDuringMutation(t *testing.T) {
	app := wayOutOpenInstall(t)
	app, cmd := wayOutStep(t, app, wayOutEnter)
	if cmd == nil {
		t.Fatal("the review commit emitted no request")
	}
	request, ok := cmd().(runtime.ActionRequest)
	if !ok {
		t.Fatal("the review commit did not emit an action request")
	}
	// The executor command is returned, never run: no file is written.
	app, _ = wayOutStep(t, app, request)

	app, cmd = wayOutStep(t, app, wayOutCtrlC)
	if wayOutQuits(cmd) {
		t.Fatal("Ctrl+C during mutation quit the program")
	}
	if app.CurrentRoute() != tui.RouteInstall {
		t.Fatalf("Ctrl+C during mutation navigated to %q", app.CurrentRoute())
	}
	if screen := wayOutScreen(app); !strings.Contains(screen, ui.TextCancelRequested) {
		t.Fatalf("Ctrl+C during mutation does not show cancellation pending:\n%s", screen)
	}

	for _, key := range []tea.KeyPressMsg{wayOutCtrlC, wayOutEsc, wayOutCtrlB, wayOutEnter, wayOutQ} {
		next, cmd := wayOutStep(t, app, key)
		if wayOutQuits(cmd) {
			t.Fatalf("%s before the typed result quit the program", key.String())
		}
		for _, msg := range wayOutMessages(cmd) {
			if back, ok := msg.(ui.BackMsg); ok {
				next, _ = wayOutStep(t, next, back)
			}
		}
		if next.CurrentRoute() != tui.RouteInstall {
			t.Fatalf("%s before the typed result navigated to %q", key.String(), next.CurrentRoute())
		}
		if !strings.Contains(wayOutScreen(next), ui.TextCancelRequested) {
			t.Fatalf("%s before the typed result hid cancellation pending", key.String())
		}
		app = next
	}

	stopped := runtime.ActionResult{Action: runtime.ActionInstall, Outcome: lifecycle.OutcomeCancelledNothingApplied, CancelRequested: true}
	app, _ = wayOutStep(t, app, runtime.ActionResultMsg{Request: request, Result: stopped})
	screen := wayOutScreen(app)
	if strings.Contains(screen, ui.TextCancelRequested) {
		t.Fatal("the typed result screen still shows cancellation pending")
	}
	if !strings.Contains(screen, ui.TextActionBackToMenu) {
		t.Fatalf("the typed result screen offers no labeled exit:\n%s", screen)
	}
	if hint := wayOutKeyHint.FindString(screen); hint != "" {
		t.Fatalf("the result screen shows the key hint %q", hint)
	}
	app, _ = wayOutStep(t, app, wayOutRight)
	app = wayOutPress(t, app, wayOutEnter)
	if app.CurrentRoute() != tui.RouteMenu {
		t.Fatalf("Back to menu on the result left the route at %q", app.CurrentRoute())
	}
}

// The other screens reach a valid destination through their existing
// action, and no screen shows a key bar or movement hints.
func TestOtherScreensWayOutWithoutKeyBar(t *testing.T) {
	home := wayOutSized(t, tui.New(t.TempDir()))
	if screen := wayOutScreen(home); !strings.Contains(screen, ui.TextMenuQuit) {
		t.Fatalf("the menu does not end with Quit:\n%s", screen)
	} else if hint := wayOutKeyHint.FindString(screen); hint != "" {
		t.Fatalf("the menu shows the key hint %q", hint)
	}

	review := wayOutOpenInstall(t)
	if hint := wayOutKeyHint.FindString(wayOutScreen(review)); hint != "" {
		t.Fatalf("the install review shows the key hint %q", hint)
	}
	if left := wayOutPress(t, review, wayOutCtrlB); left.CurrentRoute() != tui.RouteMenu {
		t.Fatalf("Ctrl+B on the clean review left the route at %q", left.CurrentRoute())
	}

	_, cmd := wayOutStep(t, home, wayOutQ)
	if !wayOutQuits(cmd) {
		t.Fatal("Quit on the idle menu does not exit")
	}
	if _, cmd := wayOutStep(t, review, wayOutCtrlC); !wayOutQuits(cmd) {
		t.Fatal("Ctrl+C on an idle clean flow does not exit")
	}
}
