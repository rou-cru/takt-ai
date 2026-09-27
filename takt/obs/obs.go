// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

// Package obs keeps one local event stream and control record so sessions stay observable without network or memory coupling.
package obs

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

// ─── Envelope ────────────────────────────────────────────────────────────────

// SchemaVersion pins the envelope shape so old and new readers never misread events.
const SchemaVersion = "2.0.0"

// SourcePlane names where an event came from so mixed signals stay separable.
type SourcePlane string

// Source planes are the closed set of event origins.
const (
	// PlaneVFS marks file-system signals, separate so storage activity never looks like chat.
	PlaneVFS SourcePlane = "takt.vfs"
	// PlaneOrchestration marks dispatch and unit transitions, separate so scheduling never looks like storage.
	PlaneOrchestration SourcePlane = "takt.orchestration"
	// PlanePlatform marks platform-native tool activity, separate so runtime signals stay distinguishable from Takt-native ones.
	PlanePlatform SourcePlane = "takt.platform"
	// PlaneLifecycle marks install and garbage-collection findings, separate so maintenance never looks like execution.
	PlaneLifecycle SourcePlane = "takt.lifecycle"
	// PlaneHarness marks harness-derived measurements, separate so agent claims never mix with deterministic signals.
	PlaneHarness SourcePlane = "takt.harness"
	// PlaneControl marks the control plane's own effects, so post-action state is observable apart from what triggered it.
	PlaneControl SourcePlane = "takt.control"
)

// EventClass names what an event means so consumers can filter without parsing payloads.
type EventClass string

// Event classes are the closed set of meanings; the control classes record
// actions taken, one class per action.
const (
	// EventVFSDelta marks file staging actions, so file progress stays visible content-free.
	EventVFSDelta EventClass = "vfs_delta"
	// EventCollision marks write conflicts, so competing agents stay visible to the scheduler.
	EventCollision EventClass = "vfs_collision"
	// EventDispatch marks dispatch decisions, so scheduling progress stays visible content-free.
	EventDispatch EventClass = "dispatch"
	// EventUnitLifecycle marks work-unit transitions, so progress is measurable without reading reports.
	EventUnitLifecycle EventClass = "unit_lifecycle"
	// EventToolActivity marks platform tool use, so liveness is observable without tool arguments.
	EventToolActivity EventClass = "tool_activity"
	// EventModelUsage records provider-reported token and cost usage without model content.
	EventModelUsage EventClass = "model_usage"
	// EventGCFindingUnapplied marks a finding left unapplied, so lingering artifacts stay visible.
	EventGCFindingUnapplied EventClass = "gc_finding_unapplied"
	// EventProblemRate marks a harness-derived problem rate, so stability is measured from deterministic signals.
	EventProblemRate EventClass = "problem_rate"
	// EventCycleReedit marks a file edited again after a cycle consolidated it, so the reversion stays attributed.
	EventCycleReedit EventClass = "cycle_reedit"
	// EventControlThrottle marks a THROTTLE action taken, so its effect reaches later evaluation.
	EventControlThrottle EventClass = "control_throttle"
	// EventControlContain marks a CONTAIN action taken, so its effect reaches later evaluation.
	EventControlContain EventClass = "control_contain"
	// EventControlRollback marks a ROLLBACK action taken, so its effect reaches later evaluation.
	EventControlRollback EventClass = "control_rollback"
	// EventControlGate marks a GATE action taken, so its effect reaches later evaluation.
	EventControlGate EventClass = "control_gate"
	// EventControlEscalate marks an ESCALATE action taken, so its effect reaches later evaluation.
	EventControlEscalate EventClass = "control_escalate"
)

// allowedAttributes lists the only attribute keys each new class may carry so nested or unexpected content
// cannot ride along; classes absent here keep the forbidden-key check only.
var allowedAttributes = map[EventClass][]string{
	EventDispatch:           {"decision", "specialist", "dispatch_id", "reason_code", "parallelism"},
	EventUnitLifecycle:      {"transition", "from_state", "to_state", "specialist", "reason_code"},
	EventToolActivity:       {"platform", "tool", "phase", "outcome", "duration_ms", "path", "path_hash"},
	EventModelUsage:         {"provider", "model", "message_id", "input_tokens", "output_tokens", "reasoning_tokens", "cache_read_tokens", "cache_write_tokens", "cost_usd"},
	EventGCFindingUnapplied: {"finding_kind", "target", "count", "cycle_id", "mandate_class"},
	EventProblemRate:        {"signal", "count", "window_seconds", "rate"},
	EventCycleReedit:        {"cycle_id", "mandate_class", "path_hash"},
	EventControlThrottle:    {"policy_ref", "triggering_condition"},
	EventControlContain:     {"policy_ref", "triggering_condition"},
	EventControlRollback:    {"policy_ref", "triggering_condition"},
	EventControlGate:        {"policy_ref", "triggering_condition"},
	EventControlEscalate:    {"policy_ref", "triggering_condition"},
}

// CorrelationIDs links one event to its session and work so scattered signals stay joinable.
type CorrelationIDs struct {
	// SessionID links the event to its run, so multi-session logs never mix.
	SessionID string `json:"session_id,omitempty"`
	// WorkUnitID links the event to its task, so per-task progress stays separable.
	WorkUnitID string `json:"work_unit_id,omitempty"`
	// AttemptID links the event to its attempt, so retries of one unit stay separable.
	AttemptID string `json:"attempt_id,omitempty"`
	// JournalEntryRef points at storage evidence without copying it, so envelopes stay content-free.
	JournalEntryRef string `json:"journal_entry_ref,omitempty"`
}

// Envelope carries one normalized event without content so telemetry stays safe to keep and share.
type Envelope struct {
	// SchemaVersion tags the shape, so readers can reject unknown versions fast.
	SchemaVersion string `json:"schema_version"`
	// SessionTimestamp holds session-clock time, so budgets use monotonic time immune to wall-clock jumps.
	SessionTimestamp SessionTime `json:"session_timestamp"`
	// WallTimestamp holds wall-clock time, so cross-system correlation stays possible.
	WallTimestamp time.Time `json:"wall_timestamp"`
	// Source names the origin plane, so mixed signals stay separable.
	Source SourcePlane `json:"source"`
	// Authority names the actor, so every event has a clear owner.
	Authority string `json:"authority"`
	// EventClass names the meaning, so filtering needs no payload parsing.
	EventClass EventClass `json:"event_class"`
	// Correlations links session and work, so queries can join across the stream.
	Correlations CorrelationIDs `json:"correlations"`
	// Attributes holds redacted details, content-free so secrets never land on the bus.
	Attributes map[string]any `json:"attributes,omitempty"`
}

// ErrContentForbidden fails validation when content slips in, so leaks surface immediately.
var ErrContentForbidden = errors.New("obs: envelope must not carry file contents, prompts, responses, or secrets")

// forbiddenAttributeKeys blocks risky keys as defense-in-depth so one missed caller cannot leak content.
var forbiddenAttributeKeys = map[string]bool{
	"content":      true,
	"file_content": true,
	"prompt":       true,
	"response":     true,
	"secret":       true,
	"credential":   true,
	"token":        true,
	"password":     true,
	"api_key":      true,
}

// Validate rejects content leaks and missing owners so bad events fail before reaching the stream.
func (e *Envelope) Validate() error {
	if e.SchemaVersion == "" {
		return errors.New("obs: envelope SchemaVersion is required")
	}
	if e.Authority == "" {
		return errors.New("obs: envelope Authority is required")
	}
	for k := range e.Attributes {
		if forbiddenAttributeKeys[k] {
			return fmt.Errorf("%w: attribute %q is forbidden", ErrContentForbidden, k)
		}
	}
	if allowed, ok := allowedAttributes[e.EventClass]; ok {
		for k, v := range e.Attributes {
			if !slices.Contains(allowed, k) {
				return fmt.Errorf("obs: attribute %q is not allowed for class %q", k, e.EventClass)
			}
			// Scalars only: a nested value would smuggle content past the key check.
			switch v.(type) {
			case string, bool, int, int32, int64, uint, uint32, uint64, float32, float64:
			default:
				return fmt.Errorf("obs: attribute %q of class %q must be a scalar", k, e.EventClass)
			}
		}
	}
	return nil
}

// NewEnvelope builds a pre-stamped envelope so callers cannot forget versions or timestamps.
func NewEnvelope(clock *Clock, source SourcePlane, agent string) Envelope {
	return Envelope{
		SchemaVersion:    SchemaVersion,
		SessionTimestamp: clock.Now(),
		WallTimestamp:    time.Now(),
		Source:           source,
		Authority:        agent,
	}
}

// ─── Session Clock ───────────────────────────────────────────────────────────

// SessionTime counts time since session start so budgets survive wall-clock jumps.
type SessionTime = time.Duration

// Clock owns monotonic session time so budgets never break on clock adjustments.
type Clock struct {
	// start anchors the session, immutable so reads need no lock.
	start time.Time
}

// NewClock starts a session clock so all later budgets share one origin.
func NewClock() *Clock {
	return &Clock{start: time.Now()}
}

// Now reads elapsed session time so callers share one consistent clock.
func (c *Clock) Now() SessionTime {
	return time.Since(c.start)
}

// ─── Control Actions ─────────────────────────────────────────────────────────

// ActionClass names one control response ordered by intrusiveness so escalation stays predictable.
type ActionClass string

// Ordered by intrusion, least to most, per PR-OBS-CTL-1: OBSERVE, THROTTLE,
// CONTAIN, ROLLBACK, GATE, ESCALATE. THROTTLE/CONTAIN/ROLLBACK's decision
// logic belongs to takt/gc, not this package; here they are only named and
// validated so the taxonomy is complete and the effect event below is ready
// for that package to call.
const (
	// ActionObserve keeps the default watch posture, needed so normal events cost nothing.
	ActionObserve ActionClass = "OBSERVE"
	// ActionThrottle slows an agent's pace, so pressure eases before it needs containment.
	ActionThrottle ActionClass = "THROTTLE"
	// ActionContain narrows an agent's scope, so a misbehaving agent can't widen the blast radius.
	ActionContain ActionClass = "CONTAIN"
	// ActionRollback reverts staged VFS deltas only — never the real filesystem (PR-OBS-CTL-3).
	ActionRollback ActionClass = "ROLLBACK"
	// ActionGate holds work for approval, needed so budget or permission widening never happens unattended.
	ActionGate ActionClass = "GATE"
	// ActionEscalate hands an unresolved condition to the interface holder, so course problems reach a human.
	ActionEscalate ActionClass = "ESCALATE"
)

// controlEffectEvents maps each intrusive ActionClass to the EventClass
// PublishControlEffectEvent emits for it, so every action beyond OBSERVE
// produces the typed event PR-OBS-CTL-5 requires.
var controlEffectEvents = map[ActionClass]EventClass{
	ActionThrottle: EventControlThrottle,
	ActionContain:  EventControlContain,
	ActionRollback: EventControlRollback,
	ActionGate:     EventControlGate,
	ActionEscalate: EventControlEscalate,
}

// PolicyObserve is the policy reference of the automatic watch record, so every action names a policy.
const PolicyObserve = "bus.observe"

// ControlRecord keeps one decision with its reason so later review can audit what happened.
type ControlRecord struct {
	// ActionClass names the decision, so audits can filter by response type.
	ActionClass ActionClass `json:"action_class"`
	// TriggeringCondition explains why, so decisions stay explainable.
	TriggeringCondition string `json:"triggering_condition"`
	// PolicyRef names the policy that selected the action, so decisions trace back to their rule.
	PolicyRef string `json:"policy_ref"`
	// ActingAgent names who was involved, so responsibility stays clear.
	ActingAgent string `json:"acting_agent"`
	// Correlations links session, work and journal entry, so decisions stay joinable to events and file operations.
	Correlations CorrelationIDs `json:"correlations"`
	// Timestamp marks session time, so ordering stays reproducible.
	Timestamp SessionTime `json:"timestamp"`
}

// Validate rejects incomplete decisions so every recorded action stays auditable.
func (r *ControlRecord) Validate() error {
	switch r.ActionClass {
	case ActionObserve, ActionThrottle, ActionContain, ActionRollback, ActionGate, ActionEscalate:
	default:
		return fmt.Errorf("obs: unknown action class %q", r.ActionClass)
	}
	if r.TriggeringCondition == "" || r.PolicyRef == "" || r.ActingAgent == "" {
		return errors.New("obs: control record needs TriggeringCondition, PolicyRef and ActingAgent")
	}
	return nil
}

// ─── Internal Control Bus ─────────────────────────────────────────────────────

// Bus keeps one ordered stream plus decisions so control stays deterministic and local.
type Bus struct {
	// mu guards the store and the degraded posture, so concurrent sensors cannot reorder events.
	mu sync.Mutex

	// clock stamps events, shared so events and decisions use one time.
	clock *Clock

	// store persists what the bus publishes, so signals outlive the process; nil keeps the bus memory-only.
	store *Store

	// sessionID ties events together, so multi-session logs never mix.
	sessionID string

	// degraded flags control-plane outage, so callers can fall back safely.
	degraded bool

	// degradedReason explains the outage, so fallback stays explainable.
	degradedReason string
}

// NewBus creates one bus per session so events never leak across runs.
func NewBus(sessionID string, clock *Clock) *Bus {
	return &Bus{
		clock:     clock,
		sessionID: sessionID,
	}
}

// AttachStore persists everything published from now on, so a bus outlives its process.
func (b *Bus) AttachStore(s *Store) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.store = s
}

// Publish appends one validated event plus its watch record so nothing enters silently.
func (b *Bus) Publish(e Envelope) error {
	if err := e.Validate(); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.publishLocked(e)
}

// publishLocked writes the event and its watch record so nothing enters without an audit trail.
func (b *Bus) publishLocked(e Envelope) error {
	if e.Correlations.SessionID == "" {
		e.Correlations.SessionID = b.sessionID
	}
	watch := ControlRecord{
		ActionClass:         ActionObserve,
		TriggeringCondition: string(e.EventClass),
		PolicyRef:           PolicyObserve,
		ActingAgent:         e.Authority,
		Correlations:        e.Correlations,
		Timestamp:           b.clock.Now(),
	}
	if b.store != nil {
		if _, err := b.store.AppendEvent(e); err != nil {
			return err
		}
		if _, err := b.store.AppendAction(watch); err != nil {
			return err
		}
	}
	return nil
}

// Degrade marks the control plane as unavailable. The fallback posture is the
// static deterministic budgets defined elsewhere.
func (b *Bus) Degrade(reason string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.degraded = true
	b.degradedReason = reason
}

// SessionID returns the session identifier for this bus instance.
func (b *Bus) SessionID() string { return b.sessionID }

// ─── Convenience: publish a Takt-native signal ───────────────────────────────

// PublishVFSEvent constructs and publishes an envelope referencing a VFS
// journal entry by ref, describing it through attributes without content.
func PublishVFSEvent(bus *Bus, clock *Clock, agent, journalRef, workUnitID string, attributes map[string]any) error {
	env := NewEnvelope(clock, PlaneVFS, agent)
	env.EventClass = EventVFSDelta
	env.Correlations = CorrelationIDs{
		SessionID:       bus.SessionID(),
		WorkUnitID:      workUnitID,
		JournalEntryRef: journalRef,
	}
	env.Attributes = attributes
	return bus.Publish(env)
}

// PublishCollisionEvent publishes a cross-agent write collision intercepted
// by the VFS.
func PublishCollisionEvent(bus *Bus, clock *Clock, attemptingAgent, owningAgent, path, workUnitID string) error {
	env := NewEnvelope(clock, PlaneVFS, attemptingAgent)
	env.EventClass = EventCollision
	env.Correlations = CorrelationIDs{
		SessionID:  bus.SessionID(),
		WorkUnitID: workUnitID,
	}
	env.Attributes = map[string]any{
		"owning_agent": owningAgent,
		"path":         path,
	}
	return bus.Publish(env)
}

// PublishCycleReeditEvent publishes the event PR-MNT-31 requires when a path
// a maintenance cycle already consolidated gets edited again: it attributes
// the reversion back to that cycle and its mandate class instead of letting
// it look like an unattributed edit.
func PublishCycleReeditEvent(bus *Bus, clock *Clock, agent, cycleID, mandateClass, pathHash, workUnitID string) error {
	env := NewEnvelope(clock, PlaneVFS, agent)
	env.EventClass = EventCycleReedit
	env.Correlations = CorrelationIDs{
		SessionID:  bus.SessionID(),
		WorkUnitID: workUnitID,
	}
	env.Attributes = map[string]any{
		"cycle_id":      cycleID,
		"mandate_class": mandateClass,
		"path_hash":     pathHash,
	}
	return bus.Publish(env)
}

// PublishControlEffectEvent publishes the typed event PR-OBS-CTL-5 requires
// for every control action beyond OBSERVE: THROTTLE, CONTAIN, ROLLBACK, GATE
// or ESCALATE. Deciding *when* to throttle, contain or roll back belongs to
// takt/gc; this only makes the chosen action's effect observable on the bus.
func PublishControlEffectEvent(bus *Bus, clock *Clock, action ActionClass, agent, policyRef, triggeringCondition, workUnitID string) error {
	class, ok := controlEffectEvents[action]
	if !ok {
		return fmt.Errorf("obs: %q has no control effect event", action)
	}
	env := NewEnvelope(clock, PlaneControl, agent)
	env.EventClass = class
	env.Correlations = CorrelationIDs{
		SessionID:  bus.SessionID(),
		WorkUnitID: workUnitID,
	}
	env.Attributes = map[string]any{
		"policy_ref":           policyRef,
		"triggering_condition": triggeringCondition,
	}
	return bus.Publish(env)
}
