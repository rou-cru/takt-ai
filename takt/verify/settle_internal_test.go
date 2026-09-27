package verify

import (
	"context"
	"testing"
	"time"
)

func withShortSettle(t *testing.T) {
	t.Helper()
	previousTimeout, previousInterval := reloadSettleTimeout, reloadSettleInterval
	reloadSettleTimeout, reloadSettleInterval = 50*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { reloadSettleTimeout, reloadSettleInterval = previousTimeout, previousInterval })
}

func TestAnyNotVerified(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks []CheckResult
		want   bool
	}{
		{name: "empty", checks: nil, want: false},
		{name: "all verified", checks: []CheckResult{{"a", Verified, ""}, {"b", Verified, ""}}, want: false},
		{name: "not verifiable only", checks: []CheckResult{{"a", NotVerifiable, ""}}, want: false},
		{name: "one not verified", checks: []CheckResult{{"a", Verified, ""}, {"b", NotVerified, ""}}, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := anyNotVerified(tc.checks); got != tc.want {
				t.Fatalf("anyNotVerified(%+v) = %v, want %v", tc.checks, got, tc.want)
			}
		})
	}
}

func TestSettleAfterReloadHappyPath(t *testing.T) {
	withShortSettle(t)
	calls := 0
	produce := func() []CheckResult {
		calls++
		return []CheckResult{{"mcp:opencode:codegraph", Verified, "connected"}}
	}
	checks := settleAfterReload(context.Background(), produce)
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (should not retry once verified)", calls)
	}
	if len(checks) != 1 || checks[0].State != Verified {
		t.Fatalf("checks = %+v, want single Verified", checks)
	}
}

func TestSettleAfterReloadResolvesWithinWindow(t *testing.T) {
	withShortSettle(t)
	calls := 0
	produce := func() []CheckResult {
		calls++
		if calls < 3 {
			return []CheckResult{{"mcp:opencode:codegraph", NotVerified, "OpenCode did not report the managed MCP server as registered."}}
		}
		return []CheckResult{{"mcp:opencode:codegraph", Verified, "connected"}}
	}
	checks := settleAfterReload(context.Background(), produce)
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (stop as soon as it settles)", calls)
	}
	if len(checks) != 1 || checks[0].State != Verified {
		t.Fatalf("checks = %+v, want single Verified", checks)
	}
}

func TestSettleAfterReloadGivesUpAtTimeout(t *testing.T) {
	withShortSettle(t)
	calls := 0
	produce := func() []CheckResult {
		calls++
		return []CheckResult{{"plugins:opencode", NotVerified, "OpenCode reports a failed plugin: local: Plugin failed to load"}}
	}
	checks := settleAfterReload(context.Background(), produce)
	if calls < 2 {
		t.Fatalf("calls = %d, want at least 2 (must retry, not give up on the first sample)", calls)
	}
	if len(checks) != 1 || checks[0].State != NotVerified {
		t.Fatalf("checks = %+v, want the last observed NotVerified result", checks)
	}
}
