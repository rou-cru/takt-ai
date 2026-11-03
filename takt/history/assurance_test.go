// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

// resultAt builds a result for the unit's current attempt at an evaluated
// revision; evidence names it so a test can tell which result prevailed.
func resultAt(t *testing.T, unit string, status history.ResultStatus, revision uint64, evidence string) history.Entry {
	t.Helper()
	e := observed(t, unit, history.KindResult, "")
	e.Status, e.Revision, e.Evidence = status, &revision, evidence
	return e
}

func settledUnit(t *testing.T, unit string) []history.Entry {
	t.Helper()
	return []history.Entry{
		observed(t, unit, history.KindAdmitted, ""),
		observed(t, unit, history.KindLaunched, ""),
		observed(t, unit, history.KindTerminated, history.OutcomeCompleted),
	}
}

func TestResultForTheCurrentRevisionPrevailsOverALateEarlierOne(t *testing.T) {
	entries := append(settledUnit(t, "res-late"),
		resultAt(t, "res-late", history.ResultFailed, 1, "journal/1"),
		resultAt(t, "res-late", history.ResultVerified, 2, "journal/2"),
		// Evidence for revision 1 arriving after revision 2's stays historical.
		resultAt(t, "res-late", history.ResultFailed, 1, "journal/3"),
	)
	u, _ := history.Project(entries).Unit("root", "res-late")
	if u.Assurance == nil || u.Assurance.Status != history.ResultVerified || *u.Assurance.Revision != 2 || u.Assurance.Evidence != "journal/2" {
		t.Fatalf("assurance = %+v; want the verified result of revision 2", u.Assurance)
	}
	if u.State != history.StateSettled || u.Outcome != history.OutcomeCompleted {
		t.Fatalf("a result changed the lifecycle: %+v", u)
	}
	later := append(entries, resultAt(t, "res-late", history.ResultFailed, 2, "journal/4"))
	if u, _ = history.Project(later).Unit("root", "res-late"); u.Assurance.Status != history.ResultFailed || u.Assurance.Evidence != "journal/4" {
		t.Fatalf("assurance = %+v; want the later result of the same revision to prevail", u.Assurance)
	}
}

func TestUnevaluatedResultNeverReplacesAnEvaluatedOne(t *testing.T) {
	unverified := observed(t, "res-bare", history.KindResult, "")
	unverified.Author, unverified.Status = history.AuthorHarness, history.ResultUnverified
	entries := append(settledUnit(t, "res-bare"), unverified, resultAt(t, "res-bare", history.ResultVerified, 3, "journal/9"), unverified)
	u, _ := history.Project(entries).Unit("root", "res-bare")
	if u.Assurance.Status != history.ResultVerified || *u.Assurance.Revision != 3 {
		t.Fatalf("assurance = %+v; want the evaluated result to stand", u.Assurance)
	}
}

func TestAResultBelongsToItsAttempt(t *testing.T) {
	retried := observed(t, "res-retry", history.KindAdmitted, "")
	retried.AttemptID = "2"
	stale := resultAt(t, "res-retry", history.ResultVerified, 5, "journal/5")
	entries := append(settledUnit(t, "res-retry"), resultAt(t, "res-retry", history.ResultVerified, 1, "journal/1"), retried, stale)
	u, _ := history.Project(entries).Unit("root", "res-retry")
	if u.AttemptID != "2" || u.Assurance != nil {
		t.Fatalf("unit = %+v; want attempt 2 with no assurance carried over or taken from the earlier attempt", u)
	}
}

func TestResultValidation(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	if err := h.Append(observed(t, "res-valid", history.KindAdmitted, "")); err != nil {
		t.Fatal(err)
	}
	missing := observed(t, "res-valid", history.KindResult, "")
	other := observed(t, "res-valid", history.KindLaunched, "")
	other.Status = history.ResultVerified
	forged := observed(t, "res-valid", history.KindResult, "")
	forged.Author, forged.Status = history.AuthorOrchestrator, history.ResultVerified
	for name, e := range map[string]history.Entry{"no status": missing, "unknown status": func() history.Entry { e := missing; e.Status = "passed"; return e }(), "status on another kind": other, "orchestrator author": forged} {
		if err := h.Append(e); err == nil {
			t.Errorf("%s was recorded", name)
		}
	}
	harness := observed(t, "res-valid", history.KindResult, "")
	harness.Author, harness.Status = history.AuthorHarness, history.ResultUnverified
	if err := h.Append(harness); err != nil {
		t.Fatalf("a harness result was refused: %v", err)
	}
}

func TestUnmonitoredExecutionStaysInFlightAndKeepsItsReservation(t *testing.T) {
	entries := []history.Entry{
		observed(t, "mon-a", history.KindAdmitted, ""),
		observed(t, "mon-a", history.KindLaunched, ""),
		observed(t, "mon-a", history.KindUnmonitored, ""),
	}
	p := history.Project(entries)
	u, _ := p.Unit("root", "mon-a")
	if u.State != history.StateInFlight || u.Flight != history.FlightUnmonitored || !u.Launched || p.InFlight() != 1 {
		t.Fatalf("unit = %+v, held = %d; want unmonitored work still in flight and holding its slot", u, p.InFlight())
	}
	if got := history.BuildSnapshot(entries).Nodes[0].Flight; got != history.FlightUnmonitored {
		t.Fatalf("snapshot flight = %q", got)
	}
}

func TestSnapshotCarriesAssuranceAndUnmonitoredWireNames(t *testing.T) {
	entries := append(settledUnit(t, "wire-a"), resultAt(t, "wire-a", history.ResultVerified, 4, "journal/7"),
		observed(t, "wire-b", history.KindAdmitted, ""), observed(t, "wire-b", history.KindUnmonitored, ""))
	raw, err := json.Marshal(history.BuildSnapshot(entries).Nodes)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"assurance":{"status":"verified","revision":4,"evidence":"journal/7","author":"verification"}`, `"flight":"unmonitored"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("nodes %s lack %s", raw, want)
		}
	}
	if strings.Contains(strings.SplitN(string(raw), `"id":"wire-b"`, 2)[1], "assurance") {
		t.Errorf("a unit with no result reports assurance: %s", raw)
	}
}
