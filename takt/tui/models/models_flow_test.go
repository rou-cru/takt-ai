package models_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
	"github.com/rou-cru/takt-ai/takt/tui/models"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

var (
	enterKey = tea.KeyPressMsg{Code: tea.KeyEnter}
	tabKey   = tea.KeyPressMsg{Code: tea.KeyTab}
	downKey  = tea.KeyPressMsg{Code: tea.KeyDown}
)

// fakeOpenCodeModelsScript answers `opencode api GET /api/model` with two
// real model choices, so a reassignment has something new to pick.
const fakeOpenCodeModelsScript = `#!/bin/sh
case "$*" in
  'api GET /api/model') printf '%s' '{"location":{},"data":[{"providerID":"opencode","modelID":"big-pickle","limit":{"context":1000,"output":100}},{"providerID":"provider","modelID":"other-model","limit":{"context":2000,"output":200}}]}' ;;
  *) echo "unexpected OpenCode invocation: $*" >&2; exit 1 ;;
esac
`

func withFakeOpenCodeModels(t *testing.T) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(fakeOpenCodeModelsScript), 0o755); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func viewText(m models.Model) string {
	sized, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	view, _ := sized.(models.Model)
	return ansi.Strip(view.View().Content)
}

func TestModelsPasteFiltersPickerWhileOpen(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}

	withFakeOpenCodeModels(t)
	m := models.New(root)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned a nil command for an installed root")
	}
	next, _ := m.Update(cmd())
	m = next.(models.Model)

	// Open the model list for the first specialist, then paste a filter.
	next, _ = m.Update(enterKey)
	m = next.(models.Model)
	next, _ = m.Update(tea.PasteMsg{Content: "pickle"})
	m = next.(models.Model)
	if got := viewText(m); !strings.Contains(got, "big-pickle") {
		t.Fatalf("picker View() after paste = %q, want it to still list the matching model", got)
	}
}

func TestModelsReassignFlow(t *testing.T) {
	root := t.TempDir()
	request := setuputil.TestPlanRequest()
	if _, err := (lifecycle.Runtime{}).Run(context.Background(), "install", root, request); err != nil {
		t.Fatalf("Run(install) error = %v", err)
	}

	withFakeOpenCodeModels(t)
	m := models.New(root)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("Init() returned a nil command for an installed root")
	}
	next, _ := m.Update(cmd())
	m = next.(models.Model)

	if m.Dirty() {
		t.Fatal("Dirty() before any selection = true, want false")
	}

	// Open the model list for the first specialist (cursor 0, focus body).
	next, _ = m.Update(enterKey)
	m = next.(models.Model)
	if got := viewText(m); !strings.Contains(got, "big-pickle") {
		t.Fatalf("model list View() = %q, want it to list the discovered models", got)
	}

	// Move to the second model choice (index 1: the inherit label occupies
	// index 0), then confirm it.
	next, _ = m.Update(downKey)
	m = next.(models.Model)
	next, _ = m.Update(enterKey)
	m = next.(models.Model)

	if !m.Dirty() {
		t.Fatal("Dirty() after choosing a real model = false, want true")
	}

	// Move focus to the footer's Apply action and confirm.
	next, _ = m.Update(tabKey)
	m = next.(models.Model)
	restoring, cmd := m.Update(enterKey)
	m = restoring.(models.Model)
	if cmd == nil {
		t.Fatal("confirming the reassignment produced no command")
	}
	if !m.Run().Busy() {
		t.Fatal("model is not busy after starting the reassignment action")
	}

	requestMsg := cmd()
	actionRequest, ok := requestMsg.(runtime.ActionRequest)
	if !ok {
		t.Fatalf("reassignment command message = %#v, want runtime.ActionRequest", requestMsg)
	}
	if actionRequest.Action != runtime.ActionReassignModels || len(actionRequest.ReassignOverrides) == 0 {
		t.Fatalf("request = %+v, want ActionReassignModels with at least one override", actionRequest)
	}

	result, err := (runtime.Adapter{}).Execute(actionRequest)
	if err != nil {
		t.Fatalf("Adapter.Execute() error = %v", err)
	}
	resultMsg := runtime.ActionResultMsg{Request: actionRequest, Result: result, Err: err}
	next, _ = m.Update(resultMsg)
	m = next.(models.Model)

	if m.Run().Busy() {
		t.Error("model is still busy after the result was consumed")
	}
	if got := viewText(m); got == "" {
		t.Error("result View() is empty")
	}
}

func TestModelsUnavailableWithoutInstall(t *testing.T) {
	m := models.New(t.TempDir())
	if got := viewText(m); !strings.Contains(got, "Could not load the installed configuration") {
		t.Errorf("View() on an uninstalled root = %q, want the load-failure message", got)
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_ = next.(models.Model)
	if cmd == nil {
		t.Fatal("Esc on the unavailable screen produced no command")
	}
	if _, ok := cmd().(ui.BackMsg); !ok {
		t.Fatal("Esc on the unavailable screen did not request BackMsg")
	}
}
