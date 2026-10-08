// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history_test

import (
	"database/sql"
	"github.com/rou-cru/takt-ai/takt/history"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The on-disk contract the store promises: its file name and the owner-only
// modes that keep the execution history unreadable to other users.
const (
	storeFile                   = "history.sqlite"
	privateDirMode  os.FileMode = 0o700
	privateFileMode os.FileMode = 0o600
)

func open(t *testing.T, state string) *history.History {
	t.Helper()
	h, err := history.Open(state)
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
func observed(t *testing.T, unit string, kind history.Kind, outcome history.Outcome) history.Entry {
	t.Helper()
	e := history.Entry{Author: history.AuthorOf(kind), Kind: kind, SessionID: "root", WorkUnitID: unit,
		AttemptID: history.FirstAttempt, Cause: history.CauseUncaptured, Outcome: outcome}
	if kind == history.KindAdmitted {
		e.NodeKind = history.NodeKindDelegated
	}
	return e
}

func TestProjectionLifecycleAndReplay(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	h := open(t, state)
	entries := []history.Entry{
		{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "root", WorkUnitID: "a", NodeKind: history.NodeKindDelegated, AttemptID: history.FirstAttempt, Cause: history.CauseNone},
		{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "root", WorkUnitID: "b", NodeKind: history.NodeKindDelegated, AttemptID: history.FirstAttempt, Cause: history.CauseNone},
		observed(t, "a", history.KindAdmitted, ""),
		observed(t, "a", history.KindLaunched, ""),
		observed(t, "a", history.KindCancelRequested, ""),
		observed(t, "b", history.KindWithdrawn, ""),
		observed(t, "c", history.KindAdmitted, ""),
		{Author: history.AuthorHarness, Kind: history.KindDenied, SessionID: "root", WorkUnitID: "d", AttemptID: history.FirstAttempt, Cause: history.BoundContests},
		observed(t, "e", history.KindAdmitted, ""),
		observed(t, "e", history.KindUncertain, ""),
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
	want := map[string]history.Unit{
		"a": {SessionID: "root", NodeKind: history.NodeKindDelegated, AttemptID: history.FirstAttempt, State: history.StateInFlight, Flight: history.FlightCancelling, Launched: true, Committed: true},
		"b": {SessionID: "root", NodeKind: history.NodeKindDelegated, AttemptID: history.FirstAttempt, State: history.StateWithdrawn, Committed: true},
		"c": {SessionID: "root", NodeKind: history.NodeKindDelegated, AttemptID: history.FirstAttempt, State: history.StateInFlight, Flight: history.FlightPendingLaunch},
		"e": {SessionID: "root", NodeKind: history.NodeKindDelegated, AttemptID: history.FirstAttempt, State: history.StateInFlight, Flight: history.FlightUncertain},
	}
	if !reflect.DeepEqual(p.Units, want) {
		t.Fatalf("projection %+v; want %+v", p.Units, want)
	}
	// A pending admission that terminates never appears executed.
	if err := h.Append(observed(t, "c", history.KindTerminated, history.OutcomeInterrupted)); err != nil {
		t.Fatal(err)
	}
	if u := h.Project().Units["c"]; u.State != history.StateSettled || u.Outcome != history.OutcomeInterrupted || u.Launched {
		t.Fatalf("settled without launch: %+v", u)
	}
	// A later entry never rewrites a settled unit.
	if err := h.Append(observed(t, "c", history.KindLaunched, "")); err != nil {
		t.Fatal(err)
	}
	if u := h.Project().Units["c"]; u.State != history.StateSettled || u.Launched {
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
	contest := func(unit, attempt string) history.Entry {
		e := observed(t, unit, history.KindContested, "")
		e.AttemptID = attempt
		return e
	}
	recovery := func(kind history.Kind, objective string, outcome history.Outcome) history.Entry {
		e := observed(t, "scope", kind, outcome)
		e.Objective = objective
		return e
	}
	for _, e := range []history.Entry{
		{Author: history.AuthorOrchestrator, Kind: history.KindPlanned, SessionID: "root", WorkUnitID: "covered", AttemptID: history.FirstAttempt, Cause: history.CauseNone, Contract: "c"},
		observed(t, "covered", history.KindAdmitted, ""),
		observed(t, "loose", history.KindAdmitted, ""),
		observed(t, "loose", history.KindAdmitted, ""),
		contest("loose", history.FirstAttempt),
		contest("loose", history.FirstAttempt),
		contest("loose", "2"),
		recovery(history.KindRecoveryDeclared, "goal", ""),
		recovery(history.KindRecoveryClosed, "goal", history.OutcomeFailed),
		recovery(history.KindRecoveryDeclared, "goal", ""),
		recovery(history.KindRecoveryClosed, "goal", history.OutcomeFailed),
		recovery(history.KindRecoveryDeclared, "other", ""),
		recovery(history.KindRecoveryClosed, "other", history.OutcomeFailed),
		recovery(history.KindRecoveryDeclared, "other", ""),
		recovery(history.KindRecoveryClosed, "other", history.OutcomeCompleted),
		{Author: history.AuthorHarness, Kind: history.KindException, SessionID: "root", WorkUnitID: "loose", AttemptID: history.FirstAttempt,
			Cause: history.CauseUncaptured, Bound: history.BoundRecovery, Objective: "goal", Allowance: 1},
	} {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	want := history.Budgets{
		Unplanned: 1,
		Contests:  map[string]bool{"loose/1": true, "loose/2": true},
		Recoveries: map[string]history.Recovery{
			"goal":  {Failures: 2, Unit: "scope", Attempted: map[string]bool{}, Cursor: -1},
			"other": {Unit: "scope", Attempted: map[string]bool{}, Cursor: -1},
		},
		Allowance: map[string]int{history.AllowanceKey(history.BoundRecovery, "goal"): 1},
	}
	if got := h.Project().Budgets("root"); !reflect.DeepEqual(got, want) {
		t.Fatalf("budgets %+v; want %+v", got, want)
	}
	// Another session's budgets are its own.
	if got := h.Project().Budgets("elsewhere"); !reflect.DeepEqual(got, history.Budgets{}) {
		t.Fatalf("budgets leaked across sessions: %+v", got)
	}
}

// TestInterlocutorBudgets pins the interlocutor holder accounting a switch,
// a denial and a handoff produce.
func TestInterlocutorBudgets(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	switched := observed(t, "child", history.KindInterlocutorSwitched, "")
	switched.Agent, switched.Artifact = "specialist", "artifacts/report.md"
	denied := observed(t, "child", history.KindDenied, "")
	denied.Cause = history.CauseUncaptured
	for _, e := range []history.Entry{switched, denied} {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	got := h.Project().Budgets("root")
	if got.InterlocutorHolder != "child" || got.InterlocutorAgent != "specialist" || got.InterlocutorArtifact != "artifacts/report.md" {
		t.Fatalf("holder state %+v", got)
	}
	handoff := observed(t, "child", history.KindInterlocutorHandoff, "")
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
	if err := h.Append(observed(t, "a", history.KindAdmitted, "")); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(state, storeFile))
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
	valid := observed(t, "a", history.KindAdmitted, "")
	cases := map[string]func(history.Entry) history.Entry{
		"author":    func(e history.Entry) history.Entry { e.Author = "auditor"; return e },
		"kind":      func(e history.Entry) history.Entry { e.Kind = "consolidated"; return e },
		"node kind": func(e history.Entry) history.Entry { e.NodeKind = "guess"; return e },
		"identity":  func(e history.Entry) history.Entry { e.WorkUnitID = ""; return e },
		"cause":     func(e history.Entry) history.Entry { e.Cause = ""; return e },
		"outcome":   func(e history.Entry) history.Entry { e.Outcome = history.OutcomeCompleted; return e },
		"terminal":  func(e history.Entry) history.Entry { e.Kind = history.KindTerminated; return e },
		"objective": func(e history.Entry) history.Entry {
			e.Kind, e.Author = history.KindRecoveryDeclared, history.AuthorOrchestrator
			return e
		},
		"unknown bound": func(e history.Entry) history.Entry {
			e.Kind, e.Bound, e.Allowance = history.KindException, "budget/invented", 1
			return e
		},
		"open-ended allowance": func(e history.Entry) history.Entry {
			e.Kind, e.Bound = history.KindException, history.BoundContests
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
