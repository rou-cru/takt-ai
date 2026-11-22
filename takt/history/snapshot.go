// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history

import (
	"encoding/json"
	"slices"
)

// SnapshotSchemaVersion is the wire shape version every snapshot reports, so a
// future breaking change to the contract is visible to a caller instead of
// silently changing shape under it.
const SnapshotSchemaVersion = 1

// Capture states the snapshot contract distinguishes (PRD_DAG_TUI.md §7.4,
// §11). CaptureUnavailable is deliberately not produced by Snapshot itself: a
// failed workspace/history read never reaches Project, so it never reaches a
// Snapshot value either. The command that opens the history (runDag in
// takt/cli/dag.go) builds that JSON object itself on a read failure; the
// constant lives here only so both sides name the same string.
const (
	// CaptureCurrent is a successful, non-empty read of the recorded history.
	CaptureCurrent = "current"
	// CaptureEmpty is a successful read of a history that has recorded
	// nothing yet: an explicit empty plan, distinguishable from a failed read.
	CaptureEmpty = "empty"
	// CaptureUncertain is a successful read in which at least one unit's
	// launch or capture could not be reconciled (FlightUncertain).
	CaptureUncertain = "uncertain"
	// CaptureUnavailable marks a control-plane query failure. Never set on a
	// Snapshot value; see the doc comment above.
	CaptureUnavailable = "unavailable"
)

// The wire names of PRD_DAG_TUI.md §8 are snake_case identifiers; the
// projection's own State and Flight values read as prose ("in flight"). Only
// the JSON form is renamed, so every in-memory comparison keeps the
// projection's values.
var (
	stateWire  = map[State]string{StateInFlight: "in_flight"}
	flightWire = map[Flight]string{
		FlightPendingLaunch: "pending_launch",
		FlightRunning:       "observed_running",
		FlightCancelling:    "cancellation_pending",
		FlightUncertain:     "uncertain",
		FlightUnmonitored:   "unmonitored",
	}
)

// MarshalJSON emits the state's wire name.
func (s State) MarshalJSON() ([]byte, error) {
	if w, ok := stateWire[s]; ok {
		return json.Marshal(w)
	}
	return json.Marshal(string(s))
}

// MarshalJSON emits the flight's wire name.
func (f Flight) MarshalJSON() ([]byte, error) {
	if w, ok := flightWire[f]; ok {
		return json.Marshal(w)
	}
	return json.Marshal(string(f))
}

// Node is one work unit's content-free projection. Temporal fields and declared
// prerequisites map Project's own state; NodeKind carries the recorded
// classification.
type Node struct {
	ID string `json:"id"`
	// Work-unit nodes are delegated only; direct records live in the separate
	// Activities array.
	NodeKind  NodeKind `json:"node_kind"`
	SessionID string   `json:"session_id,omitempty"`
	AttemptID string   `json:"attempt_id,omitempty"`
	State     State    `json:"state"`
	Flight    Flight   `json:"flight,omitempty"`
	// Outcome is set only once the unit is settled.
	Outcome       Outcome  `json:"outcome,omitempty"`
	Launched      bool     `json:"launched"`
	Contract      string   `json:"contract,omitempty"`
	Prerequisites []string `json:"prerequisites,omitempty"`
	// Layer is the unit's depth: 0 without prerequisites, otherwise one more than
	// its deepest prerequisite (PR-DTUI-9). The view never recomputes it.
	Layer int `json:"layer"`
	// Agent identifies the specialist admitted for the current attempt; empty
	// until the unit is first admitted.
	Agent string `json:"agent,omitempty"`
}

// ActivityNode is a non-work-unit activity projected separately from Nodes.
// ActivityID retains the source identity (for GC, the exact CycleID) and is
// never used as a work-unit ID or edge endpoint.
type ActivityNode struct {
	ActivityID string   `json:"activity_id"`
	NodeKind   NodeKind `json:"node_kind"`
	State      State    `json:"state"`
	Flight     Flight   `json:"flight,omitempty"`
	Outcome    Outcome  `json:"outcome,omitempty"`
}

// Edge is one declared prerequisite, directed from the prerequisite to its
// dependent (PRD_DAG_TUI.md §5). It describes declared structure only; it
// grants no scheduling or blocking authority.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Snapshot is the read-only, content-free DAG projection PRD_DAG_TUI.md §8
// contracts: identities, states and structural references only, never
// prompts, responses, or other free-text content.
type Snapshot struct {
	SchemaVersion      int    `json:"schema_version"`
	ProjectionRevision int    `json:"projection_revision"`
	HistoryPosition    int    `json:"history_position"`
	SessionID          string `json:"session_id"`
	Capture            string `json:"capture"`
	// PlanVersion is the current committed plan's version identity, tracked
	// per session and carried through untouched by an invalid revision.
	PlanVersion string `json:"plan_version,omitempty"`
	// Stopped and Escalated mirror Budgets' own counters: distinguishable
	// from an enabling decision, never inferred from Nodes/Edges.
	Stopped   int `json:"stopped,omitempty"`
	Escalated int `json:"escalated,omitempty"`
	// Counts tallies the units by what the view shows; withdrawn units are
	// counted apart and are not work.
	Counts Counts `json:"counts"`
	Nodes  []Node `json:"nodes"`
	Edges  []Edge `json:"edges"`
	// Activities are direct orchestrator records. They are
	// rendered in a separate lane and never participate in graph edges.
	Activities []ActivityNode `json:"activities"`
}

// Counts is the number of units in each state the view tells apart.
type Counts struct {
	Planned     int `json:"planned"`
	InFlight    int `json:"in_flight"`
	Completed   int `json:"completed"`
	Failed      int `json:"failed"`
	Backtracked int `json:"backtracked"`
	Interrupted int `json:"interrupted"`
	Withdrawn   int `json:"withdrawn"`
}

// BuildSnapshot derives the content-free DAG snapshot from the recorded
// prefix, mapping Project's output; layers and counts are the only facts it
// derives, so every view reads them from here (PR-DTUI-3). Work delegated
// without a committed plan is a node with no edges: the projection records
// observed work, never invented prerequisites (PR-DAG-REP-3). Because
// Project is a pure fold over entries (PR-DAG-REP-1), entry count alone gives a
// deterministic, monotonic projection_revision equal to history_position. The
// plan belongs to one root session, named as the earliest-appearing SessionID
// in Seq order, and only that session's units and activities are drawn: a unit
// name is unique within its root, so another root's units would collide. A
// caller that wants another root filters the entries to it first.
//
// A root that declared continuity with earlier roots (PR-DAG-USE-1) passes
// them, oldest first, as continued: the snapshot is then scoped to that root
// and the sessions it continued, the earlier history preceding the new entries,
// and a unit name the root reuses shows the root's own unit, not the earlier one.
func BuildSnapshot(entries []Entry, continued ...string) Snapshot {
	p := Project(entries)
	session := rootSession(entries)
	scope := []string{session}
	if len(continued) > 0 {
		session = continued[len(continued)-1]
		scope = continued
	}
	s := Snapshot{
		SchemaVersion:      SnapshotSchemaVersion,
		ProjectionRevision: len(entries),
		HistoryPosition:    len(entries),
		SessionID:          session,
		Capture:            CaptureCurrent,
		PlanVersion:        p.Budgets(session).PlanVersion,
		Stopped:            p.Budgets(session).Stopped,
		Escalated:          p.Budgets(session).Escalated,
		Nodes:              make([]Node, 0, len(p.Units)),
		Edges:              []Edge{},
		Activities:         make([]ActivityNode, 0, len(p.Activities)),
	}
	if len(entries) == 0 {
		s.Capture = CaptureEmpty
	}
	// Units is a map; iterating it directly would make node and edge order
	// vary run to run even though the projection itself is deterministic. IDs
	// are sorted so two Snapshot calls over the same prefix agree byte for
	// byte (PRD_DAG_TUI.md §8's "same prefix yields same snapshot"). A name in
	// several scoped sessions is drawn once, from the latest.
	drawn := map[string]Unit{}
	for _, u := range p.Units {
		at := slices.Index(scope, u.SessionID)
		if at < 0 {
			continue
		}
		if known, seen := drawn[u.WorkUnitID]; !seen || slices.Index(scope, known.SessionID) < at {
			drawn[u.WorkUnitID] = u
		}
	}
	names := make([]string, 0, len(drawn))
	for name := range drawn {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		u := drawn[name]
		id := u.WorkUnitID
		if u.Flight == FlightUncertain {
			s.Capture = CaptureUncertain
		}
		s.Nodes = append(s.Nodes, Node{
			ID: id, NodeKind: NodeKindDelegated, SessionID: u.SessionID, AttemptID: u.AttemptID,
			State: u.State, Flight: u.Flight, Outcome: u.Outcome,
			Launched: u.Launched, Contract: u.Contract, Prerequisites: u.Prerequisites, Agent: u.Agent,
		})
		for _, from := range u.Prerequisites {
			s.Edges = append(s.Edges, Edge{From: from, To: id})
		}
	}
	s.assignLayers()
	s.countUnits()
	activityIDs := make([]string, 0, len(p.Activities))
	for id := range p.Activities {
		activityIDs = append(activityIDs, id)
	}
	slices.Sort(activityIDs)
	for _, id := range activityIDs {
		a := p.Activities[id]
		if !slices.Contains(scope, a.SessionID) {
			continue
		}
		s.Activities = append(s.Activities, ActivityNode{
			ActivityID: a.ActivityID, NodeKind: a.NodeKind, State: a.State,
			Flight: a.Flight, Outcome: a.Outcome,
		})
	}
	return s
}

// rootSession names the session Snapshot treats as the plan's owner; see
// BuildSnapshot's doc comment.
func rootSession(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[0].SessionID
}

// assignLayers sets every node's Layer from the edges it carries. The plan is
// acyclic by its validation (PR-DAG-MUT-6), so the recursion ends.
func (s *Snapshot) assignLayers() {
	index := make(map[string]int, len(s.Nodes))
	for i, n := range s.Nodes {
		index[n.ID] = i
	}
	done := make([]bool, len(s.Nodes))
	var layerOf func(i int) int
	layerOf = func(i int) int {
		if done[i] {
			return s.Nodes[i].Layer
		}
		layer := 0
		for _, prerequisite := range s.Nodes[i].Prerequisites {
			if j, ok := index[prerequisite]; ok {
				layer = max(layer, layerOf(j)+1)
			}
		}
		s.Nodes[i].Layer, done[i] = layer, true
		return layer
	}
	for i := range s.Nodes {
		layerOf(i)
	}
}

// countUnits tallies Nodes into Counts.
func (s *Snapshot) countUnits() {
	for _, n := range s.Nodes {
		switch {
		case n.State == StatePlanned:
			s.Counts.Planned++
		case n.State == StateInFlight:
			s.Counts.InFlight++
		case n.State == StateWithdrawn:
			s.Counts.Withdrawn++
		case n.Outcome == OutcomeCompleted:
			s.Counts.Completed++
		case n.Outcome == OutcomeFailed:
			s.Counts.Failed++
		case n.Outcome == OutcomeBacktracked:
			s.Counts.Backtracked++
		case n.Outcome == OutcomeInterrupted:
			s.Counts.Interrupted++
		}
	}
}
