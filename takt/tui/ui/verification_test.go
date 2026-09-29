package ui_test

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/tui/ui"
	"github.com/rou-cru/takt-ai/takt/verify"
)

func TestVerificationRendersEachCheck(t *testing.T) {
	report := verify.Report{
		Checks: []verify.CheckResult{
			{ID: "mcp:engram", State: verify.Verified, Explanation: "connected"},
			{ID: "mcp:missing", State: verify.NotVerified, Explanation: "no evidence"},
			{ID: "mcp:broken", State: verify.NotVerifiable, Explanation: "cannot query"},
		},
		FinalNote: "final note text",
	}

	got := ui.Verification(report)

	for _, want := range []string{"mcp:engram", "connected", "mcp:missing", "no evidence", "mcp:broken", "cannot query", "final note text"} {
		if !strings.Contains(got, want) {
			t.Errorf("Verification() = %q, want it to contain %q", got, want)
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
