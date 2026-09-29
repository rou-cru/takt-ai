// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package gc

import (
	"fmt"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// BarrierPolicyRef names the barrier rule on every record.
const BarrierPolicyRef = "gc.barrier"

// Hold reasons keep a stopped barrier explainable in the record.
const (
	// ReasonActiveUnits holds the cycle while unfinished ordinary dispatches remain.
	ReasonActiveUnits = "active_units"
	// ReasonPendingDeltas holds the cycle while unresolved ordinary VFS deltas remain.
	ReasonPendingDeltas = "pending_ordinary_deltas"
)

// BarrierInput carries the A9 decision and the execution facts the barrier depends on.
// All facts are data supplied by the harness; the barrier reads no state of its own.
type BarrierInput struct {
	// Decision is the trigger result; only OutcomeRun is subject to the barrier.
	Decision Decision
	// ActiveUnits counts all unfinished ordinary dispatches, even outside scope.
	ActiveUnits int
	// ActiveUnitIDs names the units ActiveUnits counts; diagnostics only, it
	// never affects Proceed or Reason.
	ActiveUnitIDs []string
	// PendingOrdinaryDeltas counts ordinary VFS deltas not yet resolved.
	PendingOrdinaryDeltas int
	// PendingDeltaIdentities names the deltas PendingOrdinaryDeltas counts;
	// diagnostics only, it never affects Proceed or Reason.
	PendingDeltaIdentities []string
	// Deferrals is the current consecutive-deferral count carried by the caller (TriggerInput.Deferrals).
	Deferrals int
}

// BarrierVerdict says whether the cycle may start and what the caller must carry forward.
type BarrierVerdict struct {
	// Decision echoes the trigger decision unchanged, so records keep the trigger's trigger, units and mutations.
	Decision Decision `json:"decision"`
	// Proceed is true only for a run decision that met the barrier.
	Proceed bool `json:"proceed"`
	// Reason is set only when the barrier held the cycle.
	Reason string `json:"reason,omitempty"`
	// Deferrals is the value the caller must store as TriggerInput.Deferrals:
	// incremented on hold, reset on proceed, untouched otherwise.
	Deferrals int `json:"deferrals"`
	// ActiveUnitIDs and PendingDeltaIdentities name what a held barrier is
	// waiting on, so a stalled cycle is explainable, not just measured.
	ActiveUnitIDs          []string `json:"active_unit_ids,omitempty"`
	PendingDeltaIdentities []string `json:"pending_delta_identities,omitempty"`
}

// Held says whether the barrier itself stopped a due cycle.
func (v BarrierVerdict) Held() bool { return v.Reason != "" }

// Barrier gates a due cycle on the maintenance barrier.
func Barrier(in BarrierInput) BarrierVerdict {
	v := BarrierVerdict{Decision: in.Decision, Deferrals: in.Deferrals}
	if in.Decision.Outcome != OutcomeRun {
		return v
	}
	switch {
	case in.ActiveUnits != 0:
		v.Reason = ReasonActiveUnits
	case in.PendingOrdinaryDeltas != 0:
		v.Reason = ReasonPendingDeltas
	}
	if v.Held() {
		v.Deferrals++
		v.ActiveUnitIDs, v.PendingDeltaIdentities = in.ActiveUnitIDs, in.PendingDeltaIdentities
		return v
	}
	v.Proceed, v.Deferrals = true, 0
	return v
}

// ControlRecord renders a stopped barrier for the control bus.
func (v BarrierVerdict) ControlRecord(agent string) (obs.ControlRecord, bool) {
	if !v.Held() {
		return obs.ControlRecord{}, false
	}
	d := v.Decision
	cond := fmt.Sprintf("gc barrier stopped (trigger=%s units=%d mutations=%d deferrals=%d) reason=%s", d.Trigger, d.Units, d.Mutations, v.Deferrals, v.Reason)
	if len(v.ActiveUnitIDs) > 0 {
		cond += fmt.Sprintf(" blocking_units=%v", v.ActiveUnitIDs)
	}
	if len(v.PendingDeltaIdentities) > 0 {
		cond += fmt.Sprintf(" blocking_deltas=%v", v.PendingDeltaIdentities)
	}
	// PR-MNT-3: a stalled barrier halts subsequent task dispatch and must
	// reach a human, not merely be watched (PRD_GC's documented Failure
	// Behavior, which ActionObserve here previously contradicted).
	return obs.ControlRecord{ActionClass: obs.ActionEscalate, TriggeringCondition: cond, PolicyRef: BarrierPolicyRef, ActingAgent: agent}, true
}
