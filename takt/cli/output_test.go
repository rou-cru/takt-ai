// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main

import (
	"errors"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/verify"
)

func TestRenderPlans(t *testing.T) {
	t.Run("install without conflicts", func(t *testing.T) {
		var output strings.Builder
		plan := lifecycle.InstallPreview{Plans: []setup.TargetPlan{{ManagedPaths: []string{"a", "b"}}}}
		if err := renderPlanText(&output, "install", plan); err != nil {
			t.Fatal(err)
		}
		if got := output.String(); !strings.Contains(got, "Plan for install") || !strings.Contains(got, "2 managed") {
			t.Fatalf("renderPlanText() = %q", got)
		}
	})

	t.Run("sync with conflicts", func(t *testing.T) {
		var output strings.Builder
		plan := lifecycle.InstallPreview{Conflicts: []setup.ConflictEntry{{Path: "AGENTS.md", Reason: "user-edited"}}}
		if err := renderPlanText(&output, "sync", plan); err != nil {
			t.Fatal(err)
		}
		if got := output.String(); !strings.Contains(got, "AGENTS.md") || !strings.Contains(got, "decision") {
			t.Fatalf("renderPlanText() = %q", got)
		}
	})

	t.Run("uninstall", func(t *testing.T) {
		var output strings.Builder
		plan := setup.UninstallResult{
			Removed: []string{"plugin.ts"}, Preserved: []string{"config.json"}, Restored: []string{"prior.json"},
			PreservedReasons: map[string]string{"config.json": "user-edited"},
		}
		if err := renderPlanText(&output, "uninstall", plan); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"[remove] plugin.ts", "[preserve] config.json", "[restore] prior.json"} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("renderPlanText() = %q; missing %q", output.String(), want)
			}
		}
	})

	if err := renderPlanText(&strings.Builder{}, "install", "unsupported"); err == nil {
		t.Fatal("renderPlanText(unsupported) returned no error")
	}
}

func TestRenderCancelledTextReportsNothingAndPartialChanges(t *testing.T) {
	t.Run("nothing applied", func(t *testing.T) {
		var output strings.Builder
		result := lifecycle.LifecycleResult{Outcome: lifecycle.OutcomeCancelledNothingApplied}
		if err := renderCancelledText(&output, "sync", result); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(output.String(), "Sync cancelled before any change was applied") || !strings.Contains(output.String(), "Nothing was changed") {
			t.Fatalf("renderCancelledText() = %q", output.String())
		}
	})

	t.Run("some applied with backups", func(t *testing.T) {
		var output strings.Builder
		result := lifecycle.LifecycleResult{
			Outcome: lifecycle.OutcomeCancelledPartial, Changed: []string{"plugin.ts"}, Removed: []string{"old.ts"},
			Restored: []string{"prior.ts"}, NotApplied: []string{"new.ts"}, BackupDir: "/backups/run-1",
		}
		if err := renderCancelledText(&output, "install", result); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"Install cancelled after applying 3 of 4 changes", "[applied] plugin.ts", "[applied] old.ts", "[applied] prior.ts", "Backup copies", "/backups/run-1", "setup install again"} {
			if !strings.Contains(output.String(), want) {
				t.Errorf("renderCancelledText() = %q; missing %q", output.String(), want)
			}
		}
	})
}

func TestRenderPlanPropagatesWriterErrors(t *testing.T) {
	if err := renderInstallPlanText(failingWriter{}, "install", lifecycle.InstallPreview{}, runtime.InstallSummary{}); !errors.Is(err, errWriteFailed) {
		t.Fatalf("renderInstallPlanText() error = %v, want %v", err, errWriteFailed)
	}
}

func TestRenderResultVariantsAndVerification(t *testing.T) {
	deployment := setup.DeploymentResult{Changed: []string{"plugin.ts"}, Unchanged: []string{"config.json"}, Actions: []string{"reload"}}
	var output strings.Builder
	if err := renderResultText(&output, "install", deployment); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Install complete: 1 changed, 1 unchanged", "[changed] plugin.ts", "[action] reload"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("renderResultText() = %q; missing %q", output.String(), want)
		}
	}

	output.Reset()
	result := deploymentResult{DeploymentResult: deployment, Verify: &verify.Report{
		Checks: []verify.CheckResult{{ID: "opencode", State: verify.NotVerified, Explanation: "no model"}},
		Ready:  false, FinalNote: "Install completed; readiness needs attention.",
	}}
	if err := renderResultText(&output, "install", result); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Verification:", "not verified", "no model", "Not ready", result.Verify.FinalNote} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("verification result = %q; missing %q", output.String(), want)
		}
	}

	output.Reset()
	if err := renderPartialResultText(&output, "sync", deployment, errors.New("provider failed")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Sync failed after applying partial work") || !strings.Contains(output.String(), "Sync partial") {
		t.Fatalf("partial result = %q", output.String())
	}

	if err := renderResultText(&strings.Builder{}, "install", "unsupported"); err == nil {
		t.Fatal("renderResultText(unsupported) returned no error")
	}
	if err := renderPartialResultText(&strings.Builder{}, "sync", "unsupported", errors.New("failure")); err == nil {
		t.Fatal("renderPartialResultText(unsupported) returned no error")
	}

	output.Reset()
	if err := renderResultText(&output, "uninstall", setup.UninstallResult{Removed: []string{"plugin.ts"}, Restored: []string{"prior.json"}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Uninstall complete") || !strings.Contains(output.String(), "[restored] prior.json") {
		t.Fatalf("uninstall result = %q", output.String())
	}
}

func TestRenderReadyVerificationAndCancellationWithoutBackups(t *testing.T) {
	var output strings.Builder
	if err := renderVerificationText(&output, &verify.Report{Ready: true, FinalNote: "All good."}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Ready: yes") || !strings.Contains(output.String(), "All good.") {
		t.Fatalf("ready verification = %q", output.String())
	}

	output.Reset()
	result := lifecycle.LifecycleResult{
		Outcome: lifecycle.OutcomeCancelledPartial, Changed: []string{"plugin.ts"}, NotApplied: []string{"new.ts"},
	}
	if err := renderCancelledText(&output, "install", result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Automatic rollback is not available") || strings.Contains(output.String(), "Backup copies") {
		t.Fatalf("cancelled result without backup = %q", output.String())
	}
}

var errWriteFailed = errors.New("write failed")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWriteFailed }

// The plan states what the install sets up, in the TUI review's terms.
func TestRenderInstallPlanStatesTheSummary(t *testing.T) {
	var output strings.Builder
	summary := runtime.InstallSummary{Agents: make([]runtime.AgentModel, 13), Skills: 27, MCPServers: []string{"codegraph", "engram"}}
	if err := renderInstallPlanText(&output, "install", lifecycle.InstallPreview{}, summary); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Agents        13", "Skills        27", "MCP servers   codegraph, engram"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("plan output lacks %q:\n%s", want, output.String())
		}
	}
	if strings.Contains(output.String(), "Integrations") {
		t.Errorf("an empty group was printed:\n%s", output.String())
	}
}
