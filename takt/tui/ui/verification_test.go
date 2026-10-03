package ui_test

import (
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// Each capability shows once by its user-facing name; only checks that did
// not pass add their state and explanation (PR-UX-27).
func TestVerificationRendersEachCheck(t *testing.T) {
	report := verify.Report{
		Checks: []verify.CheckResult{
			{ID: "mcp:opencode:engram", State: verify.Verified, Explanation: "connected"},
			{ID: "mcp:opencode:codegraph", State: verify.NotVerified, Explanation: "no evidence"},
			{ID: "skills:opencode:takt", State: verify.NotVerifiable, Explanation: "cannot query"},
		},
		FinalNote: "final note text",
	}

	got := ansi.Strip(ui.Verification(report))

	for _, want := range []string{"✓ Memory", "✗ Code navigation" + ui.TextStateNotWorking, "no evidence", "? Skills" + ui.TextStateNotChecked, "cannot query", "final note text"} {
		if !strings.Contains(got, want) {
			t.Errorf("Verification() = %q, want it to contain %q", got, want)
		}
	}
	for _, unwanted := range []string{"mcp:opencode", "skills:opencode", "connected"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("Verification() = %q, must not show %q", got, unwanted)
		}
	}
}

func TestVerificationWithNoChecks(t *testing.T) {
	report := verify.Report{FinalNote: "nothing to show"}
	got := ui.Verification(report)
	if !strings.Contains(got, "nothing to show") {
		t.Errorf("Verification() with no checks = %q, want it to still contain the final note", got)
	}
}

func TestVerificationSeparatesFailedFromUnconfirmed(t *testing.T) {
	unconfirmed := ui.Verification(verify.Report{Checks: []verify.CheckResult{
		{ID: "mcp:engram", State: verify.Verified, Explanation: "connected"},
		{ID: "skills", State: verify.NotVerifiable, Explanation: "cannot query"},
	}})
	if !strings.Contains(unconfirmed, ui.TextUnconfirmed) || strings.Contains(unconfirmed, ui.TextNotReady) {
		t.Errorf("a check that could not run = %q, want unconfirmed and not a failure", unconfirmed)
	}
	failed := ui.Verification(verify.Report{Checks: []verify.CheckResult{
		{ID: "mcp:engram", State: verify.NotVerified, Explanation: "absent"},
		{ID: "skills", State: verify.NotVerifiable, Explanation: "cannot query"},
	}})
	if !strings.Contains(failed, ui.TextNotReady) || strings.Count(failed, ui.TextNotReady) != 1 {
		t.Errorf("a failed check = %q, want the not-ready verdict exactly once", failed)
	}
}
