package gc_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/gc"
)

var (
	invPlan   = gc.Plan{Request: gc.Request{SessionID: "s", CycleID: "c1", Mandate: gc.MandateDeadCode}}
	invReport = gc.Report{Findings: []gc.Finding{
		{ID: "dead-code:a.go#Used"},
		{ID: "dead-code:a.go#Gone"},
	}}
	invAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
)

func investigation(id, outcome string) gc.Investigation {
	return gc.Investigation{FindingID: id, Outcome: outcome, Evidence: "traced the call site", Instance: "verifier-1", At: invAt}
}

func TestInvestigateRecordsAgainstPlanCycle(t *testing.T) {
	r := investigation("dead-code:a.go#Used", "confirmed")
	r.CycleID, r.SessionID = "other", "other"
	got, err := gc.Investigate(invPlan, invReport, nil, r)
	if err != nil || len(got) != 1 {
		t.Fatalf("Investigate() = %v, %v", got, err)
	}
	if got[0].CycleID != "c1" || got[0].SessionID != "s" {
		t.Errorf("Investigate() stamped record = %+v, want cycle/session from the plan", got[0])
	}
}

func TestInvestigateRejectsWithoutEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*gc.Investigation){
		"no evidence text": func(r *gc.Investigation) { r.Evidence = "   " },
		"no instance":      func(r *gc.Investigation) { r.Instance = "" },
		"no timestamp":     func(r *gc.Investigation) { r.At = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			r := investigation("dead-code:a.go#Used", "confirmed")
			mutate(&r)
			if _, err := gc.Investigate(invPlan, invReport, nil, r); !errors.Is(err, gc.ErrNoEvidence) {
				t.Errorf("Investigate() error = %v, want ErrNoEvidence", err)
			}
		})
	}
}

func TestInvestigateRejectsInvalidOutcome(t *testing.T) {
	r := investigation("dead-code:a.go#Used", "maybe")
	if _, err := gc.Investigate(invPlan, invReport, nil, r); err == nil {
		t.Fatal("Investigate() with an invalid outcome error = nil, want an error")
	}
}

func TestInvestigateRejectsUnknownFinding(t *testing.T) {
	r := investigation("dead-code:a.go#Typo", "confirmed")
	if _, err := gc.Investigate(invPlan, invReport, nil, r); !errors.Is(err, gc.ErrUnknownFinding) {
		t.Fatalf("Investigate() error = %v, want ErrUnknownFinding", err)
	}
}

func TestInvestigateDuplicateIsIdempotent(t *testing.T) {
	first, err := gc.Investigate(invPlan, invReport, nil, investigation("dead-code:a.go#Used", "confirmed"))
	if err != nil {
		t.Fatalf("Investigate() error = %v", err)
	}
	second, err := gc.Investigate(invPlan, invReport, first, investigation("dead-code:a.go#Used", "confirmed"))
	if err != nil || len(second) != 1 || second[0] != first[0] {
		t.Fatalf("Investigate() duplicate = %+v, %v, want the same single record", second, err)
	}
}

func TestInvestigateConflictingEvidenceErrors(t *testing.T) {
	first, err := gc.Investigate(invPlan, invReport, nil, investigation("dead-code:a.go#Used", "confirmed"))
	if err != nil {
		t.Fatalf("Investigate() error = %v", err)
	}
	conflicting := investigation("dead-code:a.go#Used", "refuted")
	if _, err := gc.Investigate(invPlan, invReport, first, conflicting); err == nil {
		t.Fatal("Investigate() with conflicting evidence for the same finding error = nil, want an error")
	}
}

func TestAuthorizedFinding(t *testing.T) {
	base := gc.Finding{ID: "f1", Tool: "deadcode", ToolVersion: "1.0", Rule: "unreachable", Evidence: "e"}
	confirmed := []gc.Investigation{{FindingID: "f1", Outcome: "confirmed", Evidence: "traced"}}

	if !gc.AuthorizedFinding(base, confirmed) {
		t.Error("AuthorizedFinding() with a confirmed investigation and evidence = false, want true")
	}
	if gc.AuthorizedFinding(base, nil) {
		t.Error("AuthorizedFinding() with no investigations = true, want false")
	}

	proposalOnly := base
	proposalOnly.ProposalOnly = true
	if gc.AuthorizedFinding(proposalOnly, confirmed) {
		t.Error("AuthorizedFinding() on a proposal-only finding = true, want false")
	}

	introduced := base
	introduced.Introduced = true
	if gc.AuthorizedFinding(introduced, confirmed) {
		t.Error("AuthorizedFinding() on an introduced finding = true, want false")
	}

	exported := base
	exported.Exported = true
	if gc.AuthorizedFinding(exported, confirmed) {
		t.Error("AuthorizedFinding() on an exported finding = true, want false")
	}

	integrityMandate := base
	integrityMandate.Mandate = gc.MandateAnalyzerIntegrity
	if gc.AuthorizedFinding(integrityMandate, confirmed) {
		t.Error("AuthorizedFinding() on an analyzer-integrity finding = true, want false")
	}

	pending := base
	pending.Status = gc.StatusPending
	if gc.AuthorizedFinding(pending, confirmed) {
		t.Error("AuthorizedFinding() on a pending finding = true, want false")
	}

	missingMetadata := gc.Finding{ID: "f1"}
	if gc.AuthorizedFinding(missingMetadata, confirmed) {
		t.Error("AuthorizedFinding() on a finding missing tool/rule/evidence metadata = true, want false")
	}
}
