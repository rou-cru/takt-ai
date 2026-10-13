package dispatch_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
)

func TestStrengtheningBudgetSurvivesReplanningAndReopen(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	}()
	p := loadPolicy(t)
	objective := "acceptance/cart"
	version := ""
	for round := 1; round <= p.Recovery.MaxAttempts+1; round++ {
		next := fmt.Sprintf("v%d", round)
		author, sibling := fmt.Sprintf("test-%d-a", round), fmt.Sprintf("test-%d-b", round)
		units := []dispatch.PlanUnit{{Unit: author, Contract: "strengthen oracle"}, {Unit: sibling, Contract: "strengthen another row"}}
		if err := dispatch.Declare(h, "", "root", next, version, units, nil, &history.QARound{Objective: objective, Units: []string{author, sibling}}); err != nil {
			t.Fatal(err)
		}
		version = next
		req := dispatch.AdmissionRequest{Session: "root", Event: author, Agent: "test", Dispatch: author}
		err := dispatch.Admit(h, p, "", req)
		if round > p.Recovery.MaxAttempts {
			if err == nil || !strings.Contains(err.Error(), history.BoundQARounds) || !strings.Contains(err.Error(), "Close/Report") {
				t.Fatalf("fourth round = %v", err)
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		// A transport duplicate is denied, and cannot consume a second round.
		_ = dispatch.Admit(h, p, "", req)
		if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Session: "root", Event: sibling, Agent: "test", Dispatch: sibling}); err != nil {
			t.Fatal(err)
		}
		for _, unit := range []string{author, sibling} {
			if err := dispatch.Finish(h, "", unit, "root"); err != nil {
				t.Fatal(err)
			}
		}
		req.Dispatch += "/retry"
		if err := dispatch.Admit(h, p, "", req); err != nil {
			t.Fatal(err)
		}
		if err := dispatch.Finish(h, "", author, "root"); err != nil {
			t.Fatal(err)
		}
		if used := h.Project().Budgets("root").StrengtheningUsed(objective); used != round {
			t.Fatalf("used = %d, want %d", used, round)
		}
		// A later non-QA plan revision leaves the admitted round's membership intact.
		later := next + "-gate"
		if err := dispatch.Declare(h, "", "root", later, version, []dispatch.PlanUnit{{Unit: next + "/gate", Contract: "judge staged delta"}, {Unit: next + "/mutation", Contract: "affected rows"}}, nil); err != nil {
			t.Fatal(err)
		}
		version = later
		for _, unit := range []string{next + "/gate", next + "/mutation"} {
			if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Session: "root", Event: unit, Agent: "verify", Dispatch: unit}); err != nil {
				t.Fatal(err)
			}
			if err := dispatch.Finish(h, "", unit, "root"); err != nil {
				t.Fatal(err)
			}
		}
		if err := h.Close(); err != nil {
			t.Fatal(err)
		}
		h, err = history.Open(state)
		if err != nil {
			t.Fatal(err)
		}
	}
	// Close is not a member, and is still admitted at exhaustion.
	if err := dispatch.Declare(h, "", "root", "close-plan", version, []dispatch.PlanUnit{{Unit: "close", Contract: "report residual risk"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Session: "root", Event: "close", Agent: "verify", Dispatch: "close"}); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Finish(h, "", "close", "root"); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Except(h, "", "", "root", history.BoundQARounds, "", 1); err == nil {
		t.Fatal("unscoped exception admitted")
	}
	for _, allowance := range []int{0, -1} {
		if err := dispatch.Except(h, "", "", "root", history.BoundQARounds, objective, allowance); err == nil {
			t.Fatal("non-finite-positive allowance accepted")
		}
	}
	if err := dispatch.Except(h, "", "", "root", history.BoundQARounds, objective, 1); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Session: "root", Event: "test-4-a", Agent: "test", Dispatch: "fourth-after-exception"}); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Finish(h, "", "test-4-a", "root"); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Declare(h, "", "root", "v5", "close-plan", []dispatch.PlanUnit{{Unit: "test-5", Contract: "fifth round"}}, nil, &history.QARound{Objective: objective, Units: []string{"test-5"}}); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Session: "root", Event: "test-5", Agent: "test", Dispatch: "fifth"}); err == nil {
		t.Fatal("finite exception allowed fifth round")
	}
	if h.Project().Budgets("root").StrengtheningUsed(objective) != 4 {
		t.Fatal("round accounting changed")
	}
	// Other roots/objectives have no inherited consumption.
	if h.Project().Budgets("another-root").StrengtheningUsed(objective) != 0 || h.Project().Budgets("root").StrengtheningUsed("another-objective") != 0 {
		t.Fatal("budget leaked scope")
	}
}

func TestQARoundDeclarationRejectsPartialEffectsAndMigration(t *testing.T) {
	h := newHistory(t)
	plan := []dispatch.PlanUnit{{Unit: "author", Contract: "test"}, {Unit: "sibling", Contract: "test"}}
	if err := dispatch.Declare(h, "", "root", "v1", "", plan, nil, &history.QARound{Objective: "acceptance", Units: []string{"author", "sibling"}}); err != nil {
		t.Fatal(err)
	}
	p := loadPolicy(t)
	if err := dispatch.Admit(h, p, "", dispatch.AdmissionRequest{Session: "root", Event: "author", Agent: "test", Dispatch: "first"}); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name  string
		round *history.QARound
		units []dispatch.PlanUnit
	}{
		{"moving a sibling", &history.QARound{Objective: "renamed", Units: []string{"sibling"}}, []dispatch.PlanUnit{{Unit: "extra", Contract: "test"}}},
		{"moving admitted member", &history.QARound{Objective: "acceptance", Units: []string{"author"}}, []dispatch.PlanUnit{{Unit: "extra", Contract: "test"}}},
		{"unknown member", &history.QARound{Objective: "acceptance", Units: []string{"unknown"}}, []dispatch.PlanUnit{{Unit: "extra", Contract: "test"}}},
		{"empty objective", &history.QARound{Units: []string{"extra"}}, []dispatch.PlanUnit{{Unit: "extra", Contract: "test"}}},
		{"duplicate member", &history.QARound{Objective: "acceptance", Units: []string{"extra", "extra"}}, []dispatch.PlanUnit{{Unit: "extra", Contract: "test"}}},
		{"invalid plan", &history.QARound{Objective: "acceptance", Units: []string{"extra"}}, []dispatch.PlanUnit{{Unit: "extra", Contract: "test", Prerequisites: []string{"extra"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := dispatch.Declare(h, "", "root", "v2", "v1", tc.units, nil, tc.round); err == nil {
				t.Fatal("invalid round admitted")
			}
			if _, ok := h.Project().Unit("root", "extra"); ok {
				t.Fatal("partial plan effect")
			}
			b := h.Project().Budgets("root")
			if b.PlanVersion != "v1" || len(b.QARounds) != 1 || !slices.Equal(b.QARounds["v1"].Units, []string{"author", "sibling"}) {
				t.Fatalf("partial round effect: %+v", b)
			}
		})
	}
	if err := dispatch.Declare(h, "", "root", "v1", "v1", []dispatch.PlanUnit{{Unit: "extra", Contract: "test"}}, nil, &history.QARound{Objective: "acceptance", Units: []string{"extra"}}); err == nil {
		t.Fatal("round version reused")
	}
}

func TestLegacyPlansDoNotInferQARounds(t *testing.T) {
	h := newHistory(t)
	if err := dispatch.Declare(h, "", "root", "old", "", []dispatch.PlanUnit{{Unit: "strengthen-1", Contract: "test"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Admit(h, loadPolicy(t), "", dispatch.AdmissionRequest{Session: "root", Event: "strengthen-1", Agent: "test", Dispatch: "old"}); err != nil {
		t.Fatal(err)
	}
	if len(h.Project().Budgets("root").QARounds) != 0 {
		t.Fatal("inferred a round from a legacy name")
	}
	// Validation of a later batch entry cannot persist an earlier plan effect.
	before := len(h.Entries())
	valid := history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "root", WorkUnitID: "partial", AttemptID: "1", Cause: history.CauseNone, PlanVersion: "partial", Contract: "test"}
	if err := h.AppendBatch([]history.Entry{valid, {}}); err == nil || len(h.Entries()) != before {
		t.Fatal("batch failed non-atomically")
	}
}
