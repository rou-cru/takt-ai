package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/common"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	syncflow "github.com/rou-cru/takt-ai/takt/tui/sync"
)

func TestMenuRoutesAndQuits(t *testing.T) {
	tests := []struct {
		name  string
		keys  []tea.KeyType
		route Route
		quit  bool
	}{
		{name: "install", keys: []tea.KeyType{tea.KeyEnter}, route: RouteInstall},
		{name: "sync", keys: []tea.KeyType{tea.KeyDown, tea.KeyEnter}, route: RouteSync},
		{name: "uninstall", keys: []tea.KeyType{tea.KeyDown, tea.KeyDown, tea.KeyEnter}, route: RouteUninstall},
		{name: "quit", keys: []tea.KeyType{tea.KeyRunes}, route: RouteMenu, quit: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := New(t.TempDir())
			var command tea.Cmd
			for _, key := range tt.keys {
				message := tea.KeyMsg{Type: key}
				if key == tea.KeyRunes {
					message.Runes = []rune("q")
				}
				app, command = update(t, app, message)
			}
			if app.CurrentRoute() != tt.route {
				t.Fatalf("route = %q, want %q", app.CurrentRoute(), tt.route)
			}
			if tt.quit {
				if _, ok := command().(tea.QuitMsg); !ok {
					t.Fatalf("quit command = %T, want tea.QuitMsg", command())
				}
			}
		})
	}
}

func TestActionResultReturnsToTheActiveFlowBeforeBack(t *testing.T) {
	app := New(t.TempDir())
	app, _ = update(t, app, tea.KeyMsg{Type: tea.KeyDown})
	app, _ = update(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	app, _ = update(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	app, requestCommand := update(t, app, common.AcceptConfirmation{})

	request := actionRequest(t, requestCommand)
	app, actionCommand := update(t, app, request)
	if actionCommand == nil || !app.runtime.Busy {
		t.Fatal("action request must be deferred through runtime")
	}

	app, _ = update(t, app, runtime.ActionResultMsg{
		Request: request,
		Result:  runtime.ActionResult{Action: runtime.ActionSync, Changed: []string{".codex/AGENTS.md"}},
	})
	if app.runtime.Busy {
		t.Fatal("runtime remains busy after action result")
	}
	if got := app.View(); !strings.Contains(got, "Sync complete") {
		t.Fatalf("view = %q, want active flow result", got)
	}

	app, _ = update(t, app, syncflow.BackMsg{})
	if app.CurrentRoute() != RouteMenu {
		t.Fatalf("route after back = %q, want menu", app.CurrentRoute())
	}
}

func TestActionRequestDoesNotTouchFilesystemDuringUpdate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "missing")
	app := New(root)
	app, _ = update(t, app, tea.KeyMsg{Type: tea.KeyDown})
	app, _ = update(t, app, tea.KeyMsg{Type: tea.KeyEnter})
	_, command := update(t, app, runtime.ActionRequest{
		Action:  runtime.ActionSync,
		RootDir: root,
		Targets: []model.AgentID{model.AgentCodex},
	})
	if command == nil {
		t.Fatal("action request command = nil")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("root exists after Update: %v", err)
	}
}

func update(t *testing.T, app Model, message tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, command := app.Update(message)
	return next.(Model), command
}

// actionRequest extracts the emitted ActionRequest from a command, following
// tea.Batch wrappers the flow attaches alongside the busy spinner tick.
func actionRequest(t *testing.T, cmd tea.Cmd) runtime.ActionRequest {
	t.Helper()
	if cmd == nil {
		t.Fatal("nil command")
	}
	if request, ok := cmd().(runtime.ActionRequest); ok {
		return request
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, sub := range batch {
			if request, ok := sub().(runtime.ActionRequest); ok {
				return request
			}
		}
	}
	t.Fatalf("command did not emit runtime.ActionRequest")
	return runtime.ActionRequest{}
}
