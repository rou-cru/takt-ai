// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// planned builds a KindPlanned entry the way dispatch.Commit/Revise record
// one, without importing dispatch (history must not depend on its own
// consumer).
func planned(session, unit, version, contract string, prereqs []string) Entry {
	return Entry{Author: AuthorOrchestrator, Kind: KindPlanned, SessionID: session, WorkUnitID: unit,
		NodeKind: NodeKindDelegated, AttemptID: FirstAttempt, Cause: CauseNone, PlanVersion: version, Contract: contract, Prerequisites: prereqs}
}

// withdrawn builds a KindWithdrawn entry the way dispatch.Revise records one.
func withdrawn(session, unit string) Entry {
	return Entry{Author: AuthorOrchestrator, Kind: KindWithdrawn, SessionID: session, WorkUnitID: unit,
		AttemptID: FirstAttempt, Cause: CauseUncaptured}
}

// revised builds a KindRevised entry the way dispatch.Revise records one.
func revised(session, base, version, classification string) Entry {
	return Entry{Author: AuthorOrchestrator, Kind: KindRevised, SessionID: session, WorkUnitID: session,
		AttemptID: FirstAttempt, Cause: CauseNone, BaseVersion: base, PlanVersion: version, Classification: classification}
}

func activityRecord(session, id string, kind NodeKind, start bool, outcome Outcome) Entry {
	e := Entry{SessionID: session, ActivityID: id, NodeKind: kind, Cause: CauseNone, Outcome: outcome}
	if kind == NodeKindOrchestrator {
		e.Author = AuthorOrchestrator
		if start {
			e.Kind = KindActivityStarted
		} else {
			e.Kind = KindActivityFinished
		}
	} else {
		e.Author = AuthorHarness
		if start {
			e.Kind = KindMaintenanceStarted
		} else {
			e.Kind = KindMaintenanceFinished
		}
	}
	return e
}

// TestBuildSnapshotShapeAcrossARevision pins the exact JSON shape of
// PRD_DAG_TUI.md §8 against a plan that has actually been revised, not just
// committed once: plan_version and topology both come from the revision.
func TestBuildSnapshotShapeAcrossARevision(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	entries := []Entry{
		planned("root", "storage", "v1", "storage contract", nil),
		planned("root", "api", "v1", "api contract", []string{"storage"}),
		observed(t, "storage", KindAdmitted, ""),
		observed(t, "storage", KindLaunched, ""),
		observed(t, "storage", KindTerminated, OutcomeCompleted),
		// A valid revision: withdraw "api", add "api-v2" reconnected to
		// "storage" (strategic, since a retained unit's prerequisites would
		// change; here "api" is withdrawn rather than retained, but the
		// revision entry itself is what Snapshot must reflect either way).
		revised("root", "v1", "v2", ClassificationStrategic),
		withdrawn("root", "api"),
		planned("root", "api-v2", "v2", "api contract v2", []string{"storage"}),
	}
	for _, e := range entries {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	got := BuildSnapshot(h.Entries())
	want := Snapshot{
		SchemaVersion:      SnapshotSchemaVersion,
		ProjectionRevision: len(entries),
		HistoryPosition:    len(entries),
		SessionID:          "root",
		Capture:            CaptureCurrent,
		PlanVersion:        "v2",
		Nodes: []Node{
			// A withdrawn unit keeps the contract/prerequisites its own
			// KindPlanned entry recorded: Project never clears them on
			// withdrawal, and Snapshot maps Project's Unit fields 1:1 rather
			// than inventing a "withdrawal clears prerequisites" rule of
			// its own.
			{ID: "api", NodeKind: NodeKindDelegated, SessionID: "root", AttemptID: FirstAttempt, State: StateWithdrawn, Contract: "api contract", Prerequisites: []string{"storage"}},
			{ID: "api-v2", NodeKind: NodeKindDelegated, SessionID: "root", AttemptID: FirstAttempt, State: StatePlanned, Contract: "api contract v2", Prerequisites: []string{"storage"}},
			{ID: "storage", NodeKind: NodeKindDelegated, SessionID: "root", AttemptID: FirstAttempt, State: StateSettled, Outcome: OutcomeCompleted, Launched: true, Contract: "storage contract"},
		},
		Edges:      []Edge{{From: "storage", To: "api"}, {From: "storage", To: "api-v2"}},
		Activities: []ActivityNode{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot %+v; want %+v", got, want)
	}
}

// TestBuildSnapshotIsDeterministic proves the same recorded prefix yields the
// same revision, nodes and edges no matter how many times it is replayed
// (PRD_DAG_TUI.md §8's "same prefix yields same snapshot").
func TestBuildSnapshotIsDeterministic(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	entries := []Entry{
		planned("root", "a", "v1", "a contract", nil),
		planned("root", "b", "v1", "b contract", []string{"a"}),
		planned("root", "c", "v1", "c contract", []string{"a"}),
		planned("root", "d", "v1", "d contract", []string{"b", "c"}),
	}
	for _, e := range entries {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	first := BuildSnapshot(h.Entries())
	second := BuildSnapshot(h.Entries())
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("snapshot not deterministic:\n%+v\n%+v", first, second)
	}
	if first.ProjectionRevision != len(entries) || first.ProjectionRevision != first.HistoryPosition {
		t.Fatalf("revision %d, position %d; want both %d", first.ProjectionRevision, first.HistoryPosition, len(entries))
	}
}

// TestBuildSnapshotEdgesChainFanOutFanInMultiRoot pins edge derivation over a
// topology with two roots, a fan-out and a fan-in.
func TestBuildSnapshotEdgesChainFanOutFanInMultiRoot(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	entries := []Entry{
		planned("root", "domain", "v1", "domain contract", nil), // root 1
		planned("root", "config", "v1", "config contract", nil), // root 2
		planned("root", "storage", "v1", "storage contract", []string{"domain"}),
		planned("root", "api-tests", "v1", "api-tests contract", []string{"storage", "config"}), // fan-in
		planned("root", "api-docs", "v1", "api-docs contract", []string{"storage"}),             // fan-out sibling
	}
	for _, e := range entries {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	got := BuildSnapshot(h.Entries())
	want := []Edge{
		{From: "storage", To: "api-docs"},
		{From: "storage", To: "api-tests"},
		{From: "config", To: "api-tests"},
		{From: "domain", To: "storage"},
	}
	if len(got.Edges) != len(want) {
		t.Fatalf("edges %+v; want %+v", got.Edges, want)
	}
	for _, e := range want {
		if !containsEdge(got.Edges, e) {
			t.Fatalf("missing edge %+v in %+v", e, got.Edges)
		}
	}
}

// containsEdge avoids depending on edge ordering across nodes here:
// BuildSnapshot's own determinism is already pinned separately above, this
// test only cares that every declared prerequisite produced its edge.
func containsEdge(edges []Edge, want Edge) bool {
	for _, e := range edges {
		if e == want {
			return true
		}
	}
	return false
}

// TestBuildSnapshotWithdrawal pins that a withdrawn unit is still reported,
// as withdrawn, never silently dropped.
func TestBuildSnapshotWithdrawal(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	for _, e := range []Entry{
		planned("root", "a", "v1", "a contract", nil),
		withdrawn("root", "a"),
	} {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	got := BuildSnapshot(h.Entries())
	if len(got.Nodes) != 1 || got.Nodes[0].ID != "a" || got.Nodes[0].State != StateWithdrawn {
		t.Fatalf("nodes %+v; want one withdrawn node", got.Nodes)
	}
}

// TestBuildSnapshotSettlementOutcomes covers every settled outcome the
// projection can carry.
func TestBuildSnapshotSettlementOutcomes(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	outcomes := map[string]Outcome{
		"completed":   OutcomeCompleted,
		"failed":      OutcomeFailed,
		"backtracked": OutcomeBacktracked,
		"interrupted": OutcomeInterrupted,
	}
	for unit, outcome := range outcomes {
		if err := h.Append(planned("root", unit, "v1", unit+" contract", nil)); err != nil {
			t.Fatal(err)
		}
		if err := h.Append(observed(t, unit, KindAdmitted, "")); err != nil {
			t.Fatal(err)
		}
		if err := h.Append(observed(t, unit, KindTerminated, outcome)); err != nil {
			t.Fatal(err)
		}
	}
	got := BuildSnapshot(h.Entries())
	seen := map[string]Outcome{}
	for _, n := range got.Nodes {
		if n.State != StateSettled {
			t.Fatalf("node %+v not settled", n)
		}
		seen[n.ID] = n.Outcome
	}
	if !reflect.DeepEqual(seen, outcomes) {
		t.Fatalf("settled outcomes %+v; want %+v", seen, outcomes)
	}
}

// TestBuildSnapshotUncertainFlight proves an unreconciled launch is reported
// as the projection's own FlightUncertain sub-state, never omitted or
// collapsed into a guessed settled outcome (PRD_DAG_TUI.md §7.4/§11).
func TestBuildSnapshotUncertainFlight(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	for _, e := range []Entry{
		planned("root", "a", "v1", "a contract", nil),
		observed(t, "a", KindAdmitted, ""),
		observed(t, "a", KindUncertain, ""),
	} {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	got := BuildSnapshot(h.Entries())
	if len(got.Nodes) != 1 || got.Nodes[0].State != StateInFlight || got.Nodes[0].Flight != FlightUncertain {
		t.Fatalf("nodes %+v; want one in-flight node with FlightUncertain", got.Nodes)
	}
	if got.Capture != CaptureUncertain {
		t.Fatalf("capture = %q; want %q", got.Capture, CaptureUncertain)
	}
}

// TestBuildSnapshotEmptyHistoryIsExplicit proves a zero-entry history is
// reported as an explicit empty DAG, distinguishable from unavailable.
func TestBuildSnapshotEmptyHistoryIsExplicit(t *testing.T) {
	got := BuildSnapshot(nil)
	if got.Capture != CaptureEmpty {
		t.Fatalf("capture = %q; want %q", got.Capture, CaptureEmpty)
	}
	if len(got.Nodes) != 0 || len(got.Edges) != 0 {
		t.Fatalf("empty history produced data: %+v", got)
	}
	if got.ProjectionRevision != 0 || got.HistoryPosition != 0 {
		t.Fatalf("revision/position not zero: %+v", got)
	}
}

// TestSnapshotWireNames proves the JSON form carries the PRD_DAG_TUI.md §8
// snake_case names while the in-memory projection values stay as they are.
func TestSnapshotWireNames(t *testing.T) {
	node := Node{ID: "a", NodeKind: NodeKindDelegated, State: StateInFlight, Flight: FlightRunning}
	raw, err := json.Marshal(node)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"node_kind":"delegated"`) || !strings.Contains(string(raw), `"state":"in_flight"`) || !strings.Contains(string(raw), `"flight":"observed_running"`) {
		t.Fatalf("wire form %s; want node_kind / in_flight / observed_running", raw)
	}
}

func TestBuildSnapshotNodeKindsPreserveIdentityAndPrerequisites(t *testing.T) {
	entries := []Entry{
		func() Entry {
			e := planned("root", "delegated", "v1", "child work", nil)
			e.NodeKind = NodeKindDelegated
			return e
		}(),
		planned("root", "dependent", "v1", "depends on delegated work", []string{"delegated"}),
		revised("root", "v1", "v2", ClassificationTactical),
	}
	got := BuildSnapshot(entries)
	kinds := make(map[string]NodeKind, len(got.Nodes))
	for _, node := range got.Nodes {
		kinds[node.ID] = node.NodeKind
	}
	wantKinds := map[string]NodeKind{
		"delegated": NodeKindDelegated,
		"dependent": NodeKindDelegated,
	}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("node kinds %v; want %v", kinds, wantKinds)
	}
	wantEdges := []Edge{{From: "delegated", To: "dependent"}}
	if !reflect.DeepEqual(got.Edges, wantEdges) {
		t.Fatalf("edges %+v; want %+v", got.Edges, wantEdges)
	}
	if _, found := kinds["root"]; found {
		t.Fatalf("plan/session identity was invented as a node: %v", kinds)
	}
}

func TestWorkUnitEntriesRejectActivityNodeKinds(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	for _, kind := range []NodeKind{NodeKindOrchestrator, NodeKindMaintenance} {
		e := planned("root", string(kind), "v1", "not a work-unit activity", nil)
		e.NodeKind = kind
		if err := h.Append(e); err == nil {
			t.Fatalf("work-unit entry accepted activity kind %q", kind)
		}
	}
}

func TestSnapshotProjectsActivitiesSeparateFromWorkUnits(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	cycleID := "a13f86e2b7c94e6d"
	directID := "orchestrator-activity-7fca2e"
	entries := []Entry{
		planned("root", "base", "v1", "base", nil),
		planned("root", "dependent", "v1", "dependent", []string{"base"}),
		observed(t, "base", KindAdmitted, ""),
		observed(t, "base", KindLaunched, ""),
		observed(t, "base", KindTerminated, OutcomeCompleted),
		observed(t, "later", KindAdmitted, ""), // ordinary delegation follows settled base
		activityRecord("root", directID, NodeKindOrchestrator, true, ""),
		activityRecord("root", directID, NodeKindOrchestrator, false, OutcomeCompleted),
		activityRecord("root", cycleID, NodeKindMaintenance, true, ""),
		activityRecord("root", cycleID, NodeKindMaintenance, false, OutcomeInterrupted),
	}
	for _, e := range entries {
		if err := h.Append(e); err != nil {
			t.Fatalf("append %+v: %v", e, err)
		}
	}
	p := h.Project()
	if len(p.Units) != 3 {
		t.Fatalf("units=%v; want only base, dependent and later", p.Units)
	}
	if _, found := p.Units[directID]; found {
		t.Fatalf("direct activity was folded into work units: %v", p.Units)
	}
	if _, found := p.Units[cycleID]; found {
		t.Fatalf("GC CycleID was folded into work units: %v", p.Units)
	}
	if len(p.Activities) != 2 || p.Activities[directID].NodeKind != NodeKindOrchestrator || p.Activities[cycleID].NodeKind != NodeKindMaintenance {
		t.Fatalf("activities %+v; want direct and maintenance activities", p.Activities)
	}
	for _, e := range h.Entries() {
		if e.ActivityID != "" && (e.WorkUnitID != "" || e.AttemptID != "") {
			t.Fatalf("activity identity leaked into work identity: %+v", e)
		}
	}

	snapshot := BuildSnapshot(h.Entries())
	if len(snapshot.Nodes) != 3 {
		t.Fatalf("work nodes=%+v; activities must stay outside nodes", snapshot.Nodes)
	}
	for _, node := range snapshot.Nodes {
		if node.NodeKind != NodeKindDelegated {
			t.Fatalf("delegated unit %q got kind %q", node.ID, node.NodeKind)
		}
	}
	if len(snapshot.Activities) != 2 || snapshot.Activities[0].ActivityID != cycleID || snapshot.Activities[1].ActivityID != directID {
		t.Fatalf("snapshot activities %+v; activity IDs must be retained and sorted", snapshot.Activities)
	}
	if snapshot.Activities[0].State != StateSettled || snapshot.Activities[0].Outcome != OutcomeInterrupted || snapshot.Activities[1].Outcome != OutcomeCompleted {
		t.Fatalf("activity lifecycle state lost: %+v", snapshot.Activities)
	}
	if !containsEdge(snapshot.Edges, Edge{From: "base", To: "dependent"}) || !containsEdge(snapshot.Edges, Edge{From: "base", To: "later"}) || len(snapshot.Edges) != 2 {
		t.Fatalf("activity entries changed declared/inferred unit edges: %+v", snapshot.Edges)
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"work_unit_id"`) || !strings.Contains(string(raw), `"activity_id":"`+cycleID+`"`) {
		t.Fatalf("snapshot did not preserve a distinct activity identity: %s", raw)
	}
}

// TestBuildSnapshotShowsUncoveredWorkAsExecuted proves work delegated without
// a committed plan is drawn as it ran: serial units chain, units admitted
// together are siblings, a later unit joins the frontier it followed, and a
// committed unit keeps exactly its declared prerequisites.
func TestBuildSnapshotShowsUncoveredWorkAsExecuted(t *testing.T) {
	run := func(unit string) []Entry {
		return []Entry{observed(t, unit, KindAdmitted, ""), observed(t, unit, KindLaunched, "")}
	}
	done := func(units ...string) (out []Entry) {
		for _, u := range units {
			out = append(out, observed(t, u, KindTerminated, OutcomeCompleted))
		}
		return
	}
	var entries []Entry
	entries = append(entries, run("pm")...)
	entries = append(entries, done("pm")...)
	entries = append(entries, run("arch")...)
	entries = append(entries, run("design")...) // admitted while arch is in flight
	entries = append(entries, done("arch", "design")...)
	entries = append(entries, run("spec")...)
	entries = append(entries, done("spec")...)
	entries = append(entries, planned("root", "tpm", "v1", "decompose", nil))
	entries = append(entries, run("tpm")...)

	got := map[string][]string{}
	for _, n := range BuildSnapshot(entries).Nodes {
		got[n.ID] = n.Prerequisites
	}
	want := map[string][]string{
		"pm":     nil,
		"arch":   {"pm"},
		"design": {"pm"},
		"spec":   {"arch", "design"},
		"tpm":    nil, // committed with no prerequisites: declared, not inferred
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prerequisites %v; want %v", got, want)
	}
	if empty := BuildSnapshot(nil); len(empty.Nodes) != 0 || empty.Capture != CaptureEmpty {
		t.Fatalf("empty history drew %+v", empty)
	}
}

// TestBuildSnapshotReportsStoppedAndEscalated checks that Snapshot exposes
// Budgets' own Stopped/Escalated counters, the same way it already does for
// PlanVersion.
func TestBuildSnapshotReportsStoppedAndEscalated(t *testing.T) {
	entries := []Entry{
		{Author: AuthorOrchestrator, Kind: KindStopped, SessionID: "root", WorkUnitID: "unit-1", AttemptID: FirstAttempt, Cause: CauseNone},
		{Author: AuthorOrchestrator, Kind: KindEscalated, SessionID: "root", WorkUnitID: "unit-1", AttemptID: FirstAttempt, Cause: CauseNone},
	}
	got := BuildSnapshot(entries)
	if got.Stopped != 1 || got.Escalated != 1 {
		t.Fatalf("Stopped = %d, Escalated = %d; want 1, 1", got.Stopped, got.Escalated)
	}
}

// TestBuildSnapshotCarriesTheAdmittedAgent proves a node names no agent while
// only planned, then the specialist of its current attempt: a retry admitted
// to another specialist replaces the first.
func TestBuildSnapshotCarriesTheAdmittedAgent(t *testing.T) {
	h := open(t, filepath.Join(t.TempDir(), "state"))
	agentOf := func() string {
		t.Helper()
		nodes := BuildSnapshot(h.Entries()).Nodes
		if len(nodes) != 1 {
			t.Fatalf("nodes = %+v, want one", nodes)
		}
		return nodes[0].Agent
	}
	admitted := func(agent, attempt string) Entry {
		e := observed(t, "a", KindAdmitted, "")
		e.Agent, e.AttemptID = agent, attempt
		return e
	}
	if err := h.Append(planned("root", "a", "v1", "a contract", nil)); err != nil {
		t.Fatal(err)
	}
	if got := agentOf(); got != "" {
		t.Fatalf("planned agent = %q, want none", got)
	}
	for _, e := range []Entry{admitted("pm", FirstAttempt), observed(t, "a", KindTerminated, OutcomeFailed)} {
		if err := h.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if got := agentOf(); got != "pm" {
		t.Fatalf("settled agent = %q, want pm", got)
	}
	if err := h.Append(admitted("dev", "2")); err != nil {
		t.Fatal(err)
	}
	if got := agentOf(); got != "dev" {
		t.Fatalf("retried agent = %q, want dev", got)
	}
}
