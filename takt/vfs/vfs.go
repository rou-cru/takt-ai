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

// Package vfs implements the Virtual File System, the transactional execution layer for agent file operations.
package vfs

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/rou-cru/takt-ai/takt/model"
)

// OperationType classifies a VFS journal entry.
type OperationType string

// Operation types are the closed set of journal entries.
const (
	// OpRead records a read against the merged VFS view.
	OpRead OperationType = "read"
	// OpCreate records a new file creation in staging.
	OpCreate OperationType = "create"
	// OpPatch records an in-place content update in staging.
	OpPatch OperationType = "patch"
	// OpDelete records a staged deletion.
	OpDelete OperationType = "delete"
	// OpFlush records the physical consolidation of staged state.
	OpFlush OperationType = "flush"
	// OpRollback records the discard of a virtual transaction delta.
	OpRollback OperationType = "rollback"
)

// AgentID identifies the actor behind a VFS operation.
type AgentID string

// Sentinel errors returned by FS operations.
var (
	// ErrGitMutationDenied is returned when a non-orchestrator role attempts a
	// mutating Git command.
	ErrGitMutationDenied = errors.New("vfs: mutating git command denied for this role class")

	// ErrCollision is returned when an agent attempts to mutate a file owned by
	// another concurrently active agent.
	ErrCollision = errors.New("vfs: write collision — file is owned by another agent")

	// ErrConsolidationConflict is returned when consolidation is attempted while
	// unresolved collisions exist in the same session, unit and attempt.
	ErrConsolidationConflict = errors.New("vfs: consolidation blocked by unresolved collision")

	// ErrVerificationRequired is returned when consolidation is attempted without
	// a passing verification verdict.
	ErrVerificationRequired = errors.New("vfs: consolidation requires a passing verification verdict")

	// ErrSelfVerification is returned when the authoring agent attempts to
	// verify its own delta.
	ErrSelfVerification = errors.New("vfs: an agent cannot verify its own delta")

	// ErrFlushPartial is returned when consolidation fails mid-write. The staged
	// state is preserved for recovery.
	ErrFlushPartial = errors.New("vfs: consolidation flush failed — staged state preserved for recovery")

	// ErrUnknownCycle is returned when a cycle has no consolidated state
	// retained: it consolidated nothing, or it was already completed or
	// discarded.
	ErrUnknownCycle = errors.New("vfs: no consolidated state retained for this cycle")

	// ErrCycleDiverged is returned when a path no longer holds what the cycle
	// consolidated, so restoring it would overwrite a later write.
	ErrCycleDiverged = errors.New("vfs: workspace diverged from what the cycle consolidated — not restored")
)

// JournalEntry records a single VFS operation.
type JournalEntry struct {
	Role       model.RoleClass `json:"role,omitempty"`
	SessionID  string          `json:"session_id,omitempty"`
	WorkUnitID string          `json:"work_unit_id,omitempty"`
	AttemptID  string          `json:"attempt_id,omitempty"`
	// CycleID and MandateClass name the maintenance cycle and the mandate class
	// it declared; empty for entries outside maintenance and for entries
	// recorded before these fields existed.
	CycleID      string `json:"cycle_id,omitempty"`
	MandateClass string `json:"mandate_class,omitempty"`
	CallID       string `json:"call_id,omitempty"`
	Revision     uint64 `json:"revision,omitempty"`
	Outcome      string `json:"outcome,omitempty"`
	// Seq is the entry's position in the append-only journal, starting at 0.
	// It is the stable reference telemetry cites instead of copying the entry.
	Seq int `json:"seq"`
	// Timestamp is the wall-clock time the operation was recorded.
	Timestamp time.Time `json:"timestamp"`
	// Agent is the authoring agent identifier/role.
	Agent AgentID `json:"agent"`
	// Path is the target file path, relative to workspace root, slash-separated.
	Path string `json:"path"`
	// Operation is the operation type.
	Operation OperationType `json:"operation"`
	// BeforeHash is the SHA-256 hex of the file's content before the operation,
	// or empty string for creates.
	BeforeHash string `json:"before_hash,omitempty"`
	// AfterHash is the SHA-256 hex of the file's content after the operation,
	// or empty string for deletes or rollbacks.
	AfterHash string `json:"after_hash,omitempty"`
	// ReeditOfCycleID and ReeditOfMandateClass name the earlier maintenance
	// cycle (and its mandate class) that last consolidated Path, when this
	// entry writes over work that cycle already closed (PR-MNT-31). Both are
	// empty when Path was never consolidated. Computed at append time, under
	// the same lock that later stamps CycleID/MandateClass for this entry's
	// own cycle, so a journal handler never has to call back into the FS.
	ReeditOfCycleID      string `json:"reedit_of_cycle_id,omitempty"`
	ReeditOfMandateClass string `json:"reedit_of_mandate_class,omitempty"`
}

// Ref returns the entry's stable reference for telemetry.
func (e JournalEntry) Ref() string { return "journal/" + strconv.Itoa(e.Seq) }

// VerificationVerdict carries the gate verifier's result.
type VerificationVerdict struct {
	VerifierRole model.RoleClass
	Revision     uint64
	DeltaHash    string
	// Invariants is the applicable invariant set the delta was judged against,
	// and InvariantsHash its version at that moment (PR-VFS-CSL-7).
	Invariants     InvariantSet
	InvariantsHash string
	// Pass indicates the staged delta passed the reference invariants.
	Pass bool
	// Finding records the verifier's rationale. Required when Pass is false.
	Finding string
	// VerifierID identifies the verification specialist. Must differ from the
	// authoring agent.
	VerifierID AgentID
}

// CollisionEvent is emitted when the VFS intercepts a cross-agent write collision.
type CollisionEvent struct {
	SessionID  string `json:"session_id,omitempty"`
	AttemptID  string `json:"attempt_id,omitempty"`
	WorkUnitID string `json:"work_unit_id,omitempty"`
	// AttemptingAgent attempted the conflicting mutation.
	AttemptingAgent AgentID
	// OwningAgent currently holds exclusive ownership.
	OwningAgent AgentID
	// Path is the file the conflict was detected on.
	Path string
	// Timestamp is when the collision was detected.
	Timestamp time.Time
}

// CollisionError is a refused ownership request with the exact requested path
// and both dispatch identities. It is a denial observation, not unresolved
// in-flight work; callers can still use errors.Is(err, ErrCollision).
type CollisionError struct {
	RequestedPath  string  `json:"requested_path"`
	TargetAgent    AgentID `json:"target_agent"`
	TargetInstance string  `json:"target_instance"`
	TargetSession  string  `json:"target_session"`
	TargetUnit     string  `json:"target_unit"`
	OwnerAgent     AgentID `json:"owner_agent"`
	OwnerSession   string  `json:"owner_session"`
	OwnerUnit      string  `json:"owner_unit"`
}

// Error renders the refused path and both dispatch identities behind it.
func (e *CollisionError) Error() string {
	return fmt.Sprintf("%v: requested path %q for target %q (instance %q, session %q, unit %q) is owned by agent %q (session %q, unit %q); ask the orchestrator to release it",
		ErrCollision, e.RequestedPath, e.TargetAgent, e.TargetInstance, e.TargetSession, e.TargetUnit,
		e.OwnerAgent, e.OwnerSession, e.OwnerUnit)
}

// Unwrap preserves errors.Is(err, ErrCollision) for typed refusals.
func (e *CollisionError) Unwrap() error { return ErrCollision }

// OwnershipClaim is one binding's currently held scope. Pending means the
// orchestrator assigned it before the target bound; Active means it belongs to
// the requested current session and has been adopted by the target.
type OwnershipClaim struct {
	Key            AgentID  `json:"key"`
	AgentID        AgentID  `json:"agent_id"`
	TargetInstance string   `json:"target_instance"`
	RootSessionID  string   `json:"root_session_id"`
	WorkUnitID     string   `json:"work_unit_id"`
	Scope          []string `json:"scope"`
	// AuthorKey names the staged work a verifier gate judges; empty for authors.
	AuthorKey AgentID `json:"author_key,omitempty"`
	Active    bool    `json:"active"`
	Pending   bool    `json:"pending"`
}

// agentDelta tracks staged mutations and verification state for a single agent.
type agentDelta struct {
	// files maps relative slash-path → staged content. A nil value means the
	// path is staged for deletion; a staged empty file holds a non-nil empty
	// slice, so nil is never ambiguous.
	files    map[string][]byte
	revision uint64
	bases    map[string]baseFile
	// verdict holds the attached verification result, nil until set.
	verdict *VerificationVerdict
}

// FS governs all in-flight agent work for one session.
type FS struct {
	mu sync.RWMutex

	// rootDir is the physical workspace root (absolute).
	rootDir            string
	root               *os.Root
	db                 *sql.DB
	workspaceLock      *os.File
	fault              error
	persisted          int
	notifiedCollisions int
	bindings           map[AgentID]Identity
	calls              map[string]bool
	recovery           *recoveryManifest
	// cycles maps a maintenance cycle id to what its consolidations overwrote,
	// so the cycle stays discardable until it closes.
	cycles map[string]*cycleSnapshot
	// failpoint is test-only fault injection at durable flush boundaries.
	failpoint func(string) error

	// staged maps agentID → agentDelta.
	staged map[AgentID]*agentDelta

	// owners maps slash-path → owning agent (exclusive write ownership).
	owners map[string]AgentID

	// collisions holds unresolved collision events.
	collisions []CollisionEvent

	// journal is the append-only action journal.
	journal []JournalEntry

	// collisionHandler is called on every collision, in order, while the FS
	// lock is held.
	collisionHandler func(CollisionEvent)

	// journalHandler is called on every appended journal entry, in order,
	// while the FS lock is held.
	journalHandler func(JournalEntry)
}

// newFS creates a VFS for the workspace at rootDir. rootDir must exist.
// It is the shared bootstrap for Open; there is no standalone in-memory
// entry point.
func newFS(rootDir string) (*FS, error) {
	abs, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("vfs: resolve workspace root: %w", err)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("vfs: workspace root does not exist: %w", err)
	}
	return &FS{
		rootDir:  abs,
		root:     root,
		bindings: make(map[AgentID]Identity),
		calls:    make(map[string]bool),
		staged:   make(map[AgentID]*agentDelta),
		owners:   make(map[string]AgentID),
		cycles:   make(map[string]*cycleSnapshot),
	}, nil
}

// OnCollision registers a handler called for every emitted collision event;
// the handler runs synchronously under the FS lock and must not call back
// into the FS.
func (f *FS) OnCollision(fn func(CollisionEvent)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.collisionHandler = fn
}

// OnJournal registers a handler called for every journal entry appended, in
// order and under the same lock and re-entrancy rules as OnCollision.
func (f *FS) OnJournal(fn func(JournalEntry)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.journalHandler = fn
}

// stageLocked stages content for path; nil content stages a deletion.
func (f *FS) stageLocked(agent AgentID, path string, content []byte) error {
	identity, ok := f.bindings[agent]
	if !ok {
		return ErrIdentity
	}
	capability := model.VFSCapabilityWrite
	if content == nil {
		capability = model.VFSCapabilityDelete
	}
	if err := RequireVFSCapability(identity.Specialist, capability); err != nil {
		return err
	}
	rel := filepath.ToSlash(path)
	if err := f.validatePath(rel); err != nil {
		return err
	}
	if err := f.checkOwnership(agent, rel); err != nil {
		return err
	}

	if err := f.captureBaseLocked(agent, rel); err != nil {
		return err
	}
	before, existed, err := f.readMergedLocked(agent, rel)
	if err != nil {
		return err
	}
	d := f.ensureDelta(agent)
	d.revision++
	d.verdict = nil

	var staged []byte
	op, after := OpDelete, ""
	if content != nil {
		staged = append([]byte{}, content...)
		op, after = OpCreate, hashOf(staged, true)
		if existed {
			op = OpPatch
		}
	}
	d.files[rel] = staged

	f.appendJournalLocked(JournalEntry{
		Agent:      agent,
		Path:       rel,
		Operation:  op,
		BeforeHash: hashOf(before, existed),
		AfterHash:  after,
	})
	return nil
}

// readLockedAs checks workspace ownership for ordinary reads. ReadAs reaches
// this helper only after admitLocked has authorized the verifier's staged view.
func (f *FS) readLockedAs(agent AgentID, path string, authorizedStagedView bool) ([]byte, error) {
	rel := filepath.ToSlash(path)
	if err := f.validatePath(rel); err != nil {
		return nil, err
	}
	if !authorizedStagedView {
		identity, ok := f.bindings[agent]
		if !ok {
			return nil, ErrIdentity
		}
		if err := RequireVFSCapability(identity.Specialist, model.VFSCapabilityRead); err != nil {
			return nil, err
		}
		if err := f.checkOwnership(agent, rel); err != nil {
			f.appendJournalLocked(JournalEntry{Agent: agent, Path: rel, Operation: OpRead, Outcome: "denied"})
			return nil, err
		}
	}

	content, ok, err := f.readMergedLocked(agent, rel)
	if err != nil {
		f.appendJournalLocked(JournalEntry{Agent: agent, Path: rel, Operation: OpRead, Outcome: "failed"})
		return nil, err
	}
	if !ok {
		f.appendJournalLocked(JournalEntry{Agent: agent, Path: rel, Operation: OpRead, Outcome: "not_found"})
		return nil, os.ErrNotExist
	}

	f.appendJournalLocked(JournalEntry{
		Agent:     agent,
		Path:      rel,
		Operation: OpRead,
		AfterHash: hashOf(content, true),
	})
	return slices.Clone(content), nil
}

func (f *FS) consolidateLocked(agent AgentID) error {
	d, ok := f.staged[agent]
	if !ok {
		return nil // nothing to consolidate — idempotent
	}
	for path := range d.files {
		if f.owners[path] != agent {
			return ErrScopeDenied
		}
	}

	if d.verdict == nil && f.bindings[agent].CycleID != "" {
		return ErrVerificationRequired
	}
	if d.verdict != nil && !d.verdict.Pass {
		return fmt.Errorf("%w: finding: %s", ErrVerificationRequired, d.verdict.Finding)
	}

	if f.collidedLocked(f.bindings[agent]) {
		return ErrConsolidationConflict
	}

	if err := f.materializeLocked(agent, d); err != nil {
		return err
	}

	delete(f.staged, agent)
	f.releaseOwnershipLocked(agent)
	return nil
}

// ResolveCollision removes a matching collision from the unresolved list.
// Attempting agent, owning agent and path must all match.
func (f *FS) ResolveCollision(event CollisionEvent) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	defer f.finishLocked(&err)
	f.collisions = slices.DeleteFunc(f.collisions, func(c CollisionEvent) bool {
		return c.AttemptingAgent == event.AttemptingAgent &&
			c.OwningAgent == event.OwningAgent &&
			c.Path == event.Path
	})
	return nil
}

// ─── Git boundary guard ───────────────────────────────────────────────────────

// GuardGitMutation returns ErrGitMutationDenied if role is not permitted to
// execute mutating git commands; only model.RoleOrchestrator is.
func GuardGitMutation(role model.RoleClass) error {
	if role != model.RoleOrchestrator {
		return fmt.Errorf("%w: role %q cannot issue mutating git commands", ErrGitMutationDenied, role)
	}
	return nil
}

var readOnlyGitSubcommands = []string{"diff", "log", "show", "blame", "status", "ls-tree"}

// ReadOnlyGitSubcommands returns the read-only git subcommands.
func ReadOnlyGitSubcommands() []string {
	return slices.Clone(readOnlyGitSubcommands)
}

// ─── internal helpers ─────────────────────────────────────────────────────────

// ensureDelta returns the agentDelta for agent, creating it if absent.
func (f *FS) ensureDelta(agent AgentID) *agentDelta {
	d, ok := f.staged[agent]
	if !ok {
		d = &agentDelta{files: make(map[string][]byte), bases: make(map[string]baseFile)}
		f.staged[agent] = d
	}
	return d
}

// releaseOwnershipLocked drops every ownership claim held by agent.
func (f *FS) releaseOwnershipLocked(agent AgentID) {
	maps.DeleteFunc(f.owners, func(_ string, owner AgentID) bool {
		return owner == agent
	})
}

// stamp copies id's identity fields onto e.
func stamp(e *JournalEntry, id Identity) {
	e.Agent = id.AgentID
	e.Role = id.Role
	e.SessionID = id.SessionID
	e.WorkUnitID = id.WorkUnitID
	e.AttemptID = id.AttemptID
	e.CycleID = id.CycleID
	e.MandateClass = id.MandateClass
}

// appendJournalLocked stamps entry with its sequence number and appends it to the journal.
func (f *FS) appendJournalLocked(entry JournalEntry) {
	if id, ok := f.bindings[entry.Agent]; ok {
		key := entry.Agent
		stamp(&entry, id)
		if d := f.staged[key]; d != nil {
			entry.Revision = d.revision
		}
	}
	if cycleID, mandateClass, ok := f.consolidatedByLocked(entry.Path); ok {
		entry.ReeditOfCycleID, entry.ReeditOfMandateClass = cycleID, mandateClass
	}
	entry.Seq = len(f.journal)
	entry.Timestamp = time.Now()
	f.journal = append(f.journal, entry)
}

// checkOwnership returns ErrCollision if rel is owned by a different agent.
func (f *FS) checkOwnership(agent AgentID, rel string) error {
	existing, owned := f.owners[rel]
	if !owned {
		return fmt.Errorf("%w: %q is outside this binding's scope; ask the orchestrator to rebind with that path", ErrScopeDenied, rel)
	}
	if existing == agent {
		return nil
	}
	event := CollisionEvent{
		AttemptingAgent: agent,
		OwningAgent:     existing,
		Path:            rel,
		Timestamp:       time.Now(),
	}
	if id, ok := f.bindings[agent]; ok {
		event.SessionID = id.SessionID
		event.AttemptID = id.AttemptID
		event.WorkUnitID = id.WorkUnitID
		event.AttemptingAgent = id.AgentID
	}
	if id, ok := f.bindings[existing]; ok {
		event.OwningAgent = id.AgentID
	}
	f.collisions = append(f.collisions, event)
	identity := f.bindings[agent]
	return f.collisionError(identity, rel, existing)
}

// readMergedLocked resolves rel as seen by agent: staging over physical disk.
func (f *FS) readMergedLocked(agent AgentID, rel string) ([]byte, bool, error) {
	if d, ok := f.staged[agent]; ok {
		if content, staged := d.files[rel]; staged {
			return content, content != nil, nil // nil = staged delete
		}
		if base, ok := d.bases[rel]; ok {
			return base.Content, base.Present, nil
		}
	}
	base, err := f.physical(rel)
	if err != nil {
		return nil, false, err
	}
	return base.Content, base.Present, nil
}

// hashOf returns the hex-encoded SHA-256 of content, or "" when absent.
func hashOf(content []byte, present bool) string {
	if !present {
		return ""
	}
	h := sha256.Sum256(content)
	return hex.EncodeToString(h[:])
}
