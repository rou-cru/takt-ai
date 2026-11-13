// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package diagnostics_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/diagnostics"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// installedModel returns a diagnostics screen over a root that holds an installation, so it starts collecting.
func installedModel(t *testing.T) diagnostics.Model {
	t.Helper()
	root := t.TempDir()
	if err := setup.SaveInstalledConfig(root, setup.PlanRequest{}); err != nil {
		t.Fatal(err)
	}
	return diagnostics.New(root)
}

func renderDiagnostics(model diagnostics.Model) string {
	updated, _ := model.Update(tea.WindowSizeMsg{Width: 400, Height: 60})
	return ansi.Strip(updated.(diagnostics.Model).View().Content)
}

func requireBackOnEscape(t *testing.T, model diagnostics.Model) {
	t.Helper()
	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if command == nil {
		t.Fatal("Escape returned no navigation command")
	}
	if _, ok := command().(ui.BackMsg); !ok {
		t.Fatal("Escape did not request BackMsg")
	}
}

func TestNotInstalledDiagnosticsSkipsCollectionAndReturnsToMenu(t *testing.T) {
	model := diagnostics.New(t.TempDir())
	if model.Init() != nil {
		t.Fatal("Init() should skip collection when nothing is installed")
	}
	if got := model.Title(); got != ui.TextDiagCheckingTitle {
		t.Fatalf("Title() = %q, want %q", got, ui.TextDiagCheckingTitle)
	}
	if got := renderDiagnostics(model); !strings.Contains(got, setup.NotInstalledMessage) {
		t.Fatalf("View() = %q, want not-installed message", got)
	}
	requireBackOnEscape(t, model)
}

func TestDiagnosticsCollectingAndReportViews(t *testing.T) {
	model := installedModel(t)
	if model.Init() == nil {
		t.Fatal("Init() should start collection while installed-state checks are pending")
	}
	if got := renderDiagnostics(model); !strings.Contains(got, ui.TextDiagBusy) {
		t.Fatalf("collecting View() = %q, want pending status", got)
	}

	empty := verify.Report{FinalNote: "No capabilities were reported."}
	updated, command := model.Update(empty)
	model = updated.(diagnostics.Model)
	if command != nil {
		t.Fatalf("Update(report) returned a command %v", command)
	}
	if got := model.Title(); got != ui.TextDiagReportTitle {
		t.Fatalf("Title() = %q, want %q", got, ui.TextDiagReportTitle)
	}
	if got := renderDiagnostics(model); !strings.Contains(got, ui.TextDiagNone) || !strings.Contains(got, empty.FinalNote) {
		t.Fatalf("empty report View() = %q, want empty-report warning and final note", got)
	}

	populated := verify.Report{
		Checks: []verify.CheckResult{
			{ID: "mcp:opencode:takt_memory", State: verify.NotVerified, Explanation: "absent from installed configuration"},
			{ID: "memory:service", State: verify.NotVerified, Explanation: "memory service is not running"},
		},
	}
	updated, _ = model.Update(populated)
	model = updated.(diagnostics.Model)
	got := renderDiagnostics(model)
	for _, want := range []string{"Memory", "Memory service", "absent from installed configuration", "memory service is not running"} {
		if !strings.Contains(got, want) {
			t.Fatalf("populated report View() = %q, want %q", got, want)
		}
	}
	if strings.Contains(got, "mcp:opencode") || strings.Contains(got, "memory:service") {
		t.Fatalf("populated report View() = %q, must name capabilities, never check IDs", got)
	}
	requireBackOnEscape(t, model)
}

func TestDiagnosticsKeepsStateOnResizeAndUnrelatedMessages(t *testing.T) {
	model := installedModel(t)
	updated, command := model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if command != nil {
		t.Fatalf("resize returned a command %v", command)
	}
	model = updated.(diagnostics.Model)
	updated, command = model.Update(tea.KeyPressMsg{Code: 'x', Text: "x"})
	if command != nil || updated.(diagnostics.Model).Title() != ui.TextDiagCheckingTitle {
		t.Fatal("unrelated input changed state or emitted a command")
	}
	if got := renderDiagnostics(updated.(diagnostics.Model)); !strings.Contains(got, ui.TextDiagBusy) {
		t.Fatalf("View() = %q, want the screen still collecting", got)
	}
}

// A failed capability offers Repair first; it asks to open the configure flow.
func TestFailedReportOffersRepair(t *testing.T) {
	failed := verify.Report{Checks: []verify.CheckResult{{ID: "mcp:opencode:takt_memory", State: verify.NotVerified, Explanation: "absent"}}}
	updated, _ := installedModel(t).Update(failed)
	model := updated.(diagnostics.Model)
	if got := renderDiagnostics(model); !strings.Contains(got, ui.TextActionRepair) {
		t.Fatalf("failed report must offer Repair:\n%s", got)
	}
	_, command := model.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if command == nil {
		t.Fatal("Repair returned no command")
	}
	if _, ok := command().(diagnostics.OpenRepairMsg); !ok {
		t.Fatal("Repair must ask to open the configure flow")
	}
	passed, _ := installedModel(t).Update(verify.Report{Ready: true, Checks: []verify.CheckResult{{ID: "mcp:opencode:takt_memory", State: verify.Verified}}})
	if got := renderDiagnostics(passed.(diagnostics.Model)); strings.Contains(got, ui.TextActionRepair) {
		t.Fatalf("a passing report must not offer Repair:\n%s", got)
	}
}
