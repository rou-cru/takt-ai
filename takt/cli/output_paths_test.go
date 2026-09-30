package main

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// TestRenderResultTextByType verifies each result type has its own summary line and unknown types are refused.
func TestRenderResultTextByType(t *testing.T) {
	deployed := setup.DeploymentResult{Changed: []string{"a"}, Unchanged: []string{"b", "c"}, Actions: []string{"reload"}}
	var out strings.Builder
	if err := renderResultText(&out, "sync", deployed); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Sync complete: 1 changed, 2 unchanged.", "[changed] a", "[action] reload"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("deployment output = %q; missing %q", out.String(), want)
		}
	}

	out.Reset()
	withReport := deploymentResult{DeploymentResult: deployed, Verify: &verify.Report{Ready: true, FinalNote: "all good"}}
	if err := renderResultText(&out, "install", withReport); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Install complete") || !strings.Contains(out.String(), "Verification:") || !strings.Contains(out.String(), "all good") {
		t.Errorf("deploymentResult output = %q", out.String())
	}

	out.Reset()
	if err := renderResultText(&out, "uninstall", setup.UninstallResult{Removed: []string{"r"}, Preserved: []string{"p"}, Restored: []string{"s"}}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Uninstall complete: 1 removed, 1 preserved.", "[removed] r", "[preserved] p", "[restored] s"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("uninstall output = %q; missing %q", out.String(), want)
		}
	}

	if err := renderResultText(&out, "install", 42); err == nil || !strings.Contains(err.Error(), "unsupported result type int") {
		t.Fatalf("unsupported type error = %v", err)
	}
}

// TestRenderVerificationText verifies readiness is reported separately, per check, in both states.
func TestRenderVerificationText(t *testing.T) {
	var out strings.Builder
	if err := renderVerificationText(&out, nil); err != nil || out.Len() != 0 {
		t.Fatalf("nil report output = %q, %v", out.String(), err)
	}
	report := &verify.Report{
		Checks:    []verify.CheckResult{{ID: "opencode", State: "verified", Explanation: "responds"}},
		FinalNote: "note",
	}
	if err := renderVerificationText(&out, report); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Verification:", "[verified] opencode — responds", "Not ready: some capabilities are not verified.", "  note"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("not-ready output = %q; missing %q", out.String(), want)
		}
	}
	out.Reset()
	report.Ready = true
	if err := renderVerificationText(&out, report); err != nil || !strings.Contains(out.String(), "Ready: yes") {
		t.Fatalf("ready output = %q, %v", out.String(), err)
	}
}

// TestRenderPartialResultTextByType verifies partial output for each result shape.
func TestRenderPartialResultTextByType(t *testing.T) {
	cause := errWriteFailed
	var out strings.Builder
	report := deploymentResult{DeploymentResult: setup.DeploymentResult{Changed: []string{"a"}}}
	if err := renderPartialResultText(&out, "sync", report, cause); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Sync failed after applying partial work: write failed") || !strings.Contains(out.String(), "Sync partial: 1 changed") {
		t.Errorf("deploymentResult partial = %q", out.String())
	}
	out.Reset()
	if err := renderPartialResultText(&out, "uninstall", setup.UninstallResult{Removed: []string{"r"}}, cause); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Uninstall complete: 1 removed") {
		t.Errorf("uninstall partial = %q", out.String())
	}
	if err := renderPartialResultText(failingWriter{}, "sync", report, cause); err == nil {
		t.Error("write failure not reported")
	}
}

// TestRenderErrorsPropagateWriteFailures verifies a broken output stream aborts every renderer.
func TestRenderErrorsPropagateWriteFailures(t *testing.T) {
	uninstall := setup.UninstallResult{Removed: []string{"r"}, Preserved: []string{"p"}, Restored: []string{"s"}}
	for name, err := range map[string]error{
		"uninstall plan":  renderUninstallPlanText(failingWriter{}, uninstall),
		"uninstall":       renderUninstall(failingWriter{}, uninstall),
		"uninstall paths": renderUninstallPaths(failingWriter{}, uninstall),
		"deployment":      renderDeployment(failingWriter{}, "install", setup.DeploymentResult{Changed: []string{"a"}}),
		"verification":    renderVerificationText(failingWriter{}, &verify.Report{}),
		"cancelled":       renderCancelledText(failingWriter{}, "install", lifecycle.LifecycleResult{Outcome: lifecycle.OutcomeCancelledNothingApplied}),
		"cancelled part":  renderCancelledText(failingWriter{}, "install", lifecycle.LifecycleResult{Outcome: lifecycle.OutcomeCancelledPartial}),
	} {
		if err == nil {
			t.Errorf("%s: write failure was swallowed", name)
		}
	}
}

// TestCapitalize verifies only the first letter changes and empty input is returned as is.
func TestCapitalize(t *testing.T) {
	for in, want := range map[string]string{"": "", "install": "Install", "Sync": "Sync", "x": "X"} {
		if got := capitalize(in); got != want {
			t.Errorf("capitalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestBlockOnModifiedFiles verifies only user-edited preserved files block an uninstall.
func TestBlockOnModifiedFiles(t *testing.T) {
	preview := setup.UninstallResult{
		Preserved:        []string{"kept.json", "edited.json"},
		PreservedReasons: map[string]string{"kept.json": "takt-additions", "edited.json": "user-edited"},
	}
	err := blockOnModifiedFiles(preview)
	if err == nil {
		t.Fatal("blockOnModifiedFiles() error = nil, want a decision block")
	}
	if !strings.Contains(err.Error(), "1 modified file(s)") || !strings.Contains(err.Error(), "[modified] edited.json") || strings.Contains(err.Error(), "kept.json") {
		t.Fatalf("error = %q", err.Error())
	}
	preview.PreservedReasons["edited.json"] = "missing"
	if err := blockOnModifiedFiles(preview); err != nil {
		t.Fatalf("blockOnModifiedFiles(no edits) = %v, want nil", err)
	}
}

// TestBlockOnConflictsListsBlockingFiles verifies uncertain unaccepted conflicts block with per-file labels.
func TestBlockOnConflictsListsBlockingFiles(t *testing.T) {
	conflicts := []setup.ConflictEntry{
		{Path: "free.json", Impact: setup.ImpactUnrelated},
		{Path: "risky.json", Impact: setup.ImpactUncertain, Reason: "user-edited"},
	}
	preserve, err := blockOnConflicts("install", conflicts)
	if err == nil || preserve != nil {
		t.Fatalf("blockOnConflicts() = %v, %v; want block", preserve, err)
	}
	if !strings.Contains(err.Error(), "setup install: 1 file(s) require a decision") || !strings.Contains(err.Error(), "[modified] risky.json") {
		t.Fatalf("error = %q", err.Error())
	}
	preserve, err = blockOnConflicts("install", conflicts[:1])
	if err != nil || len(preserve) != 1 || preserve[0] != "free.json" {
		t.Fatalf("unrelated only = %v, %v", preserve, err)
	}
}
