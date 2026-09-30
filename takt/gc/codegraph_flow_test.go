package gc

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

const (
	// listingOneSymbol is codegraph's symbols-only listing with a single symbol.
	listingOneSymbol = "- `Work` (function)\n"
	// queryHit is a `query -j` answer placing Work in a.go.
	queryHit = `[{"node":{"qualifiedName":"pkg.Work","filePath":"a.go"}}]`
)

// stubRunner answers each codegraph subcommand from answers, failing the test
// on a subcommand nobody stubbed.
func stubRunner(t *testing.T, answers map[string]func() ([]byte, error)) Runner {
	t.Helper()
	return func(_ context.Context, args ...string) ([]byte, error) {
		answer, ok := answers[args[0]]
		if !ok {
			t.Fatalf("unexpected codegraph subcommand %q", args[0])
		}
		return answer()
	}
}

func fixed(out string) func() ([]byte, error) {
	return func() ([]byte, error) { return []byte(out), nil }
}

func TestExecRunnerPassesWorkspaceTheWayEachSubcommandExpects(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, "fake-codegraph", "#!/bin/sh\necho \"$@\"\n", fixtureScriptMode)
	run := ExecRunner(root+"/fake-codegraph", "/ws")

	for _, positional := range []string{"init", "index", "sync", "status"} {
		out, err := run(context.Background(), positional)
		if err != nil || strings.TrimSpace(string(out)) != positional+" /ws" {
			t.Errorf("run(%s) = %q, %v; want the workspace as a positional path", positional, out, err)
		}
	}
	out, err := run(context.Background(), "query", "-j", "Work")
	if err != nil || strings.TrimSpace(string(out)) != "query -j Work -p /ws" {
		t.Errorf("run(query) = %q, %v; want the workspace as -p", out, err)
	}

	putFile(t, root, "failing-codegraph", "#!/bin/sh\nexit 2\n", fixtureScriptMode)
	if _, err := ExecRunner(root+"/failing-codegraph", "/ws")(context.Background(), "impact"); err == nil || !strings.Contains(err.Error(), "codegraph impact") {
		t.Errorf("run(failing) error = %v, want the subcommand named", err)
	}
}

func TestDependentsFailureModes(t *testing.T) {
	boom := errors.New("codegraph down")
	fail := func() ([]byte, error) { return nil, boom }
	cases := map[string]map[string]func() ([]byte, error){
		"listing fails":       {"node": fail},
		"query fails":         {"node": fixed(listingOneSymbol), "query": fail},
		"query is not JSON":   {"node": fixed(listingOneSymbol), "query": fixed("garbage")},
		"impact fails":        {"node": fixed(listingOneSymbol), "query": fixed(queryHit), "impact": fail},
		"impact JSON corrupt": {"node": fixed(listingOneSymbol), "query": fixed(queryHit), "impact": fixed("{oops")},
	}
	for name, answers := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := NewCodegraph(stubRunner(t, answers)).Dependents(context.Background(), "a.go"); err == nil {
				t.Fatal("Dependents() error = nil, want the failure surfaced")
			}
		})
	}
}

func TestDependentsSkipsNoticesAndDeduplicates(t *testing.T) {
	answers := map[string]func() ([]byte, error){
		// The same symbol listed twice is resolved once.
		"node":  fixed(listingOneSymbol + listingOneSymbol + "- `Other` (function)\n"),
		"query": fixed(queryHit),
	}
	impacts := 0
	answers["impact"] = func() ([]byte, error) {
		impacts++
		if impacts == 1 {
			return []byte(`{"affected":[{"filePath":"b.go"},{"filePath":"c.go"},{"filePath":"b.go"}]}`), nil
		}
		return []byte("symbol not found"), nil // a notice, exit 0: no results
	}
	got, err := NewCodegraph(stubRunner(t, answers)).Dependents(context.Background(), "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"b.go", "c.go"}) {
		t.Errorf("Dependents() = %v, want [b.go c.go] deduplicated and sorted", got)
	}
	if impacts != 2 {
		t.Errorf("impact ran %d times, want once per distinct symbol", impacts)
	}
}

func TestInvestigateIsIdempotentAndRefusesConflicts(t *testing.T) {
	plan := Plan{Request: Request{SessionID: "s", CycleID: "c"}}
	report := Report{Findings: []Finding{{ID: "f1"}}}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	confirmed := Investigation{FindingID: "f1", Instance: "i", Outcome: "confirmed", Evidence: "traced callers", At: at}

	first, err := Investigate(plan, report, nil, confirmed)
	if err != nil || len(first) != 1 || first[0].CycleID != "c" || first[0].SessionID != "s" {
		t.Fatalf("Investigate() = %+v, %v; want one record stamped with the cycle identity", first, err)
	}
	again, err := Investigate(plan, report, first, confirmed)
	if err != nil || len(again) != 1 {
		t.Errorf("repeating the same investigation = %+v, %v; want it absorbed", again, err)
	}
	contradicting := confirmed
	contradicting.Outcome = "refuted"
	if _, err := Investigate(plan, report, first, contradicting); err == nil || !strings.Contains(err.Error(), "conflicting investigation") {
		t.Errorf("contradicting investigation error = %v, want a conflict", err)
	}
	other := Investigation{FindingID: "f2", Instance: "i", Outcome: "confirmed", Evidence: "e", At: at}
	if _, err := Investigate(plan, report, first, other); !errors.Is(err, ErrUnknownFinding) {
		t.Errorf("unknown finding error = %v, want ErrUnknownFinding", err)
	}
}

func TestAnalyzePreparedRejectsInvalidTimeoutBeforeRunningAnything(t *testing.T) {
	p := Preparation{Config: ProjectConfig{AnalyzerTimeout: "forever"}}
	if _, err := AnalyzePrepared(context.Background(), t.TempDir(), Plan{}, p); err == nil {
		t.Fatal("AnalyzePrepared() error = nil, want the timeout rejection")
	}
}
