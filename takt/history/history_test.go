// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func open(t *testing.T, state string) *History {
	t.Helper()
	h, err := Open(state)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
	})
	return h
}

// observed builds an entry attributed to the kind's own semantic author.
func observed(t *testing.T, unit string, kind Kind, outcome Outcome) Entry {
	t.Helper()
	return Entry{Author: AuthorOf(kind), Kind: kind, SessionID: "root", WorkUnitID: unit,
		AttemptID: FirstAttempt, Cause: CauseUncaptured, Outcome: outcome}
}

func TestProjectionLifecycleAndReplay(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	h := open(t, state)
	entries := []Entry{
		{Author: AuthorOrchestrator, Kind: KindPlanned, SessionID: "root", WorkUnitID: "a", AttemptID: FirstAttempt, Cause: CauseNone},
		{Author: AuthorOrchestrator, Kind: KindPlanned, SessionID: "root", WorkUnitID: "b", AttemptID: FirstAttempt, Cause: CauseNone},
		observed(t, "a", KindAdmitted, ""),
		observed(t, "a", KindLaunched, ""),
		observed(t, "a", KindCancelRequested, ""),
		observed(t, "b", KindWithdrawn, ""),
		observed(t, "c", KindAdmitted, ""),
		{Author: AuthorHarness, Kind: KindDenied, SessionID: "root", WorkUnitID: "d", AttemptID: FirstAttempt, Cause: BoundContests},
		observed(t, "e", KindAdmitted, ""),
		observed(t, "e", KindUncertain, ""),
	}
	for _, e := range entries {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	p := h.Project()
	// Cancellation pending still holds its slot; a denial holds none.
	if p.InFlight() != 3 {
		t.Fatalf("in flight %d; want 3", p.InFlight())
	}
	want := map[string]Unit{
		"a": {SessionID: "root", AttemptID: FirstAttempt, State: StateInFlight, Flight: FlightCancelling, Launched: true, Committed: true},
		"b": {SessionID: "root", AttemptID: FirstAttempt, State: StateWithdrawn, Committed: true},
		"c": {SessionID: "root", AttemptID: FirstAttempt, State: StateInFlight, Flight: FlightPendingLaunch},
		"e": {SessionID: "root", AttemptID: FirstAttempt, State: StateInFlight, Flight: FlightUncertain},
	}
	if !reflect.DeepEqual(p.Units, want) {
		t.Fatalf("projection %+v; want %+v", p.Units, want)
	}
	// A pending admission that terminates never appears executed.
	if err := h.Append(observed(t, "c", KindTerminated, OutcomeInterrupted)); err != nil {
		t.Fatal(err)
	}
	if u := h.Project().Units["c"]; u.State != StateSettled || u.Outcome != OutcomeInterrupted || u.Launched {
		t.Fatalf("settled without launch: %+v", u)
	}
	// A later entry never rewrites a settled unit.
	if err := h.Append(observed(t, "c", KindLaunched, "")); err != nil {
		t.Fatal(err)
	}
	if u := h.Project().Units["c"]; u.State != StateSettled || u.Launched {
		t.Fatalf("settled unit rewritten: %+v", u)
	}
	// A second consumer replaying the same prefix derives the same projection.
	reopened := open(t, state)
	if !reflect.DeepEqual(reopened.Project(), h.Project()) {
		t.Fatal("reopened store derived a different projection")
	}
	if n := len(reopened.Entries()); n != len(entries)+2 {
		t.Fatalf("entries %d; want %d", n, len(entries)+2)
	}
}

// TestBudgetsDerivedFromReplay pins the accounting the enforcement reads: only
// a demonstrated recovery breaks an objective's streak, unplanned admission is
// counted once per unit, plan coverage is not, and duplicates collapse.
func TestBudgetsDerivedFromReplay(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	contest := func(unit, attempt string) Entry {
		e := observed(t, unit, KindContested, "")
		e.AttemptID = attempt
		return e
	}
	recovery := func(kind Kind, objective string, outcome Outcome) Entry {
		e := observed(t, "scope", kind, outcome)
		e.Objective = objective
		return e
	}
	for _, e := range []Entry{
		{Author: AuthorOrchestrator, Kind: KindPlanned, SessionID: "root", WorkUnitID: "covered", AttemptID: FirstAttempt, Cause: CauseNone, Contract: "c"},
		observed(t, "covered", KindAdmitted, ""),
		observed(t, "loose", KindAdmitted, ""),
		observed(t, "loose", KindAdmitted, ""),
		contest("loose", FirstAttempt),
		contest("loose", FirstAttempt),
		contest("loose", "2"),
		recovery(KindRecoveryDeclared, "goal", ""),
		recovery(KindRecoveryClosed, "goal", OutcomeFailed),
		recovery(KindRecoveryDeclared, "goal", ""),
		recovery(KindRecoveryClosed, "goal", OutcomeFailed),
		recovery(KindRecoveryDeclared, "other", ""),
		recovery(KindRecoveryClosed, "other", OutcomeFailed),
		recovery(KindRecoveryDeclared, "other", ""),
		recovery(KindRecoveryClosed, "other", OutcomeCompleted),
		{Author: AuthorHarness, Kind: KindException, SessionID: "root", WorkUnitID: "loose", AttemptID: FirstAttempt,
			Cause: CauseUncaptured, Bound: BoundRecovery, Objective: "goal", Allowance: 1},
	} {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	want := Budgets{
		Unplanned: 1,
		Contests:  map[string]bool{"loose/1": true, "loose/2": true},
		Recoveries: map[string]Recovery{
			"goal":  {Failures: 2, Unit: "scope", Attempted: map[string]bool{}, Cursor: -1},
			"other": {Unit: "scope", Attempted: map[string]bool{}, Cursor: -1},
		},
		Allowance: map[string]int{AllowanceKey(BoundRecovery, "goal"): 1},
	}
	if got := h.Project().Budgets("root"); !reflect.DeepEqual(got, want) {
		t.Fatalf("budgets %+v; want %+v", got, want)
	}
	// Another session's budgets are its own.
	if got := h.Project().Budgets("elsewhere"); !reflect.DeepEqual(got, Budgets{}) {
		t.Fatalf("budgets leaked across sessions: %+v", got)
	}
}

// TestInterlocutorBudgets pins the interlocutor holder accounting a switch,
// a denial and a handoff produce.
func TestInterlocutorBudgets(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	switched := observed(t, "child", KindInterlocutorSwitched, "")
	switched.Agent, switched.Artifact = "specialist", "artifacts/report.md"
	denied := observed(t, "child", KindDenied, "")
	denied.Cause = CauseUncaptured
	for _, e := range []Entry{switched, denied} {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	got := h.Project().Budgets("root")
	if got.InterlocutorHolder != "child" || got.InterlocutorAgent != "specialist" || got.InterlocutorArtifact != "artifacts/report.md" {
		t.Fatalf("holder state %+v", got)
	}
	handoff := observed(t, "child", KindInterlocutorHandoff, "")
	handoff.Result = "Handoff"
	if err := h.Append(handoff); err != nil {
		t.Fatal(err)
	}
	got = h.Project().Budgets("root")
	if got.InterlocutorHolder != "" || got.InterlocutorAgent != "" || got.InterlocutorArtifact != "" {
		t.Fatalf("holder not cleared after handoff: %+v", got)
	}
}

func TestStoreIsAppendOnly(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	h := open(t, state)
	if err := h.Append(observed(t, "a", KindAdmitted, "")); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(state, historyFile))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	for _, statement := range []string{"UPDATE history SET data=x'00' WHERE seq=0", "DELETE FROM history WHERE seq=0"} {
		if _, err := db.Exec(statement); err == nil || !strings.Contains(err.Error(), "append-only execution history") {
			t.Fatalf("%s: %v", statement, err)
		}
	}
}

func TestAppendRejectsInvalidEntries(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	valid := observed(t, "a", KindAdmitted, "")
	cases := map[string]func(Entry) Entry{
		"author":    func(e Entry) Entry { e.Author = "auditor"; return e },
		"kind":      func(e Entry) Entry { e.Kind = "consolidated"; return e },
		"node kind": func(e Entry) Entry { e.NodeKind = "guess"; return e },
		"identity":  func(e Entry) Entry { e.WorkUnitID = ""; return e },
		"cause":     func(e Entry) Entry { e.Cause = ""; return e },
		"outcome":   func(e Entry) Entry { e.Outcome = OutcomeCompleted; return e },
		"terminal":  func(e Entry) Entry { e.Kind = KindTerminated; return e },
		"objective": func(e Entry) Entry {
			e.Kind, e.Author = KindRecoveryDeclared, AuthorOrchestrator
			return e
		},
		"unknown bound": func(e Entry) Entry {
			e.Kind, e.Bound, e.Allowance = KindException, "budget/invented", 1
			return e
		},
		"open-ended allowance": func(e Entry) Entry {
			e.Kind, e.Bound = KindException, BoundContests
			return e
		},
	}
	for name, mutate := range cases {
		if err := h.Append(mutate(valid)); err == nil {
			t.Fatalf("%s: invalid entry recorded", name)
		}
	}
	if n := len(h.Entries()); n != 0 {
		t.Fatalf("rejected entries recorded: %d", n)
	}
}
