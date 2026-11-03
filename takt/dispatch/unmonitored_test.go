package dispatch_test

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
)

func TestUnmonitoredExecutionKeepsItsReservationUntilReconciled(t *testing.T) {
	h := newHistory(t)
	policy := loadPolicy(t)
	policy.Concurrency.Specialists = 1
	req := dispatch.AdmissionRequest{Event: "unmon-a", Session: "unmon-root", Agent: "executor", Dispatch: "unmon-call-a"}
	if err := dispatch.Admit(h, policy, "", req); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Launch(h, "", "unmon-a", "unmon-root"); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Record(h, "", "unmon-a", "unmon-root", history.KindUnmonitored); err != nil {
		t.Fatal(err)
	}
	u, _ := h.Project().Unit("unmon-root", "unmon-a")
	if u.State != history.StateInFlight || u.Flight != history.FlightUnmonitored {
		t.Fatalf("unit = %+v; want unmonitored work in flight", u)
	}
	// A lost stream is not a release: the slot stays held and a second unit is refused.
	other := dispatch.AdmissionRequest{Event: "unmon-b", Session: "unmon-root", Agent: "executor", Dispatch: "unmon-call-b"}
	if err := dispatch.Admit(h, policy, "", other); err == nil {
		t.Fatal("unmonitored execution released its reservation")
	}
	// Execution found running again is reconciled like an uncertain launch.
	if err := dispatch.Reconcile(h, "", "unmon-a", "unmon-root", true); err != nil {
		t.Fatalf("reconcile running: %v", err)
	}
	if u, _ = h.Project().Unit("unmon-root", "unmon-a"); u.Flight != history.FlightRunning {
		t.Fatalf("flight = %q; want observed running after reconciliation", u.Flight)
	}
	if err := dispatch.Reconcile(h, "", "unmon-a", "unmon-root", true); err == nil || !strings.Contains(err.Error(), "nothing uncertain") {
		t.Fatalf("reconcile of running work = %v; want nothing to reconcile", err)
	}
}

func TestLaunchRefusesAnUnmonitoredExecutionUntilReconciled(t *testing.T) {
	h := newHistory(t)
	req := dispatch.AdmissionRequest{Event: "unmon-c", Session: "unmon-root-c", Agent: "executor", Dispatch: "unmon-call-c"}
	if err := dispatch.Admit(h, loadPolicy(t), "", req); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Record(h, "", "unmon-c", "unmon-root-c", history.KindUnmonitored); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Launch(h, "", "unmon-c", "unmon-root-c"); err == nil || !strings.Contains(err.Error(), "reconciled") {
		t.Fatalf("launch over unmonitored work = %v; want it to name reconciliation", err)
	}
	if err := dispatch.Reconcile(h, "", "unmon-c", "unmon-root-c", false); err != nil {
		t.Fatal(err)
	}
	if u, _ := h.Project().Unit("unmon-root-c", "unmon-c"); u.State != history.StateSettled {
		t.Fatalf("unit = %+v; want non-start to settle it and release the slot", u)
	}
}

func TestMarkActivityUnmonitoredKeepsTheActivityInFlight(t *testing.T) {
	h := newHistory(t)
	const session, id = "unmon-activity-root", "orchestrator-activity-5e0a"
	orchestrator := history.NodeKindOrchestrator
	if err := dispatch.MarkActivityUnmonitored(h, session, id); err == nil {
		t.Fatal("an activity that never started was marked unmonitored")
	}
	if err := dispatch.StartActivity(h, session, id, orchestrator); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := dispatch.MarkActivityUnmonitored(h, session, id); err != nil {
			t.Fatal(err)
		}
	}
	if a := h.Project().Activities[id]; a.State != history.StateInFlight || a.Flight != history.FlightUnmonitored || a.Outcome != "" {
		t.Fatalf("activity = %+v; want unmonitored work still in flight", a)
	}
	if n := len(h.Entries()); n != 2 {
		t.Fatalf("entries = %d; want the start and one loss", n)
	}
	if err := dispatch.FinishActivity(h, session, id, orchestrator, history.OutcomeCompleted); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.MarkActivityUnmonitored(h, session, id); err != nil {
		t.Fatalf("a late loss = %v; want a no-op", err)
	}
	if a := h.Project().Activities[id]; a.State != history.StateSettled || a.Flight != "" || len(h.Entries()) != 3 {
		t.Fatalf("activity = %+v after a late loss", a)
	}
}

func TestFinishRecordsCompletedDeliveryWithoutEvidenceAsUnverified(t *testing.T) {
	h := newHistory(t)
	policy := loadPolicy(t)
	for _, unit := range []string{"fin-plain", "fin-verified", "fin-cancelled"} {
		req := dispatch.AdmissionRequest{Event: unit, Session: "fin-root", Agent: "executor", Dispatch: "call-" + unit}
		if err := dispatch.Admit(h, policy, "", req); err != nil {
			t.Fatal(err)
		}
	}
	revision := uint64(1)
	verified := history.Entry{Author: history.AuthorVerification, Kind: history.KindResult, SessionID: "fin-root", WorkUnitID: "fin-verified",
		AttemptID: history.FirstAttempt, Cause: history.CauseNone, Status: history.ResultVerified, Revision: &revision, Evidence: "verification/fin-verified/1"}
	if err := h.Append(verified); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Record(h, "", "fin-cancelled", "fin-root", history.KindCancelRequested); err != nil {
		t.Fatal(err)
	}
	for _, unit := range []string{"fin-plain", "fin-verified", "fin-cancelled"} {
		if err := dispatch.Finish(h, "", unit, "fin-root"); err != nil {
			t.Fatal(err)
		}
	}
	p := h.Project()
	plain, _ := p.Unit("fin-root", "fin-plain")
	if plain.Outcome != history.OutcomeCompleted || plain.Assurance == nil || plain.Assurance.Status != history.ResultUnverified || plain.Assurance.Revision != nil {
		t.Fatalf("plain = %+v / %+v; want completed delivery with unverified assurance", plain, plain.Assurance)
	}
	if got, _ := p.Unit("fin-root", "fin-verified"); got.Assurance == nil || got.Assurance.Status != history.ResultVerified {
		t.Fatalf("a finish replaced the evaluated result: %+v", got.Assurance)
	}
	if got, _ := p.Unit("fin-root", "fin-cancelled"); got.Outcome != history.OutcomeInterrupted || got.Assurance != nil {
		t.Fatalf("cancelled = %+v; want interrupted with no assurance claimed", got)
	}
	for _, e := range h.Entries() {
		if e.Kind == history.KindResult && e.WorkUnitID == "fin-plain" && e.Author != history.AuthorHarness {
			t.Fatalf("finish authored its result as %q", e.Author)
		}
	}
}
