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
// against, in the precedence order PR-VFS-CSL-3 fixes (user goal and
// directives, the work unit's contract, the framework's planning artifacts).
// An entry is either an Engram reference (see EngramInvariant), which pins
// itself because an entry is immutable and a revision is another entry, or a
// workspace-relative path pinned by its content on disk.
type InvariantSet []string

// engramInvariantPrefix marks an invariant that is an Engram entry.
const engramInvariantPrefix = "engram:"

// EngramInvariant is the invariant that names the Engram entry id.
func EngramInvariant(id int64) string {
	return engramInvariantPrefix + strconv.FormatInt(id, 10)
}

// invariantScheme versions the derivation below, so a version computed by a
// later scheme can never be mistaken for one computed by this scheme.
const invariantScheme = "inv1"

// invariantsVersionLocked is the set's version: it changes whenever a declared
// invariant is added, removed, reordered or, for a path, edited, which is what
// makes evidence stop governing under PR-VFS-CSL-7.
func (f *FS) invariantsVersionLocked(set InvariantSet) (string, error) {
	pinned := []string{invariantScheme}
	for _, invariant := range set {
		if strings.HasPrefix(invariant, engramInvariantPrefix) {
			id, perr := strconv.ParseInt(strings.TrimPrefix(invariant, engramInvariantPrefix), 10, 64)
			if perr != nil || id <= 0 {
				return "", fmt.Errorf("%w: invariant %q is not a valid Engram reference", ErrIdentity, invariant)
			}
			pinned = append(pinned, EngramInvariant(id))
			continue
		}
		base, err := f.physical(invariant)
		if err != nil {
			return "", fmt.Errorf("%w: invariant document %q: %w", ErrIdentity, invariant, err)
		}
		pinned = append(pinned, invariant+"\x00"+hashOf(base.Content, base.Present))
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
	// Superseded marks a verifier gate that a later assignment of the same
	// judge over the same author replaced: it no longer attaches verdicts.
	Superseded bool
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
	// ErrNothingStaged is returned when consolidation names work that has no staged change.
	ErrNothingStaged = errors.New("vfs: nothing is staged for that author key")
	// ErrStagedWork is returned when a claim to release still holds staged work: a
	// delta without an owner could never be consolidated.
	ErrStagedWork = errors.New("vfs: the claim holds staged work; consolidate it, reassign it with its author key, or discard it")
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
	// Binding again is how an agent refreshes what it knows of its staged work.
	if key, held, err := f.heldBindingLocked(identity, scope); held || err != nil {
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
// Valid fresh assignments discard whole overlapping claims from other roots;
// same-root collisions and explicit ReassignScope continuity remain unchanged.
func (f *FS) AssignScope(identity Identity, scope []string) (key AgentID, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return "", err
	}
	defer f.finishLocked(&err)
	if !completeIdentity(identity) {
		return "", errIncompleteIdentity
	}
	if key, held, err := f.heldBindingLocked(identity, scope); held || err != nil {
		return key, err
	}
	if identity.Specialist == "" || len(scope) == 0 {
		return "", fmt.Errorf("%w: an assignment names the specialist and at least one path", ErrIdentity)
	}
	grants, _, grantErr := instanceVFSCapabilities(identity.Specialist)
	if grantErr != nil {
		return "", grantErr
	}
	if slices.Contains(grants, model.VFSCapabilityVerify) {
		return "", fmt.Errorf("%w: verifier %q requires an empty-scope author_key gate assignment", ErrScopeDenied, identity.Specialist)
	}
	// Assigning the claim a unit already holds changes nothing; a different
	// scope for it is a reassignment, which names the work it keeps.

	identity.Prelaunch = true
	if identity, err = f.bindableIdentity(identity, scope); err != nil {
		return "", err
	}
	// Assignment refusals are denial observations, not unresolved in-flight
	// conflicts. Validate the complete request before capturing or claiming paths.
	prior := make(map[AgentID]bool)
	for _, path := range scope {
		// Read every base before discarding anything, including invalid paths.
		if _, err = f.physical(path); err != nil {
			return "", err
		}
	}
	for _, path := range scope {
		for claimed, owner := range f.owners {
			if !strings.EqualFold(path, claimed) {
				continue
			}
			if f.bindings[owner].SessionID == identity.SessionID {
				return "", f.collisionError(identity, path, owner)
			}
			prior[owner] = true
		}
	}
	// Capture the new claim first: a failed base read must preserve prior work.
	key, err = f.claimLocked(identity, scope)
	if err != nil {
		return "", err
	}
	for owner := range prior {
		f.rollbackLocked(owner)
		held := f.bindings[owner]
		held.Prelaunch = false
		f.bindings[owner] = held
	}
	return key, nil
}

// adoptPrelaunchLocked adopts the prelaunch assignment matching identity, if
// any. It reports adopted=false when no assignment matches.
func (f *FS) adoptPrelaunchLocked(identity Identity, scope []string) (AgentID, bool, error) {
	for assignedKey, assigned := range f.bindings {
		if !assigned.Prelaunch || assigned.SessionID != identity.SessionID || assigned.WorkUnitID != identity.WorkUnitID || assigned.AgentID != identity.AgentID {
			continue
		}
		// A verifier unit holds one gate per author it judges; a gate preassigned
		// to another author is not this bind's assignment.
		if assigned.GateAuthorKey != "" && identity.GateAuthorKey != "" && assigned.GateAuthorKey != identity.GateAuthorKey {
			continue
		}
		retried := f.retriedAssignmentLocked(assigned, identity)
		if assigned.Specialist != identity.Specialist || assigned.GateAuthorKey != identity.GateAuthorKey ||
			(identity.AttemptID != "" && assigned.AttemptID != identity.AttemptID && !retried) ||
			(len(identity.Invariants) > 0 && !slices.Equal(assigned.Invariants, identity.Invariants)) ||
			(len(scope) > 0 && !sameScope(f.ownedScopeLocked(assignedKey), scope)) {
			return "", true, ErrScopeDenied
		}
		if err := f.requireAdoptionGrantsLocked(assigned); err != nil {
			return "", true, err
		}
		if retried {
			assigned.AttemptID = identity.AttemptID
		}
		assigned.Prelaunch = false
		f.bindings[assignedKey] = assigned
		return assignedKey, true, nil
	}
	return "", false, nil
}

// retriedAssignmentLocked reports whether identity is a later admitted attempt
// of the unit taking over an author assignment an earlier attempt never adopted
// (its specialist ended before binding). The assignment, its key and anything
// staged under it continue under the admitted attempt, as a reassignment would.
func (f *FS) retriedAssignmentLocked(assigned, identity Identity) bool {
	if assigned.GateAuthorKey != "" || identity.AttemptID == "" {
		return false
	}
	was, errWas := strconv.Atoi(assigned.AttemptID)
	now, errNow := strconv.Atoi(identity.AttemptID)
	if errWas != nil || errNow != nil || now <= was {
		return false
	}
	renumbered := assigned
	renumbered.AttemptID = identity.AttemptID
	return !f.duplicateIdentity(renumbered)
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
// already-issued author key. The harness prepares it for the admitted attempt
// before launching the verifier; identity and author key are harness-owned inputs.
func (f *FS) AssignVerifier(identity Identity, authorKey AgentID) (key AgentID, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return "", err
	}
	defer f.finishLocked(&err)
	if !completeIdentity(identity) || identity.Specialist == "" || authorKey == "" {
		return "", fmt.Errorf("%w: a verifier gate names its session, unit, agent and specialist, and the author_key it judges", ErrIdentity)
	}
	for _, capability := range []model.VFSCapability{model.VFSCapabilityRead, model.VFSCapabilityVerify} {
		if err = RequireVFSCapability(identity.Specialist, capability); err != nil {
			return "", err
		}
	}
	identity.GateAuthorKey = authorKey
	identity.Prelaunch = true
	// The gate is judged against the invariants its author was issued; a caller
	// that declares none takes them from the author binding.
	if author, ok := f.bindings[authorKey]; ok && len(identity.Invariants) == 0 {
		identity.Invariants = author.Invariants
	}
	if identity, err = f.bindableIdentity(identity, nil); err != nil {
		return "", err
	}
	if err = f.validateVerifierAuthorLocked(identity, authorKey); err != nil {
		return "", err
	}
	// A new assignment of this judge over this author supersedes every earlier
	// gate of it, adopted or not and under any unit: the judge speaks through
	// its latest delegation only, so a run it replaced cannot overwrite its
	// verdict, and a pending one is never adopted by map iteration order.
	for previousKey, previous := range f.bindings {
		if previous.SessionID == identity.SessionID && previous.AgentID == identity.AgentID &&
			previous.GateAuthorKey == authorKey && !previous.Superseded {
			previous.Prelaunch, previous.Superseded = false, true
			f.bindings[previousKey] = previous
		}
	}
	key = AgentID(fmt.Sprintf("%x", randomID()))
	f.bindings[key] = identity
	f.ensureDelta(key)
	return key, nil
}

// PendingGate returns the unadopted, current gate identity's judge already
// holds over authorKey in the same unit and attempt; an empty attempt matches
// the open one, as the store numbers maintenance attempts itself. A
// coordinator that retries its own step reuses that gate instead of asking
// AssignVerifier, which refuses a second gate for one judge and author.
func (f *FS) PendingGate(identity Identity, authorKey AgentID) (AgentID, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	identity.GateAuthorKey = authorKey
	for key, gate := range f.bindings {
		if gate.Prelaunch && !gate.Superseded && gate.sameUnit(identity) && gate.AgentID == identity.AgentID &&
			gate.Specialist == identity.Specialist && gate.GateAuthorKey == identity.GateAuthorKey &&
			(identity.AttemptID == "" || gate.AttemptID == identity.AttemptID) {
			return key, true
		}
	}
	return "", false
}

// validateVerifierAuthorLocked requires authorKey to name a binding of another
// specialist in the verifier's session that may write and was issued the same
// invariant set the verifier is judged against.
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
		if identity.Superseded {
			continue
		}
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
			Active:   identity.SessionID == currentSessionID && !identity.Prelaunch,
			Pending:  identity.Prelaunch,
			Staged:   f.stagedLocked(key),
			Verdicts: f.currentVerdictsLocked(key),
		})
	}
	return claims
}

// stagedLocked reports whether key holds at least one staged change.
func (f *FS) stagedLocked(key AgentID) bool {
	d := f.staged[key]
	return d != nil && len(d.files) > 0
}

// RevokeOwnership frees the paths a claim holds. The binding is kept so the
// unit's attempt count survives. A claim that holds staged work is refused
// rather than left as a delta nobody owns, which could never be consolidated.
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
	if f.stagedLocked(key) {
		return ErrStagedWork
	}
	identity.Prelaunch = false
	f.bindings[key] = identity
	f.releaseOwnershipLocked(key)
	return nil
}

// ReassignScope gives the work of an existing author key a new exact scope for
// its next attempt, keeping the staged delta, and hands it to the unit, agent and
// specialist the request names. The delta's own paths must stay in
// the scope; only the added paths are checked for collisions, and the paths that
// leave it were never written. The verdict, bound to the old revision, is cleared.
func (f *FS) ReassignScope(identity Identity, key AgentID, scope []string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	defer f.finishLocked(&err)
	// The key locates the staged work; whoever the request names takes it over.
	held, ok := f.bindings[key]
	if !ok || held.GateAuthorKey != "" {
		return fmt.Errorf("%w: %q is not an author key", ErrIdentity, key)
	}
	role, err := SpecialistRole(identity.Specialist)
	if err != nil {
		return err
	}
	if len(scope) == 0 {
		return fmt.Errorf("%w: the new scope names at least one path", ErrIdentity)
	}
	var staged []string
	if existing := f.staged[key]; existing != nil {
		staged = slices.Sorted(maps.Keys(existing.files))
	}
	for _, path := range staged {
		if !slices.Contains(scope, path) {
			return fmt.Errorf("%w: the new scope must keep staged path %q", ErrScopeDenied, path)
		}
	}
	owned := f.ownedScopeLocked(key)
	added := slices.DeleteFunc(slices.Clone(scope), func(path string) bool { return slices.Contains(owned, path) })
	bases := make(map[string]baseFile, len(added))
	for _, path := range added {
		if bases[path], err = f.physical(path); err != nil {
			return err
		}
	}
	if err = f.checkClaimScope(identity, added); err != nil {
		return err
	}
	if identity.AttemptID == "" {
		highest, _ := f.highestOpenAttempt(identity)
		identity.AttemptID = strconv.Itoa(highest + 1)
	}
	hash, err := f.invariantsVersionLocked(identity.Invariants)
	if err != nil {
		return err
	}
	d := f.ensureDelta(key)
	held.SessionID, held.WorkUnitID, held.AgentID, held.Specialist, held.Role = identity.SessionID, identity.WorkUnitID, identity.AgentID, identity.Specialist, role
	held.AttemptID, held.Invariants, held.InvariantsHash, held.Prelaunch = identity.AttemptID, identity.Invariants, hash, true
	f.bindings[key] = held
	f.releaseOwnershipLocked(key)
	for _, path := range scope {
		f.owners[path] = key
	}
	for _, path := range owned {
		if !slices.Contains(scope, path) {
			delete(d.bases, path)
		}
	}
	maps.Copy(d.bases, bases)
	d.verdicts = nil
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
		return identity, errIncompleteIdentity
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
		return identity, fmt.Errorf("%w: agent %q already holds a binding for this attempt of unit %q", ErrIdentity, identity.AgentID, identity.WorkUnitID)
	}
	return identity, nil
}

// attemptLocked returns the attempt this dispatch belongs to and the invariant
// set governing it. Delegated work carries the attempt its admission issued;
// otherwise, as for maintenance cycles, the store counts attempts itself. An
// attempt stays open while a binding of its unit holds file ownership, so a
// maintenance verifier joins the author it judges, while a retry after
// consolidation or discard opens the next attempt. Bindings and ownership are
// durable, so a restart neither restarts the count nor reuses an identity.
func (f *FS) attemptLocked(identity Identity) (string, InvariantSet) {
	// An attempt the delegation's admission already issued is authoritative:
	// the bind joins it, inheriting the invariant set it was opened with.
	if identity.AttemptID != "" {
		if existing, ok := f.joinedAttempt(identity); ok {
			return existing.AttemptID, existing.Invariants
		}
		return identity.AttemptID, identity.Invariants
	}
	highest, open := f.highestOpenAttempt(identity)
	if open.AttemptID != "" {
		return open.AttemptID, open.Invariants
	}
	return strconv.Itoa(highest + 1), identity.Invariants
}

// joinedAttempt finds an existing binding sharing identity's declared
// attempt, whose invariant set the join must inherit.
func (f *FS) joinedAttempt(identity Identity) (Identity, bool) {
	for _, existing := range f.bindings {
		if existing.sameAttempt(identity) {
			return existing, true
		}
	}
	return Identity{}, false
}

// highestOpenAttempt scans identity's unit's bindings for the highest
// attempt number seen, and, among bindings at that number, the one that
// still holds file ownership or awaits launch (an attempt stays open while its
// binding does, so every gate preassigned under one verifier unit shares it).
// A highest attempt with no owning or pending binding is not open: it settled.
func (f *FS) highestOpenAttempt(identity Identity) (int, Identity) {
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
		if n == highest && (owning[key] || existing.Prelaunch) {
			open = existing
		}
	}
	return highest, open
}

// errNoBinding refuses a key no binding holds.
func errNoBinding(key AgentID) error {
	return fmt.Errorf("%w: key %q has no active binding; its claim was released or its work discarded, so the orchestrator assigns the scope again with claim_assign and delegates the unit again", ErrIdentity, key)
}

// errIncompleteIdentity refuses an identity the coordinator should have filled in.
var errIncompleteIdentity = fmt.Errorf("%w: a binding names its session, work unit and agent", ErrIdentity)

// heldBindingLocked finds the binding identity's attempt already holds. The same
// scope answers with its key; another one is refused with the key that
// reassigns it. held is false when the attempt holds nothing yet.
func (f *FS) heldBindingLocked(identity Identity, scope []string) (AgentID, bool, error) {
	if !completeIdentity(identity) {
		return "", false, nil
	}
	identity.AttemptID, _ = f.attemptLocked(identity)
	for key, existing := range f.bindings {
		if !existing.sameAttempt(identity) || existing.AgentID != identity.AgentID || existing.GateAuthorKey != identity.GateAuthorKey {
			continue
		}
		if existing.Specialist != identity.Specialist {
			return "", true, fmt.Errorf("%w: unit %q holds %q for specialist %q, not %q; reassign it with claim_assign and that author_key", ErrScopeDenied, identity.WorkUnitID, key, existing.Specialist, identity.Specialist)
		}
		if err := RequireVFSCapability(identity.Specialist, model.VFSCapabilityBind); err != nil {
			return "", true, err
		}
		if len(scope) > 0 {
			if err := RequireVFSCapability(identity.Specialist, model.VFSCapabilityWrite); err != nil {
				return "", true, err
			}
		}
		if !sameScope(f.ownedScopeLocked(key), scope) {
			return "", true, fmt.Errorf("%w: unit %q already holds %q for this attempt with another scope; reassign it with claim_assign and that author_key", ErrScopeDenied, identity.WorkUnitID, key)
		}
		return key, true, nil
	}
	return "", false, nil
}

func completeIdentity(identity Identity) bool {
	return identity.SessionID != "" && identity.WorkUnitID != "" && identity.AgentID != ""
}
func (f *FS) duplicateIdentity(identity Identity) bool {
	for _, existing := range f.bindings {
		if existing.sameAttempt(identity) && existing.AgentID == identity.AgentID && existing.GateAuthorKey == identity.GateAuthorKey {
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
				// An agent meeting its own earlier claim on its unit is no
				// conflict between agents: it continues that work by its key.
				if held := f.bindings[owner]; held.AgentID != identity.AgentID || !held.sameUnit(identity) {
					f.recordCollision(identity, p, owner)
				}
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
// StagedState reports the revision and delta hash of the work staged under key,
// and whether any exists, so a binding that adopts existing work starts from it.
func (f *FS) StagedState(key AgentID) (revision uint64, deltaHash string, staged bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.revisionOf(key), f.deltaHashLocked(key), f.stagedLocked(key)
}

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
		return OperationResult{}, fmt.Errorf("%w: another author's staged view is read-only", ErrIdentity)
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
// legitimate view, a fresh call and the caller's revision. The call is consumed even when a later check rejects it.
func (f *FS) admitLocked(op Operation, view AgentID) (Identity, error) {
	identity, ok := f.bindings[op.Key]
	if !ok {
		return identity, errNoBinding(op.Key)
	}
	if op.CallID == "" {
		return identity, fmt.Errorf("%w: an operation names its call", ErrIdentity)
	}
	if view != op.Key {
		target, ok := f.bindings[view]
		if !ok {
			return identity, fmt.Errorf("%w: view %q is not an author binding", ErrIdentity, view)
		}
		if op.Action != OpRead {
			return identity, fmt.Errorf("%w: another author's staged view is read-only", ErrIdentity)
		}
		if err := RequireVFSCapability(identity.Specialist, model.VFSCapabilityVerify); err != nil {
			return identity, err
		}
		if !identity.verifies(view, target) {
			return identity, fmt.Errorf("%w: this gate is not assigned to the author whose view %q it reads", ErrIdentity, view)
		}
	}
	if err := f.consumeCallLocked(identity.SessionID, op.CallID); err != nil {
		return identity, err
	}
	if current := f.revisionOf(view); current != op.ExpectedRevision {
		return identity, fmt.Errorf("%w: current revision %d; re-read and issue a fresh call, or bind again to learn the current revision", ErrStaleRevision, current)
	}
	return identity, nil
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
		d.verdicts = nil
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
		// The verdict is about the author's staged work: it carries that revision.
		f.correlateLocked(start, verifier, author, callID)
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
	// The call is spent only by a verdict that lands: a refused one may be
	// attached again under the same call once its cause is gone.
	if err = f.consumeCallLocked(v.SessionID, callID); err != nil {
		return err
	}
	if d.verdicts == nil {
		d.verdicts = make(map[AgentID]*VerificationVerdict)
	}
	// One verdict per judge: its latest replaces its earlier one, whichever
	// delegation of it attached that.
	d.verdicts[v.AgentID] = &VerificationVerdict{Pass: pass, Finding: finding, VerifierID: v.AgentID, VerifierRole: v.Role, Revision: expected, DeltaHash: deltaHash, Invariants: a.Invariants, InvariantsHash: version}
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

// admitVerifierLocked resolves both bindings: the verifier must be the current
// gate entitled to judge the author, and never the author itself. The caller
// spends callID once the verdict is valid.
func (f *FS) admitVerifierLocked(verifier, author AgentID, callID string) (v, a Identity, err error) {
	v, vok := f.bindings[verifier]
	a, aok := f.bindings[author]
	if !vok || !aok || callID == "" {
		return v, a, fmt.Errorf("%w: a verdict names a bound verifier, a bound author and a call", ErrIdentity)
	}
	// The grant decides first: a caller without verify is told so, whatever
	// binding it names.
	if err = RequireVFSCapability(v.Specialist, model.VFSCapabilityVerify); err != nil {
		return v, a, err
	}
	if !v.verifies(author, a) {
		return v, a, fmt.Errorf("%w: verifier %q is not assigned to judge author %q", ErrIdentity, verifier, author)
	}
	if v.Superseded {
		return v, a, fmt.Errorf("%w: a later delegation of %q replaced gate %q over author %q; only that delegation attaches its verdict", ErrIdentity, v.AgentID, verifier, author)
	}
	if v.AgentID == a.AgentID {
		return v, a, ErrSelfVerification
	}
	return v, a, nil
}

// currentVerdictsLocked returns key's verdicts that still describe its staged
// work: same delta and same invariant set as when they were judged. A verdict
// whose invariants can no longer be read cannot be shown to be current.
func (f *FS) currentVerdictsLocked(key AgentID) []VerdictSummary {
	d := f.staged[key]
	if d == nil {
		return nil
	}
	hash := f.deltaHashLocked(key)
	var current []VerdictSummary
	for _, verifier := range slices.Sorted(maps.Keys(d.verdicts)) {
		v := d.verdicts[verifier]
		if v.DeltaHash != hash {
			continue
		}
		if version, err := f.invariantsVersionLocked(v.Invariants); err != nil || version != v.InvariantsHash {
			continue
		}
		current = append(current, VerdictSummary{VerifierKey: verifier, Pass: v.Pass, Finding: v.Finding})
	}
	return current
}

// consolidationAcceptedLocked lets ordinary work through unless a current
// verdict failed and the user has not accepted consolidating it anyway.
func (f *FS) consolidationAcceptedLocked(key AgentID, acceptFailing bool) error {
	if acceptFailing {
		return nil
	}
	return f.failingVerdictsLocked(key)
}

// failingVerdictsLocked refuses with every current failing finding, so the
// orchestrator and the user see each judge's report.
func (f *FS) failingVerdictsLocked(key AgentID) error {
	var findings []string
	for _, v := range f.currentVerdictsLocked(key) {
		if !v.Pass {
			findings = append(findings, fmt.Sprintf("%s: %s", v.VerifierKey, v.Finding))
		}
	}
	if len(findings) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrFailingVerdict, strings.Join(findings, "; "))
}

// ConsolidateCheckpoint is a trusted coordinator operation that consolidates
// authorized staged changes. Verdicts inform: a current failing one refuses
// unless acceptFailing carries the user's explicit acceptance, and a stale one
// is ignored. A maintenance cycle's current verdicts stay a gate that must pass.
// An empty checkpoint label defaults to the author key and revision.
func (f *FS) ConsolidateCheckpoint(key AgentID, checkpoint string, expected uint64, acceptFailing bool) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	if strings.TrimSpace(checkpoint) == "" {
		checkpoint = fmt.Sprintf("%s@%d", key, expected)
	}
	if _, ok := f.bindings[key]; !ok {
		return fmt.Errorf("%w: %q is not a known author key", ErrIdentity, key)
	}
	d := f.staged[key]
	if d == nil || len(d.files) == 0 {
		return ErrNothingStaged
	}
	if d.revision != expected {
		return fmt.Errorf("%w: the staged work is at revision %d, not %d; delegate a Verify of the author key again and consolidate against its revision", ErrStaleRevision, d.revision, expected)
	}
	// A maintenance cycle's verdict gates in consolidateLocked; ordinary
	// work asks for acceptance only over a current failing verdict.
	if !f.verdictRequiredLocked(key) {
		if err = f.consolidationAcceptedLocked(key, acceptFailing); err != nil {
			return err
		}
	}
	start := len(f.journal)
	defer func() {
		f.correlateLocked(start, key, key, "checkpoint/"+hashOf([]byte(checkpoint), true))
		f.finishLocked(&err)
	}()
	return f.consolidateLocked(key)
}
