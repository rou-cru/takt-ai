// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

// Package history keeps the execution history: the append-only, totally
// ordered record from which the execution DAG is derived by replay
// (PR-DAG-REP-1). The harness is its only writer and sequencer; the three
// semantic authors of PR-DAG-REP-5 never write here themselves.
package history

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	// modernc.org/sqlite registers the "sqlite" database/sql driver via init.
	_ "modernc.org/sqlite"
)

// History storage lives in the private state directory; FirstAttempt is the
// attempt identity of a work unit's first attempt.
const (
	// historyFile lives in the same private state directory as the VFS store:
	// the execution history is not hot telemetry (PR-DAG-REP-2).
	historyFile = "history.sqlite"
	// stateDirectoryMode keeps the execution history unreadable to other users.
	stateDirectoryMode os.FileMode = 0o700
	// stateFileMode keeps the store private even if the directory mode is relaxed.
	stateFileMode os.FileMode = 0o600
	// FirstAttempt is the attempt identity of a work unit's first attempt.
	FirstAttempt = "1"
)

const schema = `PRAGMA journal_mode=DELETE; PRAGMA synchronous=EXTRA;
 CREATE TABLE IF NOT EXISTS history (seq INTEGER PRIMARY KEY, data BLOB NOT NULL);
 CREATE TRIGGER IF NOT EXISTS history_no_update BEFORE UPDATE ON history BEGIN SELECT RAISE(ABORT,'append-only execution history'); END;
 CREATE TRIGGER IF NOT EXISTS history_no_delete BEFORE DELETE ON history BEGIN SELECT RAISE(ABORT,'append-only execution history'); END;`

// Author is the semantic author of an entry (PR-DAG-REP-5). It says whose act
// the entry records, never who physically appended it.
type Author string

// Authors are the closed set of semantic authors (PR-DAG-REP-5).
const (
	// AuthorOrchestrator authors commitment, dispatch, revision and consolidation.
	AuthorOrchestrator Author = "orchestrator"
	// AuthorHarness authors admission and denial dispositions, observed starts,
	// effective terminations, suspension and reconciliation.
	AuthorHarness Author = "harness"
	// AuthorVerification authors gate verdicts and acceptance results.
	AuthorVerification Author = "verification"
)

// NodeKind identifies the source of a projected work unit or non-unit activity.
// It is descriptive metadata only: it does not grant authority or scheduling.
type NodeKind string

const (
	// NodeKindDelegated is work actually delegated through the ordinary dispatch
	// path. GC work belongs here only when it was genuinely delegated.
	NodeKindDelegated NodeKind = "delegated"
	// NodeKindOrchestrator is non-unit activity performed directly by the orchestrator.
	NodeKindOrchestrator NodeKind = "orchestrator"
	// NodeKindMaintenance is non-unit GC or other harness-owned maintenance activity.
	NodeKindMaintenance NodeKind = "maintenance"
)

// Kind is the recorded act. Kinds carry no authority: they describe lifecycle
// facts, never dispatch eligibility.
type Kind string

// Kinds are the closed set of recorded acts.
const (
	// KindPlanned declares committed work that has not been admitted.
	KindPlanned Kind = "planned"
	// KindWithdrawn revokes still-planned work; identity and history remain.
	KindWithdrawn Kind = "withdrawn"
	// KindAdmitted reserves capacity for work pending launch.
	KindAdmitted Kind = "admitted"
	// KindDenied records a refused request: visible, with no work-state change.
	KindDenied Kind = "denied"
	// KindLaunched records execution observed running.
	KindLaunched Kind = "launched"
	// KindUncertain records launch or liveness that could not be established.
	KindUncertain Kind = "launch_uncertain"
	// KindSuspended records execution held, typically on a VFS collision.
	KindSuspended Kind = "suspended"
	// KindCancelRequested records a cancellation that is not yet effective
	// termination: capacity and ownership remain reserved (PR-HAR-18).
	KindCancelRequested Kind = "cancellation_requested"
	// KindTerminated records effective termination and carries the outcome.
	KindTerminated Kind = "terminated"
	// KindContested records a contest of an identified terminal failure; it
	// changes no work state (PR-DAG-TMP-7).
	KindContested Kind = "contest_requested"
	// KindRecoveryDeclared declares bounded recovery of an objective before the
	// uncertain work begins (PR-DAG-MUT-9).
	KindRecoveryDeclared Kind = "recovery_declared"
	// KindRecoveryClosed closes a declared recovery, carrying whether its
	// result was demonstrated.
	KindRecoveryClosed Kind = "recovery_closed"
	// KindActions records observed action consumption of a recovery scope and
	// the bus position through which it is established (PR-OBS-PRG-2).
	KindActions Kind = "actions_observed"
	// KindActionsUncertain records that consumption could not be established
	// after a failure, so budgeted admission waits instead of starting fresh.
	KindActionsUncertain Kind = "actions_uncertain"
	// KindRestored records confirmed restoration of an abandoned recovery
	// scope's virtual state: a fact of its own, later than abandonment and
	// termination (PR-DAG-TMP-5, PR-VFS-STG-6).
	KindRestored Kind = "restoration_confirmed"
	// KindStopped records a declaration that delegation stops; it reopens no
	// allowance (PR-HAR-19).
	KindStopped Kind = "stop_declared"
	// KindEscalated records an escalation request, which is not a user
	// decision (PR-HAR-19, PR-ORQ-20).
	KindEscalated Kind = "escalation_requested"
	// KindException records the scoped user exception that enables further work
	// beyond a reached bound (PR-HAR-22).
	KindException Kind = "user_exception"
	// KindInterlocutorSwitched records a new temporary interlocutor holder for a
	// root session (IR-14..19); WorkUnitID names the child session, Agent the
	// specialist, Artifact the standard artifact declared for it (PR-HAR-24).
	KindInterlocutorSwitched Kind = "interlocutor_switched"
	// KindInterlocutorHandoff records the temporary holder returning the
	// interface to the base; Result carries the envelope's IR-23 value.
	KindInterlocutorHandoff Kind = "interlocutor_handoff"
	// KindInterlocutorAborted records the harness or user ending the temporary
	// holder's turn without negotiation (IR-24); Result is always "Aborted".
	KindInterlocutorAborted Kind = "interlocutor_aborted"
	// KindRevised records a valid plan revision against its expected prior
	// version; Classification is the harness-derived tactical/strategic label
	// (PR-DAG-MUT-4, PR-DAG-MUT-6).
	KindRevised Kind = "revised"
	// KindInvalidRevision records a rejected plan revision and its reason,
	// leaving the last valid plan untouched (PR-DAG-MUT-6).
	KindInvalidRevision Kind = "invalid_revision"
	// KindActivityStarted/Finished record direct orchestrator activity outside
	// the work-unit lifecycle and without a WorkUnitID.
	KindActivityStarted  Kind = "activity_started"
	KindActivityFinished Kind = "activity_finished"
	// KindMaintenanceStarted/Finished record harness-owned maintenance activity
	// such as a GC cycle, separate from delegated work units.
	KindMaintenanceStarted  Kind = "maintenance_started"
	KindMaintenanceFinished Kind = "maintenance_finished"
)

// Classification is a plan revision's derived tactical/strategic label
// (PR-DAG-MUT-4). The harness computes it from the compared plans; it is
// never accepted as a caller-supplied value.
const (
	// ClassificationTactical is a revision that changes no retained unit's
	// prerequisites: additions, withdrawals without reconnection, or
	// contract-only changes to still-planned work.
	ClassificationTactical = "tactical"
	// ClassificationStrategic is a revision that changes the prerequisite set
	// of any preexisting unit retained in the plan.
	ClassificationStrategic = "strategic"
)

// authorOf names the single semantic author of each kind (PR-DAG-REP-5) and
// enumerates every valid kind for validation.
var authorOf = map[Kind]Author{
	KindPlanned:              AuthorOrchestrator,
	KindWithdrawn:            AuthorOrchestrator,
	KindCancelRequested:      AuthorOrchestrator,
	KindContested:            AuthorOrchestrator,
	KindRecoveryDeclared:     AuthorOrchestrator,
	KindStopped:              AuthorOrchestrator,
	KindEscalated:            AuthorOrchestrator,
	KindInterlocutorSwitched: AuthorOrchestrator,
	KindInterlocutorHandoff:  AuthorOrchestrator,
	KindRevised:              AuthorOrchestrator,
	KindInvalidRevision:      AuthorOrchestrator,
	KindActivityStarted:      AuthorOrchestrator,
	KindActivityFinished:     AuthorOrchestrator,
	KindAdmitted:             AuthorHarness,
	KindDenied:               AuthorHarness,
	KindLaunched:             AuthorHarness,
	KindUncertain:            AuthorHarness,
	KindSuspended:            AuthorHarness,
	KindTerminated:           AuthorHarness,
	KindRecoveryClosed:       AuthorHarness,
	KindException:            AuthorHarness,
	KindActions:              AuthorHarness,
	KindActionsUncertain:     AuthorHarness,
	KindRestored:             AuthorHarness,
	KindInterlocutorAborted:  AuthorHarness,
	KindMaintenanceStarted:   AuthorHarness,
	KindMaintenanceFinished:  AuthorHarness,
}

// AuthorOf returns the semantic author a kind must carry.
func AuthorOf(k Kind) Author { return authorOf[k] }

// Causal status of PR-DAG-REP-3 when no cause is referenced. A missing
// reference is recorded as uncaptured, never silently read as CauseNone.
const (
	// CauseNone is an explicit declaration that no cause exists beyond the plan.
	CauseNone = "none"
	// CauseUncaptured records that the cause was not captured.
	CauseUncaptured = "uncaptured"
	// CauseInterlocutorRole records a switch denied because the specialist's
	// role is ineligible to hold the interface.
	CauseInterlocutorRole = "interlocutor/role-ineligible"
	// CauseInterlocutorHeld records a switch denied because the interface is
	// already held by another temporary holder.
	CauseInterlocutorHeld = "interlocutor/already-held"
	// CauseInterlocutorOrigin records a switch denied because the root session
	// named is not a valid origin for a switch.
	CauseInterlocutorOrigin = "interlocutor/origin-invalid"
	// CauseInterlocutorHolder records a handoff denied because the caller is
	// not the current holder of the interface.
	CauseInterlocutorHolder = "interlocutor/not-current-holder"
)

// Entry is one recorded act. Its Seq is assigned by Append: the harness is the
// sole sequencer. Entries carry references, never copies of referenced content.
type Entry struct {
	// Seq is the entry's position in the total order, starting at 0.
	Seq int `json:"seq"`
	// Author is the semantic author of the recorded act.
	Author Author `json:"author"`
	// Kind is the recorded act.
	Kind Kind `json:"kind"`
	// SessionID scopes either a unit or a non-unit activity. WorkUnitID and
	// AttemptID are present only for work-unit lifecycle entries.
	SessionID  string `json:"session_id"`
	WorkUnitID string `json:"work_unit_id,omitempty"`
	AttemptID  string `json:"attempt_id,omitempty"`
	// ActivityID is the real stable identity of a direct or maintenance activity;
	// it is never copied into WorkUnitID.
	ActivityID string `json:"activity_id,omitempty"`
	// NodeKind classifies a work unit or activity. Work-unit entries may be
	// delegated only; direct and maintenance values belong to activity records.
	NodeKind NodeKind `json:"node_kind,omitempty"`
	// Dispatch identifies the host delegation an admission came from, so a
	// transport repetition of that delegation is told apart from a deliberate
	// new attempt of the same unit (PR-HAR-17).
	Dispatch string `json:"dispatch,omitempty"`
	// Agent is the acting specialist identity, distinct from the authority
	// behind the act.
	Agent string `json:"agent,omitempty"`
	// Cause is a referenced cause, CauseNone or CauseUncaptured; never prose.
	Cause string `json:"cause"`
	// JournalRef is the Action Journal position the entry was recorded against
	// (`journal/<seq>`, from vfs.JournalEntry.Ref), a reference and never a copy.
	JournalRef string `json:"journal_ref,omitempty"`
	// Outcome is the prevailing outcome; terminal entries only.
	Outcome Outcome `json:"outcome,omitempty"`
	// PolicyRef names the policy that governed the act, so a recorded prefix
	// stays interpretable without applying newer rules.
	PolicyRef string `json:"policy_ref,omitempty"`
	// Objective identifies a declared recovery's objective, so its failure
	// history survives renaming the work (PR-HAR-21, PR-HAR-22). On an
	// exception it narrows the scope the allowance applies to.
	Objective string `json:"objective,omitempty"`
	// Bound and Allowance are the limit a user exception scopes and the finite
	// additional allowance it grants (PR-HAR-22).
	Bound     string `json:"bound,omitempty"`
	Allowance int    `json:"allowance,omitempty"`
	// Result, Point and Scope are a recovery declaration's binary expected
	// result, the prior recoverable point it may be restored to, and the explicit
	// work scope both budgets are measured over. An absent declaration stays
	// empty here: it is recorded as missing, never defaulted (PR-DAG-MUT-9).
	Result string   `json:"result,omitempty"`
	Point  string   `json:"point,omitempty"`
	Scope  []string `json:"scope,omitempty"`
	// Artifact is the workspace-relative path a switch declares its temporary
	// holder must produce before handoff (PR-HAR-24); set only on
	// KindInterlocutorSwitched, fixed at switch time, never renegotiated.
	Artifact string `json:"artifact,omitempty"`
	// Actions and Attempts are the two declared allowances of PR-ORQ-13. On an
	// action observation Actions is the observed increment instead.
	Actions  int `json:"actions,omitempty"`
	Attempts int `json:"attempts,omitempty"`
	// Evidence links what demonstrated a closed recovery's result, or the state
	// reference a restoration confirmation produced. It is never prose standing
	// in for absent evidence.
	Evidence string `json:"evidence,omitempty"`
	// PlanVersion, Contract and Prerequisites are the identified baseline
	// version, the unit's contract, and its explicit prerequisite identities,
	// recorded by a plan commitment (PR-DAG-MUT-1).
	PlanVersion   string   `json:"plan_version,omitempty"`
	Contract      string   `json:"contract,omitempty"`
	Prerequisites []string `json:"prerequisites,omitempty"`
	// BaseVersion is the plan version a revision expects still to be current;
	// PlanVersion carries the revision's own resulting version, and the
	// per-unit entries it produces (PR-DAG-MUT-6).
	BaseVersion string `json:"base_version,omitempty"`
	// Classification is a revision's derived tactical/strategic label
	// (PR-DAG-MUT-4).
	Classification string `json:"classification,omitempty"`
	// Reason records why an invalid plan revision was rejected, so the
	// declaration stays legible without needing the last valid plan changed
	// to explain it (PR-DAG-MUT-6).
	Reason string `json:"reason,omitempty"`
}

// History is the append-only store. All access is serialized by the workspace
// lock the caller already holds through vfs.Open.
type History struct {
	db      *sql.DB
	entries []Entry
}

// Open opens the execution history under the private state directory, creating
// it if absent. A corrupt or incomplete record is an error, never a reset.
func Open(stateDir string) (*History, error) {
	if err := os.MkdirAll(stateDir, stateDirectoryMode); err != nil {
		return nil, err
	}
	// The VFS owns resolution and privacy checks of this shared state
	// directory; opening 0600 is enough here.
	path := filepath.Join(stateDir, historyFile)
	file, err := os.OpenFile(path, os.O_CREATE, stateFileMode)
	if err != nil {
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	h := &History{db: db}
	if _, err = db.Exec(schema); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	if err = h.replay(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return h, nil
}

// Close releases the store handle.
func (h *History) Close() error { return h.db.Close() }

// replay reads the whole recorded order, rejecting a gap or a short read.
func (h *History) replay() (err error) {
	rows, err := h.db.Query("SELECT data FROM history ORDER BY seq")
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return err
		}
		var e Entry
		if err = json.Unmarshal(raw, &e); err != nil {
			return err
		}
		if e.Seq != len(h.entries) {
			return fmt.Errorf("history: sequence corrupt")
		}
		h.entries = append(h.entries, e)
	}
	return rows.Err()
}

// Entries returns the recorded prefix in its total order.
func (h *History) Entries() []Entry { return h.entries }

// Project derives the current projection from the whole recorded prefix.
func (h *History) Project() Projection { return Project(h.entries) }

// Append assigns the next position and durably records the entry. A rejected
// entry is not recorded: an invalid state never becomes representable here.
func (h *History) Append(e Entry) error {
	e.Seq = len(h.entries)
	if err := e.validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if _, err = h.db.Exec("INSERT INTO history(seq,data) VALUES(?,?)", e.Seq, raw); err != nil {
		return err
	}
	h.entries = append(h.entries, e)
	return nil
}

func (e Entry) validate() error {
	isActivity, err := e.validateIdentity()
	if err != nil {
		return err
	}
	if err := e.validateNodeKind(isActivity); err != nil {
		return err
	}
	if err := e.validateOutcomeShape(); err != nil {
		return err
	}
	if err := e.validateKindFields(); err != nil {
		return err
	}
	switch e.Outcome {
	case "", OutcomeCompleted, OutcomeFailed, OutcomeBacktracked, OutcomeInterrupted:
		return nil
	}
	return fmt.Errorf("history: unknown outcome %q", e.Outcome)
}

// validateIdentity checks authorship, session, activity-or-work identity and
// causal status, and reports whether the entry is an activity record.
func (e Entry) validateIdentity() (bool, error) {
	author, ok := authorOf[e.Kind]
	if !ok {
		return false, fmt.Errorf("history: unknown entry kind %q", e.Kind)
	}
	if e.Author != author {
		return false, fmt.Errorf("history: %q is authored by the %s, not by %q", e.Kind, author, e.Author)
	}
	if e.SessionID == "" {
		return false, errors.New("history: session identity required")
	}
	activityKind, isActivity := activityKindOf(e.Kind)
	if isActivity {
		if e.ActivityID == "" || e.WorkUnitID != "" || e.AttemptID != "" {
			return false, errors.New("history: activity records require activity_id and must not carry work_unit_id or attempt_id")
		}
		if e.NodeKind != activityKind {
			return false, fmt.Errorf("history: %q requires node kind %q", e.Kind, activityKind)
		}
	} else if e.WorkUnitID == "" || e.AttemptID == "" || e.ActivityID != "" {
		return false, errors.New("history: work entries require work-unit and attempt identity, not activity_id")
	}
	if e.Cause == "" {
		return false, errors.New("history: causal status required")
	}
	return isActivity, nil
}

func (e Entry) validateNodeKind(isActivity bool) error {
	switch e.NodeKind {
	case "", NodeKindDelegated, NodeKindOrchestrator, NodeKindMaintenance:
	default:
		return fmt.Errorf("history: unknown node kind %q", e.NodeKind)
	}
	if !isActivity && e.NodeKind != "" && e.NodeKind != NodeKindDelegated {
		return errors.New("history: work-unit entries require node kind delegated")
	}
	return nil
}

func (e Entry) validateOutcomeShape() error {
	terminal := e.Kind == KindTerminated || e.Kind == KindRecoveryClosed || isActivityFinished(e.Kind)
	if terminal != (e.Outcome != "") {
		return errors.New("history: only terminal and activity-finished entries carry an outcome")
	}
	if isActivityFinished(e.Kind) && e.Outcome != OutcomeCompleted && e.Outcome != OutcomeFailed && e.Outcome != OutcomeInterrupted {
		return fmt.Errorf("history: activity finish has invalid outcome %q", e.Outcome)
	}
	return nil
}

// validateKindFields checks the fields specific to each entry kind.
func (e Entry) validateKindFields() error {
	switch e.Kind {
	case KindRecoveryDeclared, KindRecoveryClosed, KindActions, KindActionsUncertain, KindRestored:
		if e.Objective == "" {
			return errors.New("history: recovery requires an objective identity")
		}
	case KindException:
		// An exception enables further work: an unknown bound or an open-ended
		// allowance would enable more than the user granted.
		if !bounds[e.Bound] || e.Allowance <= 0 {
			return errors.New("history: a user exception names a known bound and a finite allowance")
		}
	case KindRevised:
		if e.BaseVersion == "" || e.PlanVersion == "" {
			return errors.New("history: a revision names its expected base version and its resulting version")
		}
		if e.Classification != ClassificationTactical && e.Classification != ClassificationStrategic {
			return fmt.Errorf("history: unknown revision classification %q", e.Classification)
		}
	case KindInvalidRevision:
		if e.Reason == "" {
			return errors.New("history: an invalid revision records its reason")
		}
	}
	return nil
}

// activityKindOf names the source classification encoded by an activity event.
func activityKindOf(k Kind) (NodeKind, bool) {
	switch k {
	case KindActivityStarted, KindActivityFinished:
		return NodeKindOrchestrator, true
	case KindMaintenanceStarted, KindMaintenanceFinished:
		return NodeKindMaintenance, true
	default:
		return "", false
	}
}

func isActivityStarted(k Kind) bool {
	return k == KindActivityStarted || k == KindMaintenanceStarted
}

func isActivityFinished(k Kind) bool {
	return k == KindActivityFinished || k == KindMaintenanceFinished
}
