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
// prerequisites map Project's own state; NodeKind carries an explicit recorded
// classification or the backward-compatible delegated default.
type Node struct {
	ID string `json:"id"`
	// Work-unit nodes are delegated only; direct and maintenance records live in
	// the separate Activities array.
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
	Stopped   int    `json:"stopped,omitempty"`
	Escalated int    `json:"escalated,omitempty"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	// Activities are direct orchestrator and maintenance records. They are
	// rendered in a separate lane and never participate in graph edges.
	Activities []ActivityNode `json:"activities"`
}

// BuildSnapshot derives the content-free DAG snapshot from the recorded
// prefix. It calls Project once and only maps its output: nothing here
// recomputes a state Project already computes.
//
// Because Project is a pure fold over entries (PR-DAG-REP-1), entry count
// alone gives a deterministic, monotonic projection_revision equal to
// history_position.
//
// The plan belongs to one root/orchestrator session; Snapshot names it by
// taking the earliest-appearing SessionID in entries (Seq order). A workspace
// whose history genuinely mixes more than one root session is not a case this
// function tries to disambiguate further.
func BuildSnapshot(entries []Entry) Snapshot {
	p, inferred := projectWithExecutionOrder(entries)
	session := rootSession(entries)
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
	// byte (PRD_DAG_TUI.md §8's "same prefix yields same snapshot").
	ids := make([]string, 0, len(p.Units))
	for id := range p.Units {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		u := p.Units[id]
		if u.Flight == FlightUncertain {
			s.Capture = CaptureUncertain
		}
		prerequisites := u.Prerequisites
		if !u.Committed {
			prerequisites = inferred[id]
		}
		kind := u.NodeKind
		if kind == "" {
			kind = NodeKindDelegated // legacy history carries no discriminator
		}
		s.Nodes = append(s.Nodes, Node{
			ID: id, NodeKind: kind, SessionID: u.SessionID, AttemptID: u.AttemptID,
			State: u.State, Flight: u.Flight, Outcome: u.Outcome,
			Launched: u.Launched, Contract: u.Contract, Prerequisites: prerequisites,
		})
		for _, from := range prerequisites {
			s.Edges = append(s.Edges, Edge{From: from, To: id})
		}
	}
	activityIDs := make([]string, 0, len(p.Activities))
	for id := range p.Activities {
		activityIDs = append(activityIDs, id)
	}
	slices.Sort(activityIDs)
	for _, id := range activityIDs {
		a := p.Activities[id]
		s.Activities = append(s.Activities, ActivityNode{
			ActivityID: a.ActivityID, NodeKind: a.NodeKind, State: a.State,
			Flight: a.Flight, Outcome: a.Outcome,
		})
	}
	return s
}

// projectWithExecutionOrder is Project plus the structure of ordinary work the
// orchestrator delegated without committing a plan. That work has no declared
// prerequisites, so the graph shows it as it ran: an uncovered delegated unit
// follows the units already settled when it was first admitted, reduced to the
// frontier, and units admitted while others were still in flight stay side by
// side. Non-work-unit activities are folded separately and receive no edges.
// Committed units keep exactly their declared prerequisites. The result is a
// pure fold of the recorded prefix, like the projection itself.
func projectWithExecutionOrder(entries []Entry) (Projection, map[string][]string) {
	p := newProjection()
	inferred := map[string][]string{}
	for _, e := range entries {
		_, known := p.Units[e.WorkUnitID]
		var settled []string
		if e.Kind == KindAdmitted && !known {
			settled = settledUnitIDs(&p)
		}
		p.fold(e)
		if _, now := p.Units[e.WorkUnitID]; e.Kind == KindAdmitted && !known && now {
			inferred[e.WorkUnitID] = frontier(settled, prerequisitesOf(&p, inferred))
		}
	}
	return p, inferred
}

// settledUnitIDs lists the units already settled in p, the candidates an
// admitted-but-uncovered unit's inferred prerequisites are drawn from.
func settledUnitIDs(p *Projection) []string {
	var settled []string
	for id, u := range p.Units {
		if u.State == StateSettled {
			settled = append(settled, id)
		}
	}
	return settled
}

// prerequisitesOf resolves a unit's prerequisites for frontier reduction: a
// committed unit's declared prerequisites, or an inferred unit's already
// computed ones.
func prerequisitesOf(p *Projection, inferred map[string][]string) func(string) []string {
	return func(id string) []string {
		if u := p.Units[id]; u.Committed {
			return u.Prerequisites
		}
		return inferred[id]
	}
}

// frontier keeps the candidates no other candidate already depends on,
// directly or transitively, sorted so the result is deterministic.
func frontier(candidates []string, prerequisitesOf func(string) []string) []string {
	implied := map[string]bool{}
	for _, c := range candidates {
		seen := map[string]bool{}
		var walk func(string)
		walk = func(id string) {
			for _, prerequisite := range prerequisitesOf(id) {
				if !seen[prerequisite] {
					seen[prerequisite] = true
					implied[prerequisite] = true
					walk(prerequisite)
				}
			}
		}
		walk(c)
	}
	var out []string
	for _, c := range candidates {
		if !implied[c] {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return out
}

// rootSession names the session Snapshot treats as the plan's owner; see
// BuildSnapshot's doc comment.
func rootSession(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	return entries[0].SessionID
}
