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

package vfs

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// InvariantSet is the reference the verification gate judges a staged delta
// against: the governing documents in the precedence order PR-VFS-CSL-3 fixes
// (user goal and directives, the work unit's contract, the framework's planning
// artifacts). Paths are workspace-relative, each pinned by its content on disk.
type InvariantSet []string

// invariantScheme versions the derivation below, so a version computed by a
// later scheme can never be mistaken for one computed by this scheme.
const invariantScheme = "inv1"

// invariantsVersionLocked is the set's version: it changes whenever a declared
// document is added, removed, reordered or edited, which is what makes evidence
// stop governing under PR-VFS-CSL-7.
func (f *FS) invariantsVersionLocked(set InvariantSet) (string, error) {
	pinned := []string{invariantScheme}
	for _, document := range set {
		base, err := f.physical(document)
		if err != nil {
			return "", fmt.Errorf("%w: invariant document %q: %w", ErrIdentity, document, err)
		}
		pinned = append(pinned, document+"\x00"+hashOf(base.Content, base.Present))
	}
	return invariantScheme + ":" + hashOf([]byte(strings.Join(pinned, "\n")), true), nil
}

// Identity is supplied by the trusted coordinator's dispatch.
type Identity struct {
	SessionID  string
	WorkUnitID string
	// AttemptID is issued by the harness, never chosen by the caller: a bind
	// joins the attempt a live binding of the same unit holds open, or opens the
	// next one (PR-DAG-REP-3).
	AttemptID  string
	AgentID    AgentID
	Specialist string
	Role       model.RoleClass
	// Invariants is the applicable invariant set. A bind that joins an open
	// attempt inherits the set that attempt was opened with.
	Invariants InvariantSet
	// InvariantsHash is the version the harness pinned Invariants to at bind
	// time. Like Role and AttemptID, the caller cannot choose it.
	InvariantsHash string
	// CycleID and MandateClass attribute the dispatch to the maintenance cycle
	// and mandate class that declared it. Both are empty outside maintenance;
	// they attribute journal entries and never authorize anything.
	CycleID      string
	MandateClass string
	// GateAuthorKey links a preassigned verifier binding to the exact author's
	// binding/staged view. Empty for ordinary work.
	GateAuthorKey AgentID
	// Prelaunch marks an orchestrator-assigned binding that the target has not
	// adopted yet. It is persisted with the binding, not inferred from age.
	Prelaunch bool
}

// sameUnit reports whether both identities belong to one work unit.
func (a Identity) sameUnit(b Identity) bool {
	return a.SessionID == b.SessionID && a.WorkUnitID == b.WorkUnitID
}

// sameAttempt reports whether both identities belong to one dispatch attempt.
func (a Identity) sameAttempt(b Identity) bool {
	return a.sameUnit(b) && a.AttemptID == b.AttemptID
}

// verifies reports whether v is the gate linked to the author binding aKey and
// judges it against the same frozen invariants. Every verifier binding is a
// preassigned gate (Bind refuses any other), linked by the author key alone, so
// it runs as its own work unit. The explicit verify grant is checked at the FS
// admission boundary.
func (v Identity) verifies(aKey AgentID, a Identity) bool {
	return v.GateAuthorKey == aKey && v.SessionID == a.SessionID && v.InvariantsHash == a.InvariantsHash
}

// Sentinel errors for operation-level rejections.
var (
	// ErrStaleRevision is returned when the caller's expected revision no longer matches staged state.
	ErrStaleRevision = errors.New("vfs: revision changed")
	// ErrIdentity is returned when an operation lacks a valid binding or identity.
	ErrIdentity = errors.New("vfs: invalid operation identity")
	// ErrDuplicateCall is returned when a call ID is reused.
	ErrDuplicateCall = errors.New("vfs: call already consumed")
)

// Bind registers a dispatch identity and acquires its explicit file scope.
// Role metadata comes from canonical content; VFS access comes only from the
// instance's explicit catalog capabilities.
func (f *FS) Bind(identity Identity, scope []string) (key AgentID, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return "", err
	}
	defer f.finishLocked(&err)
	// Adopt a prelaunch assignment without letting the target replace its scope.
	if key, adopted, err := f.adoptPrelaunchLocked(identity, scope); adopted || err != nil {
		return key, err
	}
	grants, declared, grantErr := instanceVFSCapabilities(identity.Specialist)
	if grantErr != nil {
		return "", grantErr
	}
	if declared && slices.Contains(grants, model.VFSCapabilityVerify) {
		return "", fmt.Errorf("%w: verifier %q must adopt an orchestrator preassignment linked to author_key", ErrScopeDenied, identity.Specialist)
	}
	if identity, err = f.bindableIdentity(identity, scope); err != nil {
		return "", err
	}
	if err = f.checkScope(identity, scope); err != nil {
		return "", err
	}
	return f.claimLocked(identity, scope)
}

// AssignScope atomically reserves explicit exclusive ownership for a target
// before its VFS session starts. The target instance must have explicit bind
// and write grants in the catalog; role alone never authorizes an assignment.
func (f *FS) AssignScope(identity Identity, scope []string) (key AgentID, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return "", err
	}
	defer f.finishLocked(&err)
	if !completeIdentity(identity) || identity.Specialist == "" || len(scope) == 0 {
		return "", ErrIdentity
	}
	grants, _, grantErr := instanceVFSCapabilities(identity.Specialist)
	if grantErr != nil {
		return "", grantErr
	}
	if slices.Contains(grants, model.VFSCapabilityVerify) {
		return "", fmt.Errorf("%w: verifier %q requires an empty-scope author_key gate assignment", ErrScopeDenied, identity.Specialist)
	}
	identity.Prelaunch = true
	if identity, err = f.bindableIdentity(identity, scope); err != nil {
		return "", err
	}
	// Assignment refusals are denial observations, not unresolved in-flight
	// conflicts. Validate the complete request before capturing or claiming paths.
	if err = f.checkClaimScope(identity, scope); err != nil {
		return "", err
	}
	return f.claimLocked(identity, scope)
}

// adoptPrelaunchLocked adopts the prelaunch assignment matching identity, if
// any. It reports adopted=false when no assignment matches.
func (f *FS) adoptPrelaunchLocked(identity Identity, scope []string) (AgentID, bool, error) {
	for assignedKey, assigned := range f.bindings {
		if !assigned.Prelaunch || assigned.SessionID != identity.SessionID || assigned.WorkUnitID != identity.WorkUnitID || assigned.AgentID != identity.AgentID {
			continue
		}
		if assigned.Specialist != identity.Specialist || assigned.GateAuthorKey != identity.GateAuthorKey ||
			(identity.AttemptID != "" && assigned.AttemptID != identity.AttemptID) ||
			(len(identity.Invariants) > 0 && !slices.Equal(assigned.Invariants, identity.Invariants)) ||
			(len(scope) > 0 && !sameScope(f.ownedScopeLocked(assignedKey), scope)) {
			return "", true, ErrScopeDenied
		}
		if err := f.requireAdoptionGrantsLocked(assigned); err != nil {
			return "", true, err
		}
		assigned.Prelaunch = false
		f.bindings[assignedKey] = assigned
		return assignedKey, true, nil
	}
	return "", false, nil
}

// requireAdoptionGrantsLocked checks the catalog grants an adopted assignment
// needs: bind plus read, verify and a valid author link for a verifier gate,
// or bind plus write otherwise.
func (f *FS) requireAdoptionGrantsLocked(assigned Identity) error {
	if err := RequireVFSCapability(assigned.Specialist, model.VFSCapabilityBind); err != nil {
		return err
	}
	if assigned.GateAuthorKey == "" {
		return RequireVFSCapability(assigned.Specialist, model.VFSCapabilityWrite)
	}
	if err := RequireVFSCapability(assigned.Specialist, model.VFSCapabilityRead); err != nil {
		return err
	}
	if err := RequireVFSCapability(assigned.Specialist, model.VFSCapabilityVerify); err != nil {
		return err
	}
	return f.validateVerifierAuthorLocked(assigned, assigned.GateAuthorKey)
}

// claimLocked captures the base of every scoped path and registers identity as
// its exclusive owner under a fresh key.
func (f *FS) claimLocked(identity Identity, scope []string) (AgentID, error) {
	bases := make(map[string]baseFile, len(scope))
	for _, p := range scope {
		var err error
		if bases[p], err = f.physical(p); err != nil {
			return "", err
		}
	}
	key := AgentID(fmt.Sprintf("%x", randomID()))
	f.bindings[key] = identity
	f.ensureDelta(key).bases = bases
	for _, p := range scope {
		f.owners[p] = key
	}
	return key, nil
}

// AssignVerifier reserves an empty-scope verifier binding linked to one
// already-issued author key. The orchestrator must do this before launching
// the verifier; identity and author key are harness-owned inputs.
func (f *FS) AssignVerifier(identity Identity, authorKey AgentID) (key AgentID, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return "", err
	}
	defer f.finishLocked(&err)
	if !completeIdentity(identity) || identity.Specialist == "" || authorKey == "" {
		return "", ErrIdentity
	}
	for _, capability := range []model.VFSCapability{model.VFSCapabilityRead, model.VFSCapabilityVerify} {
		if err = RequireVFSCapability(identity.Specialist, capability); err != nil {
			return "", err
		}
	}
	identity.GateAuthorKey = authorKey
	identity.Prelaunch = true
	if identity, err = f.bindableIdentity(identity, nil); err != nil {
		return "", err
	}
	if err = f.validateVerifierAuthorLocked(identity, authorKey); err != nil {
		return "", err
	}
	key = AgentID(fmt.Sprintf("%x", randomID()))
	f.bindings[key] = identity
	f.ensureDelta(key)
	return key, nil
}

func (f *FS) validateVerifierAuthorLocked(verifier Identity, authorKey AgentID) error {
	author, ok := f.bindings[authorKey]
	if !ok || author.SessionID != verifier.SessionID || author.AgentID == verifier.AgentID {
		return fmt.Errorf("%w: verifier author_key %q does not name another binding in the target session", ErrIdentity, authorKey)
	}
	if err := RequireVFSCapability(author.Specialist, model.VFSCapabilityWrite); err != nil {
		return err
	}
	if author.InvariantsHash != verifier.InvariantsHash {
		return fmt.Errorf("%w: verifier author_key %q is not judged against the same invariant set", ErrIdentity, authorKey)
	}
	return nil
}

// OwnershipClaims lists current claims in deterministic path order. A claim is
// Active only when adopted in currentSessionID; pending and prior-session
// ownership remain visible and are never expired automatically.
func (f *FS) OwnershipClaims(currentSessionID string) []OwnershipClaim {
	f.mu.RLock()
	defer f.mu.RUnlock()
	paths := make(map[AgentID][]string)
	for path, key := range f.owners {
		paths[key] = append(paths[key], path)
	}
	// A verifier owns no paths: its gate stays listed while pending, and after
	// adoption while the author it judges still holds staged ownership.
	owning := maps.Clone(paths)
	for key, identity := range f.bindings {
		_, authorOwns := owning[identity.GateAuthorKey]
		if identity.Prelaunch || (identity.GateAuthorKey != "" && authorOwns) {
			if _, exists := paths[key]; !exists {
				paths[key] = []string{}
			}
		}
	}
	keys := slices.Sorted(maps.Keys(paths))
	claims := make([]OwnershipClaim, 0, len(keys))
	for _, key := range keys {
		scope := paths[key]
		slices.Sort(scope)
		identity := f.bindings[key]
		claims = append(claims, OwnershipClaim{
			Key: key, AgentID: identity.AgentID, TargetInstance: identity.Specialist, RootSessionID: identity.SessionID,
			WorkUnitID: identity.WorkUnitID, Scope: scope, AuthorKey: identity.GateAuthorKey,
			Active:  identity.SessionID == currentSessionID && !identity.Prelaunch,
			Pending: identity.Prelaunch,
		})
	}
	return claims
}

// RevokeOwnership releases only the key's file ownership. Binding and staged
// delta state are intentionally retained for inspection and recovery.
func (f *FS) RevokeOwnership(key AgentID) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	defer f.finishLocked(&err)
	identity, ok := f.bindings[key]
	if !ok {
		return fmt.Errorf("%w: %q is not a known binding key", ErrIdentity, key)
	}
	identity.Prelaunch = false
	f.bindings[key] = identity
	f.releaseOwnershipLocked(key)
	return nil
}

func (f *FS) checkClaimScope(identity Identity, scope []string) error {
	for _, requested := range scope {
		if err := f.validatePath(requested); err != nil {
			return err
		}
		for existingPath, ownerKey := range f.owners {
			if strings.EqualFold(requested, existingPath) {
				return f.collisionError(identity, requested, ownerKey)
			}
		}
	}
	return nil
}

func (f *FS) ownedScopeLocked(key AgentID) []string {
	var scope []string
	for path, owner := range f.owners {
		if owner == key {
			scope = append(scope, path)
		}
	}
	return scope
}

func sameScope(a, b []string) bool {
	return slices.Equal(slices.Sorted(slices.Values(a)), slices.Sorted(slices.Values(b)))
}

// SpecialistRole resolves the role a catalog instance declares.
func SpecialistRole(instance string) (model.RoleClass, error) {
	definitions, err := catalog.LoadNativeContent()
	if err != nil {
		return "", err
	}
	definition, ok := definitions[instance]
	if !ok {
		return "", fmt.Errorf("%w: %q is not a catalog specialist instance", ErrIdentity, instance)
	}
	return definition.Role, nil
}

// OrchestratorInstance is the catalog instance of the trusted coordinator, the
// only one whose grants authorize claims, discard and consolidation.
const OrchestratorInstance = "takt"

// RequireVFSCapability consults the canonical exact-instance catalog grant.
// RoleClass describes the agent but never authorizes a VFS operation.
func RequireVFSCapability(instance string, capability model.VFSCapability) error {
	grants, declared, err := instanceVFSCapabilities(instance)
	if err != nil {
		return err
	}
	if !declared {
		return fmt.Errorf("%w: instance %q has no explicit VFS capability declaration", ErrIdentity, instance)
	}
	if !slices.Contains(grants, capability) {
		return fmt.Errorf("%w: instance %q lacks VFS capability %q", ErrScopeDenied, instance, capability)
	}
	return nil
}

func instanceVFSCapabilities(instance string) ([]model.VFSCapability, bool, error) {
	packages, err := catalog.LoadPackages()
	if err != nil {
		return nil, false, err
	}
	for _, definition := range packages.Agents {
		if !slices.Contains(definition.Instances, instance) {
			continue
		}
		grants, declared := definition.VFSCapabilities(instance)
		return grants, declared, nil
	}
	return nil, false, fmt.Errorf("%w: %q is not a catalog specialist instance", ErrIdentity, instance)
}

// bindableIdentity completes role metadata and enforces explicit bind/write
// capabilities, rejecting identities that may not bind or bind twice.
func (f *FS) bindableIdentity(identity Identity, scope []string) (Identity, error) {
	if !completeIdentity(identity) {
		return identity, ErrIdentity
	}
	role, err := SpecialistRole(identity.Specialist)
	if err != nil {
		return identity, err
	}
	if err = RequireVFSCapability(identity.Specialist, model.VFSCapabilityBind); err != nil {
		return identity, err
	}
	identity.Role = role
	if len(scope) > 0 {
		if err = RequireVFSCapability(identity.Specialist, model.VFSCapabilityWrite); err != nil {
			return identity, err
		}
	}
	identity.AttemptID, identity.Invariants = f.attemptLocked(identity)
	if identity.InvariantsHash, err = f.invariantsVersionLocked(identity.Invariants); err != nil {
		return identity, err
	}
	if f.duplicateIdentity(identity) {
		return identity, ErrIdentity
	}
	return identity, nil
}

// attemptLocked returns the attempt this dispatch belongs to and the invariant
// set that governs it. Delegated work carries the attempt its admission issued;
// otherwise, as for maintenance cycles, the store counts attempts itself. An
// attempt stays open while a binding of its unit still holds file ownership, so
// a maintenance verifier sharing its cycle's unit joins the author it judges,
// and a retry after consolidation or
// discard opens the next attempt. Bindings and ownership are durable, so a
// restart neither restarts the count nor reuses an identity.
func (f *FS) attemptLocked(identity Identity) (string, InvariantSet) {
	// An attempt the delegation's admission already issued is authoritative:
	// the bind joins it, inheriting the invariant set it was opened with.
	if identity.AttemptID != "" {
		for _, existing := range f.bindings {
			if existing.sameAttempt(identity) {
				return existing.AttemptID, existing.Invariants
			}
		}
		return identity.AttemptID, identity.Invariants
	}
	owning := make(map[AgentID]bool, len(f.owners))
	for _, owner := range f.owners {
		owning[owner] = true
	}
	highest, open := 0, Identity{}
	for key, existing := range f.bindings {
		n, err := strconv.Atoi(existing.AttemptID)
		if err != nil || !existing.sameUnit(identity) {
			continue
		}
		if n > highest {
			highest, open = n, Identity{}
		}
		if n == highest && owning[key] {
			open = existing
		}
	}
	if open.AttemptID != "" {
		return open.AttemptID, open.Invariants
	}
	return strconv.Itoa(highest + 1), identity.Invariants
}

func completeIdentity(identity Identity) bool {
	return identity.SessionID != "" && identity.WorkUnitID != "" && identity.AgentID != ""
}
func (f *FS) duplicateIdentity(identity Identity) bool {
	for _, existing := range f.bindings {
		if existing.sameAttempt(identity) && existing.AgentID == identity.AgentID {
			return true
		}
	}
	return false
}

// BindingIdentity returns the identity a binding key was issued for, so a
// transport can check that a caller is the actor the key belongs to.
func (f *FS) BindingIdentity(key AgentID) (Identity, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	id, ok := f.bindings[key]
	return id, ok
}

// checkScope requires every scope path to be valid and unclaimed; a claimed
// path is recorded as a collision.
func (f *FS) checkScope(identity Identity, scope []string) error {
	for _, p := range scope {
		if err := f.validatePath(p); err != nil {
			return err
		}
		for claimed, owner := range f.owners {
			if strings.EqualFold(p, claimed) {
				f.recordCollision(identity, p, owner)
				return f.collisionError(identity, p, owner)
			}
		}
	}
	return nil
}

func (f *FS) collisionError(target Identity, path string, ownerKey AgentID) error {
	owner, ok := f.bindings[ownerKey]
	if !ok {
		owner.AgentID = ownerKey
	}
	return &CollisionError{
		RequestedPath: path, TargetAgent: target.AgentID, TargetInstance: target.Specialist,
		TargetSession: target.SessionID, TargetUnit: target.WorkUnitID,
		OwnerAgent: owner.AgentID, OwnerSession: owner.SessionID, OwnerUnit: owner.WorkUnitID,
	}
}

func (f *FS) recordCollision(identity Identity, path string, owner AgentID) {
	if bound, ok := f.bindings[owner]; ok {
		owner = bound.AgentID
	}
	f.collisions = append(f.collisions, CollisionEvent{SessionID: identity.SessionID, WorkUnitID: identity.WorkUnitID, AttemptID: identity.AttemptID, AttemptingAgent: identity.AgentID, OwningAgent: owner, Path: path, Timestamp: time.Now()})
}

// Operation carries adapter-controlled identity and a revision from the last result.
type Operation struct {
	Key              AgentID
	CallID           string
	ExpectedRevision uint64
	Action           OperationType
	Path             string
	Content          []byte
}

// OperationResult returns the merged view content an operation produced.
type OperationResult struct {
	Content   []byte
	Revision  uint64
	DeltaHash string
}

func (f *FS) deltaHashLocked(key AgentID) string {
	d := f.staged[key]
	if d == nil {
		return hashOf([]byte("{}"), true)
	}
	// JSON map encoding orders keys and distinguishes nil deletion from empty.
	b, _ := json.Marshal(d.files)
	return hashOf(b, true)
}

// revisionOf is the staged revision of key, zero when it has no delta.
func (f *FS) revisionOf(key AgentID) uint64 {
	if d := f.staged[key]; d != nil {
		return d.revision
	}
	return 0
}

// correlateLocked stamps journal entries from start with key's identity.
func (f *FS) correlateLocked(start int, key, view AgentID, call string) {
	id := f.bindings[key]
	d := f.staged[view]
	if d == nil {
		d = f.staged[key]
	}
	for i := start; i < len(f.journal); i++ {
		e := &f.journal[i]
		stamp(e, id)
		e.CallID = call
		if d != nil {
			e.Revision = d.revision
		}
	}
}

// consumeCallLocked marks call as used in session; a replay is rejected.
func (f *FS) consumeCallLocked(session, call string) error {
	key := session + "\x00" + call
	if f.calls[key] {
		return fmt.Errorf("%w: %q was already consumed by a prior request; issue a fresh call", ErrDuplicateCall, call)
	}
	f.calls[key] = true
	return nil
}

// Apply validates identity, revision and replay, then persists the result.
func (f *FS) Apply(op Operation) (OperationResult, error) { return f.apply(op, op.Key) }

// ReadAs reads through the staged view of the author a verifier judges, so the
// gate assesses exactly the delta its verdict will cover. op.ExpectedRevision
// is the author's revision; any other caller or action is refused.
func (f *FS) ReadAs(op Operation, author AgentID) (OperationResult, error) {
	if op.Action != OpRead {
		return OperationResult{}, ErrIdentity
	}
	return f.apply(op, author)
}

func (f *FS) apply(op Operation, view AgentID) (result OperationResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return result, err
	}
	start := len(f.journal)
	defer func() {
		if err != nil && len(f.journal) == start {
			f.appendJournalLocked(JournalEntry{Agent: op.Key, Operation: op.Action, Outcome: "denied"})
		}
		f.correlateLocked(start, op.Key, view, op.CallID)
		f.finishLocked(&err)
		if err != nil {
			result.Content = nil
		}
	}()
	identity, err := f.admitLocked(op, view)
	if err != nil {
		return result, err
	}
	result.Content, err = f.dispatchLocked(op, view, identity)
	result.Revision = f.revisionOf(view)
	result.DeltaHash = f.deltaHashLocked(view)
	return result, err
}

// admitLocked runs the checks that precede any effect: a bound identity, a
// legitimate view, a fresh call, no unresolved collision and the caller's
// revision. The call is consumed even when a later check rejects it.
func (f *FS) admitLocked(op Operation, view AgentID) (Identity, error) {
	identity, ok := f.bindings[op.Key]
	if !ok || op.CallID == "" {
		return identity, fmt.Errorf("%w: key %q has no active binding; ask the orchestrator to rebind", ErrIdentity, op.Key)
	}
	if view != op.Key {
		target, ok := f.bindings[view]
		if !ok || op.Action != OpRead {
			return identity, ErrIdentity
		}
		if err := RequireVFSCapability(identity.Specialist, model.VFSCapabilityVerify); err != nil {
			return identity, err
		}
		if !identity.verifies(view, target) {
			return identity, ErrIdentity
		}
	}
	if err := f.consumeCallLocked(identity.SessionID, op.CallID); err != nil {
		return identity, err
	}
	if f.collidedLocked(identity) {
		return identity, ErrCollision
	}
	if current := f.revisionOf(view); current != op.ExpectedRevision {
		return identity, fmt.Errorf("%w: current revision %d; re-read and issue a fresh call", ErrStaleRevision, current)
	}
	return identity, nil
}

// collidedLocked reports an unresolved collision in id's own session, unit and
// attempt; collisions of other units never block it. Caller must hold the lock.
func (f *FS) collidedLocked(id Identity) bool {
	return slices.ContainsFunc(f.collisions, func(c CollisionEvent) bool {
		return c.SessionID == id.SessionID && c.WorkUnitID == id.WorkUnitID && c.AttemptID == id.AttemptID
	})
}

// dispatchLocked performs an operation only when the bound catalog instance
// has its exact operation capability.
func (f *FS) dispatchLocked(op Operation, view AgentID, identity Identity) ([]byte, error) {
	switch op.Action {
	case OpRead:
		if err := RequireVFSCapability(identity.Specialist, model.VFSCapabilityRead); err != nil {
			return nil, err
		}
		return f.readLockedAs(view, op.Path, view != op.Key)
	case OpCreate, OpPatch:
		content := op.Content
		if content == nil {
			content = []byte{} // nil content would stage a deletion
		}
		return nil, f.stageLocked(op.Key, op.Path, content)
	case OpDelete:
		return nil, f.stageLocked(op.Key, op.Path, nil)
	case OpRollback:
		// Discarding a delta is the orchestrator's backtracking decision
		// (PR-VFS-CSL-5), never the author's.
		if err := RequireVFSCapability(OrchestratorInstance, model.VFSCapabilityDiscard); err != nil {
			return nil, err
		}
		f.rollbackLocked(op.Key)
		return nil, nil
	default:
		return nil, errors.New("vfs: unsupported operation")
	}
}

// rollbackLocked discards key's staged delta, journaling each dropped path.
func (f *FS) rollbackLocked(key AgentID) {
	if d := f.staged[key]; d != nil {
		for _, p := range slices.Sorted(maps.Keys(d.files)) {
			f.appendJournalLocked(JournalEntry{Agent: key, Path: p, Operation: OpRollback, BeforeHash: hashOf(d.files[p], d.files[p] != nil)})
		}
		d.files = map[string][]byte{}
		d.bases = map[string]baseFile{}
		d.verdict = nil
		d.revision++
	}
	f.releaseOwnershipLocked(key)
}

// Verify attaches a verification gate to an exact revision and delta hash.
func (f *FS) Verify(verifier, author AgentID, callID string, expected uint64, deltaHash string, pass bool, finding string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	start := len(f.journal)
	defer func() {
		if err != nil && len(f.journal) == start {
			f.appendJournalLocked(JournalEntry{Agent: verifier, Operation: OperationType("verdict"), Outcome: "denied"})
		}
		f.correlateLocked(start, verifier, verifier, callID)
		f.finishLocked(&err)
	}()
	v, a, err := f.admitVerifierLocked(verifier, author, callID)
	if err != nil {
		return err
	}
	d := f.staged[author]
	if !validVerdict(d, expected, f.deltaHashLocked(author), deltaHash, pass, finding) {
		return ErrInvalidVerdict
	}
	// The verdict names the invariant set it was judged against and the version
	// of that set as the sources read now, not as they read at bind time
	// (PR-VFS-CSL-7).
	version, err := f.invariantsVersionLocked(a.Invariants)
	if err != nil {
		return err
	}
	d.verdict = &VerificationVerdict{Pass: pass, Finding: finding, VerifierID: v.AgentID, VerifierRole: v.Role, Revision: expected, DeltaHash: deltaHash, Invariants: a.Invariants, InvariantsHash: version}
	// Findings may contain sensitive source excerpts: retain in private state,
	// never export them in the content-free journal.
	outcome := "rejected"
	if pass {
		outcome = "passed"
	}
	f.appendJournalLocked(JournalEntry{Agent: verifier, Operation: OperationType("verdict"), Outcome: outcome, AfterHash: deltaHash})
	// A failing ordinary gate retains the delta for correction. A GC gate's
	// coordinator owns the full-cycle discard after this verdict is recorded.
	return nil
}

func validVerdict(d *agentDelta, expected uint64, actualHash, deltaHash string, pass bool, finding string) bool {
	return d != nil && d.revision == expected && actualHash == deltaHash && (pass || strings.TrimSpace(finding) != "")
}

// admitVerifierLocked resolves both bindings and consumes callID: the verifier
// must be entitled to judge the author, and never the author itself.
func (f *FS) admitVerifierLocked(verifier, author AgentID, callID string) (v, a Identity, err error) {
	v, vok := f.bindings[verifier]
	a, aok := f.bindings[author]
	if !vok || !aok || callID == "" {
		return v, a, ErrIdentity
	}
	// The grant decides first: a caller without verify is told so, whatever
	// binding it names.
	if err = RequireVFSCapability(v.Specialist, model.VFSCapabilityVerify); err != nil {
		return v, a, err
	}
	if !v.verifies(author, a) {
		return v, a, ErrIdentity
	}
	if v.AgentID == a.AgentID {
		return v, a, ErrSelfVerification
	}
	return v, a, f.consumeCallLocked(v.SessionID, callID)
}

// ConsolidateCheckpoint is a trusted coordinator operation that consolidates staged changes.
func (f *FS) ConsolidateCheckpoint(key AgentID, checkpoint string, expected uint64) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	if strings.TrimSpace(checkpoint) == "" {
		return ErrVerificationRequired
	}
	if _, ok := f.bindings[key]; !ok {
		return ErrIdentity
	}
	d := f.staged[key]
	if d == nil || d.revision != expected {
		return ErrStaleRevision
	}
	if d.verdict == nil || d.verdict.DeltaHash != f.deltaHashLocked(key) {
		return ErrInvalidVerdict
	}
	// A pass applies only to the scope it was given: re-pinning the verdict's own
	// set catches a governing document edited since the verdict was issued, which
	// requires new verification before the evidence governs (PR-VFS-CSL-7).
	version, err := f.invariantsVersionLocked(d.verdict.Invariants)
	if err != nil {
		return err
	}
	if version != d.verdict.InvariantsHash {
		return fmt.Errorf("%w: the applicable invariant set changed since the verdict", ErrInvalidVerdict)
	}
	start := len(f.journal)
	defer func() {
		f.correlateLocked(start, key, key, "checkpoint/"+hashOf([]byte(checkpoint), true))
		f.finishLocked(&err)
	}()
	return f.consolidateLocked(key)
}
