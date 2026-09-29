// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// Package gc computes and coordinates the workspace GC cycle: it decides when
// a cycle starts, declares its plan, and runs preparation, analysis,
// investigation, barrier and acceptance phases.
package gc

import (
	// Required blank import: the embed directive below depends on this package.
	_ "embed"
	"fmt"
	"sync"

	"github.com/rou-cru/takt-ai/takt/obs"
	"gopkg.in/yaml.v2"
)

//go:embed trigger_policy.yaml
var triggerPolicyYAML []byte

// loadTriggerPolicy shares one parsed policy because the embedded content never changes at runtime.
var loadTriggerPolicy = sync.OnceValues(func() (TriggerPolicy, error) {
	return parseTriggerPolicy(triggerPolicyYAML)
})

// TriggerPolicy holds the cadence parameters so they live in policy data, never in execution paths.
type TriggerPolicy struct {
	Version int `yaml:"version"`
	Limits  struct {
		Findings int `yaml:"findings"`
		Files    int `yaml:"files"`
	} `yaml:"limits"`
	Cadence struct {
		UnitsPerCycle     int `yaml:"units_per_cycle"`
		MutationsPerCycle int `yaml:"mutations_per_cycle"`
		MaxDeferrals      int `yaml:"max_deferrals"`
	} `yaml:"cadence"`
}

// LoadTriggerPolicy returns the shared embedded policy; callers must not mutate the result.
func LoadTriggerPolicy() (TriggerPolicy, error) {
	return loadTriggerPolicy()
}

// parseTriggerPolicy rejects unknown fields and non-positive parameters so a broken policy fails early instead of silently disabling GC.
func parseTriggerPolicy(data []byte) (TriggerPolicy, error) {
	var p TriggerPolicy
	if err := yaml.UnmarshalStrict(data, &p); err != nil {
		return TriggerPolicy{}, fmt.Errorf("gc trigger policy YAML: %w", err)
	}
	if p.Version != 1 {
		return TriggerPolicy{}, fmt.Errorf("gc trigger policy: unsupported version %d", p.Version)
	}
	c := p.Cadence
	if c.UnitsPerCycle <= 0 || c.MutationsPerCycle <= 0 || c.MaxDeferrals <= 0 {
		return TriggerPolicy{}, fmt.Errorf("gc trigger policy: cadence parameters must be positive")
	}
	return p, nil
}

// TriggerPolicyRef names the policy on every record.
const TriggerPolicyRef = "gc.trigger"

// TriggerKind says what asked for the cycle.
type TriggerKind string

// Trigger kinds are the closed set of things that can ask for a cycle.
const (
	// TriggerCadence marks a cycle made due by the pace of work.
	TriggerCadence TriggerKind = "cadence"
	// TriggerUser marks a cycle requested through the interface holder (PR-MNT-8).
	TriggerUser TriggerKind = "user"
)

// Outcome is the decision, so the barrier consumes one closed set.
type Outcome string

// Trigger outcomes are the closed decision set, so the barrier consumes one closed vocabulary.
const (
	// OutcomeIdle means nothing is due; no cycle exists, so nothing is recorded.
	OutcomeIdle Outcome = "idle"
	// OutcomeRun means a cycle should start.
	OutcomeRun Outcome = "run"
	// OutcomeSkip means a cycle was due but not started this time; it is recorded, never silent.
	OutcomeSkip Outcome = "skip"
	// OutcomeAbort means a due cycle is dropped for good and escalated; it is recorded.
	OutcomeAbort Outcome = "abort"
)

// Reasons keep skip and abort explainable in the record.
const (
	ReasonInFlight      = "cycle_in_flight"
	ReasonNoDelta       = "no_delta"
	ReasonDeferralLimit = "deferral_limit"
)

// TriggerInput carries the pace counters and state the decision depends on.
// The user request is data here, not a channel: the interface holder sets it (PR-MNT-8).
type TriggerInput struct {
	// UnitsDispatched counts work units dispatched since the last cycle.
	UnitsDispatched int
	// Mutations counts workspace mutations since the last cycle.
	Mutations int
	// UserRequested is set by the interface holder when the user asks for a cycle.
	UserRequested bool
	// CycleInFlight is set while a previous cycle has not completed, since cycles must not overlap.
	CycleInFlight bool
	// Deferrals counts consecutive times a due cycle was not started, including barrier deferrals.
	Deferrals int
}

// Decision is the trigger result and, for run, skip and abort, the record itself.
type Decision struct {
	Outcome Outcome     `json:"outcome"`
	Trigger TriggerKind `json:"trigger"`
	// Reason is empty for run and idle.
	Reason string `json:"reason,omitempty"`
	// Units and Mutations echo the pace that led to the decision, so records explain themselves.
	Units     int    `json:"units"`
	Mutations int    `json:"mutations"`
	PolicyRef string `json:"policy_ref"`
}

// Decide maps pace counters and policy to run, skip or abort.
func Decide(in TriggerInput, p TriggerPolicy) Decision {
	d := Decision{Trigger: TriggerCadence, Units: in.UnitsDispatched, Mutations: in.Mutations, PolicyRef: TriggerPolicyRef}
	if in.UserRequested {
		d.Trigger = TriggerUser
	}
	c := p.Cadence
	// Cross-multiplied share sum (u/U + m/M >= 1) keeps the comparison in integers.
	due := in.UserRequested ||
		in.UnitsDispatched*c.MutationsPerCycle+in.Mutations*c.UnitsPerCycle >= c.UnitsPerCycle*c.MutationsPerCycle
	switch {
	case !due:
		d.Outcome = OutcomeIdle
	case in.Deferrals >= c.MaxDeferrals:
		d.Outcome, d.Reason = OutcomeAbort, ReasonDeferralLimit
	case in.CycleInFlight:
		d.Outcome, d.Reason = OutcomeSkip, ReasonInFlight
	case in.Mutations == 0:
		// Nothing was introduced or made stale, so the causal closure would be empty (PR-MNT-4).
		d.Outcome, d.Reason = OutcomeSkip, ReasonNoDelta
	default:
		d.Outcome = OutcomeRun
	}
	return d
}

// Recorded says whether the decision must land on the control bus.
func (d Decision) Recorded() bool { return d.Outcome != OutcomeIdle }

// ControlRecord renders the decision for the control bus.
func (d Decision) ControlRecord(agent string) (obs.ControlRecord, bool) {
	if !d.Recorded() {
		return obs.ControlRecord{}, false
	}
	class := obs.ActionObserve
	if d.Outcome == OutcomeAbort {
		class = obs.ActionEscalate
	}
	cond := fmt.Sprintf("gc cycle %s (trigger=%s units=%d mutations=%d)", d.Outcome, d.Trigger, d.Units, d.Mutations)
	if d.Reason != "" {
		cond += " reason=" + d.Reason
	}
	return obs.ControlRecord{ActionClass: class, TriggeringCondition: cond, PolicyRef: d.PolicyRef, ActingAgent: agent}, true
}
