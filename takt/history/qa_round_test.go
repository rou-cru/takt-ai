package history_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

// qaRoundEntry declares round membership for a plan version.
func qaRoundEntry(version string, round *history.QARound) history.Entry {
	return history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindQARound, SessionID: "root", WorkUnitID: "root",
		AttemptID: history.FirstAttempt, Cause: history.CauseNone, PlanVersion: version, QARound: round}
}

func TestQARoundDeclarationShape(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	stray := observed(t, "a", history.KindAdmitted, "")
	stray.QARound = &history.QARound{Objective: "acceptance", Units: []string{"a"}}
	for name, e := range map[string]history.Entry{
		"round on another kind": stray,
		"no plan version":       qaRoundEntry("", &history.QARound{Objective: "acceptance", Units: []string{"a"}}),
		"no round":              qaRoundEntry("v1", nil),
		"blank objective":       qaRoundEntry("v1", &history.QARound{Objective: " ", Units: []string{"a"}}),
		"no units":              qaRoundEntry("v1", &history.QARound{Objective: "acceptance"}),
		"blank unit":            qaRoundEntry("v1", &history.QARound{Objective: "acceptance", Units: []string{" "}}),
		"duplicate unit":        qaRoundEntry("v1", &history.QARound{Objective: "acceptance", Units: []string{"a", "a"}}),
	} {
		if err := h.Append(e); err == nil {
			t.Errorf("%s: appended", name)
		}
	}
	if len(h.Entries()) != 0 {
		t.Fatalf("invalid declarations recorded: %d entries", len(h.Entries()))
	}
}

func TestQARoundStartsOnFirstMemberAdmission(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	for _, e := range []history.Entry{
		qaRoundEntry("v1", &history.QARound{Objective: "acceptance", Units: []string{"author", "sibling"}}),
		qaRoundEntry("v2", &history.QARound{Objective: "acceptance", Units: []string{"later"}}),
		qaRoundEntry("v3", &history.QARound{Objective: "other", Units: []string{"elsewhere"}}),
	} {
		if err := h.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	budgets := h.Project().Budgets("root")
	if version, round := budgets.QARoundFor("sibling"); version != "v1" || round.Started || !slices.Equal(round.Units, []string{"author", "sibling"}) {
		t.Fatalf("QARoundFor(sibling) = %q %+v; want v1 not started", version, round)
	}
	if version, _ := budgets.QARoundFor("unlisted"); version != "" {
		t.Fatalf("QARoundFor(unlisted) = %q; want no round", version)
	}
	if used := budgets.StrengtheningUsed("acceptance"); used != 0 {
		t.Fatalf("declared rounds consumed %d before any admission", used)
	}

	// The first member admission starts its round; siblings and repeats do not count again.
	for _, unit := range []string{"author", "sibling", "author", "elsewhere"} {
		if err := h.Append(observed(t, unit, history.KindAdmitted, "")); err != nil {
			t.Fatal(err)
		}
	}
	budgets = h.Project().Budgets("root")
	if used := budgets.StrengtheningUsed("acceptance"); used != 1 {
		t.Fatalf("StrengtheningUsed(acceptance) = %d; want 1", used)
	}
	if used := budgets.StrengtheningUsed("other"); used != 1 {
		t.Fatalf("StrengtheningUsed(other) = %d; want 1", used)
	}
	if _, round := budgets.QARoundFor("later"); round.Started {
		t.Fatal("an unadmitted round started")
	}
}
