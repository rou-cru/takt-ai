// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package diagnostics

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/keys"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/verify"
)

func TestNotInstalledDiagnosticsSkipsCollectionAndReturnsToMenu(t *testing.T) {
	model := New(t.TempDir())
	if model.state != StateNotInstalled {
		t.Fatalf("state = %v, want StateNotInstalled", model.state)
	}
	if model.Init() != nil {
		t.Fatal("Init() should skip collection when nothing is installed")
	}
	if got := model.Title(); got != ui.TextDiagCheckingTitle {
		t.Fatalf("Title() = %q, want %q", got, ui.TextDiagCheckingTitle)
	}
	if got := renderDiagnostics(model); !strings.Contains(got, setup.NotInstalledMessage) {
		t.Fatalf("View() = %q, want not-installed message", got)
	}

	updated, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command == nil {
		t.Fatal("Escape returned no navigation command")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Escape did not request BackMsg")
	}
	if updated.(Model).state != StateNotInstalled {
		t.Fatal("navigation changed the diagnostics state")
	}
}

func TestDiagnosticsCollectingAndReportViews(t *testing.T) {
	model := Model{root: t.TempDir(), state: StateCollecting, keymap: keys.Default()}
	if model.Init() == nil {
		t.Fatal("Init() should start collection while installed-state checks are pending")
	}
	if got := renderDiagnostics(model); !strings.Contains(got, ui.TextDiagBusy) {
		t.Fatalf("collecting View() = %q, want pending status", got)
	}

	empty := verify.Report{FinalNote: "No capabilities were reported."}
	updated, command := model.Update(empty)
	model = updated.(Model)
	if command != nil || model.state != StateReport {
		t.Fatalf("Update(report) = state %v, command %v; want report and no command", model.state, command)
	}
	if got := model.Title(); got != ui.TextDiagReportTitle {
		t.Fatalf("Title() = %q, want %q", got, ui.TextDiagReportTitle)
	}
	if got := renderDiagnostics(model); !strings.Contains(got, ui.TextDiagNone) || !strings.Contains(got, empty.FinalNote) {
		t.Fatalf("empty report View() = %q, want empty-report warning and final note", got)
	}

	populated := verify.Report{
		Checks:    []verify.CheckResult{{ID: "opencode", State: verify.Verified, Explanation: "responded"}},
		FinalNote: "Availability checked.",
	}
	updated, _ = model.Update(populated)
	model = updated.(Model)
	if got := renderDiagnostics(model); !strings.Contains(got, "opencode") || !strings.Contains(got, "responded") {
		t.Fatalf("populated report View() = %q, want the check and explanation", got)
	}

	_, command = model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command == nil {
		t.Fatal("Escape from report returned no navigation command")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Escape from report did not request BackMsg")
	}
}

func TestDiagnosticsForwardsResizeAndIgnoresUnrelatedMessages(t *testing.T) {
	model := Model{state: StateCollecting}
	updated, command := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model = updated.(Model)
	if command != nil || model.width != 100 || model.height != 30 {
		t.Fatalf("resize = (%d, %d), command %v; want (100, 30), nil", model.width, model.height, command)
	}
	updated, command = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if command != nil || updated.(Model).state != StateCollecting {
		t.Fatal("unrelated input changed state or emitted a command")
	}
}

func renderDiagnostics(model Model) string {
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 400, Height: 60})
	return ansi.Strip(updated.(Model).View().Content)
}
