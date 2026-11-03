package dispatch_test

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
)

const (
	seatRoot  = "seat-root"
	seatAgent = "dev"
)

// completeWith records model running unit to a completed outcome in seatRoot,
// the evidence that proves the model in this session.
func completeWith(t *testing.T, h *history.History, unit, model string) {
	t.Helper()
	call := "call-" + unit
	if err := dispatch.Admit(h, loadPolicy(t), "", dispatch.AdmissionRequest{Event: unit, Session: seatRoot, Agent: seatAgent, Dispatch: call}); err != nil {
		t.Fatalf("Admit(%s) error = %v", unit, err)
	}
	u, _ := h.Project().Unit(seatRoot, unit)
	if err := h.Append(history.Entry{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: seatRoot, WorkUnitID: unit, AttemptID: u.AttemptID, Dispatch: call, Agent: seatAgent, Model: model, Cause: history.CauseNone}); err != nil {
		t.Fatalf("append execution: %v", err)
	}
	if err := dispatch.Finish(h, "", unit, seatRoot); err != nil {
		t.Fatalf("Finish(%s) error = %v", unit, err)
	}
}

func TestSeatRestores(t *testing.T) {
	cases := []struct {
		seat dispatch.Seat
		want bool
	}{
		{dispatch.Seat{}, false},
		{dispatch.Seat{Model: "m1", Configured: "m1"}, false},
		{dispatch.Seat{Model: "m2", Configured: "m1"}, true},
		{dispatch.Seat{Substitute: true}, true},
	}
	for _, tc := range cases {
		if got := tc.seat.Restores(); got != tc.want {
			t.Errorf("%+v.Restores() = %v, want %v", tc.seat, got, tc.want)
		}
	}
}

func TestValidateSeatRefusesInadmissibleRestorations(t *testing.T) {
	h := newHistory(t)
	cases := []struct {
		name string
		seat dispatch.Seat
		want string
	}{
		{"unknown instance", dispatch.Seat{Agent: "nobody", Model: "m2", Configured: "m1", Reason: "down"}, "not a Takt crew member"},
		{"no reason", dispatch.Seat{Agent: seatAgent, Model: "m2", Configured: "m1", Reason: "  "}, "seat_reason"},
		{"no model", dispatch.Seat{Agent: seatAgent, Reason: "down", Substitute: true}, "seat_reason"},
		{"substitute first", dispatch.Seat{Agent: seatAgent, Model: "m2", Configured: "m1", Reason: "down", Substitute: true}, "reassign its model first"},
		{"unproven model", dispatch.Seat{Agent: seatAgent, Model: "m2", Configured: "m1", Reason: "down"}, dispatch.UnprovenModelCause},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := dispatch.ValidateSeat(h, seatRoot, tc.seat)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateSeat() error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

func TestValidateSeatAdmitsUnchangedSeat(t *testing.T) {
	h := newHistory(t)
	if err := dispatch.ValidateSeat(h, seatRoot, dispatch.Seat{Agent: "nobody", Model: "m1", Configured: "m1"}); err != nil {
		t.Fatalf("ValidateSeat() of a seat it does not restore error = %v, want nil", err)
	}
}

func TestValidateSeatAdmitsApprovedOrProvenModel(t *testing.T) {
	h := newHistory(t)
	seat := dispatch.Seat{Agent: seatAgent, Model: "m2", Configured: "m1", Reason: "provider outage"}

	approved := seat
	approved.UserConfirmed = true
	if err := dispatch.ValidateSeat(h, seatRoot, approved); err != nil {
		t.Fatalf("ValidateSeat() with user approval error = %v, want nil", err)
	}

	// Work by the model in another session, or still in flight, proves nothing.
	if err := dispatch.Admit(h, loadPolicy(t), "", dispatch.AdmissionRequest{Event: "open", Session: seatRoot, Agent: seatAgent, Dispatch: "call-open"}); err != nil {
		t.Fatal(err)
	}
	u, _ := h.Project().Unit(seatRoot, "open")
	if err := h.Append(history.Entry{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: seatRoot, WorkUnitID: "open", AttemptID: u.AttemptID, Dispatch: "call-open", Agent: seatAgent, Model: "m2", Cause: history.CauseNone}); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.ValidateSeat(h, "other-root", seat); err == nil || !strings.HasPrefix(err.Error(), dispatch.UnprovenModelCause) {
		t.Fatalf("ValidateSeat() in another root error = %v, want an unproven-model refusal", err)
	}
	if err := dispatch.ValidateSeat(h, seatRoot, seat); err == nil {
		t.Fatal("ValidateSeat() with only in-flight work by the model error = nil, want a refusal")
	}

	completeWith(t, h, "done", "m2")
	if err := dispatch.ValidateSeat(h, seatRoot, seat); err != nil {
		t.Fatalf("ValidateSeat() after the model completed work error = %v, want nil", err)
	}
}

func TestValidateSeatAdmitsSubstituteAfterFailedReassignment(t *testing.T) {
	h := newHistory(t)
	completeWith(t, h, "done", "m2")
	reassign := dispatch.Seat{Agent: seatAgent, Model: "m2", Configured: "m1", Reason: "provider outage"}
	if err := dispatch.Admit(h, loadPolicy(t), "", dispatch.AdmissionRequest{Event: "retry", Session: seatRoot, Agent: seatAgent, Dispatch: "call-retry"}); err != nil {
		t.Fatalf("Admit(retry) error = %v", err)
	}
	u, _ := h.Project().Unit(seatRoot, "retry")
	if err := h.Append(dispatch.SeatEntry(seatRoot, "retry", u.AttemptID, reassign)); err != nil {
		t.Fatalf("append seat restoration: %v", err)
	}
	substitute := reassign
	substitute.Substitute = true
	// A reassignment that has not failed may still work: no substitute yet.
	if err := dispatch.ValidateSeat(h, seatRoot, substitute); err == nil {
		t.Fatal("ValidateSeat() of a substitute while the reassignment runs error = nil, want a refusal")
	}
	if err := dispatch.Record(h, "", "retry", seatRoot, history.KindCancelRequested); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.Finish(h, "", "retry", seatRoot); err != nil {
		t.Fatal(err)
	}
	if err := dispatch.ValidateSeat(h, seatRoot, substitute); err != nil {
		t.Fatalf("ValidateSeat() of a substitute after the reassignment failed error = %v, want nil", err)
	}
	// A recorded substitute is not a reassignment: it never unlocks another one.
	other := newHistory(t)
	completeWith(t, other, "done", "m2")
	if err := other.Append(dispatch.SeatEntry(seatRoot, "done", history.FirstAttempt, substitute)); err != nil {
		t.Fatalf("append substitute restoration: %v", err)
	}
	if err := dispatch.ValidateSeat(other, seatRoot, substitute); err == nil {
		t.Fatal("ValidateSeat() after only a substitute restoration error = nil, want a refusal")
	}
}

func TestSeatEntryRecordsTheRestoration(t *testing.T) {
	seat := dispatch.Seat{Agent: seatAgent, Model: "m2", Configured: "m1", Reason: "provider outage", Substitute: true}
	e := dispatch.SeatEntry(seatRoot, "u1", "a1", seat)
	if e.Kind != history.KindSeatRestored || e.Author != history.AuthorOrchestrator || e.SessionID != seatRoot || e.WorkUnitID != "u1" || e.AttemptID != "a1" ||
		e.Agent != seatAgent || e.Model != "m2" || e.Reason != "provider outage" || !e.Substitute {
		t.Fatalf("SeatEntry() = %+v, want the declared restoration", e)
	}
	h := newHistory(t)
	missing := dispatch.SeatEntry(seatRoot, "u1", "a1", dispatch.Seat{Agent: seatAgent, Model: "m2"})
	if err := h.Append(missing); err == nil {
		t.Fatal("Append() of a restoration without a reason error = nil, want a refusal")
	}
}

func TestSubstituteIdentityReturnsResolvedPersonaAndSoul(t *testing.T) {
	texts, err := dispatch.SubstituteIdentity("pm")
	if err != nil {
		t.Fatalf("SubstituteIdentity(pm) error = %v", err)
	}
	if len(texts) != 2 {
		t.Fatalf("SubstituteIdentity(pm) = %d texts, want PERSONA and SOUL", len(texts))
	}
	for _, text := range texts {
		if text == "" || strings.Contains(text, "{{") {
			t.Errorf("SubstituteIdentity() text is empty or unresolved: %.80q", text)
		}
	}
	// An instance whose definition declares neither leaves nothing out.
	if texts, err := dispatch.SubstituteIdentity(seatAgent); err != nil || len(texts) != 0 {
		t.Fatalf("SubstituteIdentity(%s) = %v, %v; want no texts", seatAgent, texts, err)
	}
	if _, err := dispatch.SubstituteIdentity("nobody"); err == nil {
		t.Fatal("SubstituteIdentity(unknown) error = nil, want an error")
	}
}
