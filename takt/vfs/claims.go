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
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/model"
)

// AssignScope atomically reserves explicit exclusive ownership for a target
// before its VFS session starts. The target instance must have explicit bind
// and write grants in the catalog; role alone never authorizes an assignment.
// A fresh assignment never takes a path another claim holds: any overlap, of
// this root or another, refuses before anything is captured. A refusal over
// another root's claim names its exit; continuing that work is ReassignScope.
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
	// A claim carries no attempt and no holder until its target adopts it.
	identity.AttemptID, identity.HostSession = "", ""
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
	for _, path := range scope {
		// Read every base before claiming anything, including invalid paths.
		if _, err = f.physical(path); err != nil {
			return "", err
		}
	}
	if err = f.admitReadsLocked(identity); err != nil {
		return "", err
	}
	if err = f.guardReadersLocked(identity, scope); err != nil {
		return "", err
	}
	// A request mixing free paths with any held path refuses whole before
	// anything is captured.
	for _, path := range scope {
		for claimed, owner := range f.owners {
			if !strings.EqualFold(path, claimed) {
				continue
			}
			if f.bindings[owner].SessionID == identity.SessionID {
				return "", f.collisionError(identity, path, owner)
			}
			return "", fmt.Errorf("%w; %s", f.collisionError(identity, path, owner), f.releaseExitLocked(identity.SessionID, owner, f.bindings[owner]))
		}
	}
	return f.claimLocked(identity, scope)
}

// claimLocked captures the base of every scoped path and registers identity as
// its exclusive owner under a fresh key.
func (f *FS) claimLocked(identity Identity, scope []string) (AgentID, error) {
	if err := f.guardInspectionClaimLocked(identity, "", scope); err != nil {
		return "", err
	}
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
	return f.revokeLocked(key)
}

func (f *FS) revokeLocked(key AgentID) error {
	identity, ok := f.bindings[key]
	if !ok {
		return fmt.Errorf("%w: %q is not a known binding key", ErrIdentity, key)
	}
	if f.stagedLocked(key) {
		return fmt.Errorf("%w (claim_key %s, staged paths [%s]); claim_assign with author_key %s continues it", ErrStagedWork, key, strings.Join(f.stagedPathsLocked(key), ", "), key)
	}
	identity.Prelaunch, identity.HostSession = false, ""
	f.bindings[key] = identity
	f.releaseOwnershipLocked(key)
	return nil
}

// ReassignScope gives the work of an existing author key a new exact scope for
// the target's next adoption, keeping the staged delta, and hands it to the unit, agent and
// specialist the request names. A scope that leaves out some of the delta's
// staged paths hands over only the paths it names, under a new key it returns;
// the rest stays with its work. Only the added paths are checked for collisions,
// and the paths that leave it were never written. The verdict, bound to the old
// revision, is cleared.
func (f *FS) ReassignScope(identity Identity, key AgentID, scope []string) (next AgentID, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return "", err
	}
	defer f.finishLocked(&err)
	// The key locates the staged work; whoever the request names takes it over.
	held, ok := f.bindings[key]
	if !ok || held.GateAuthorKey != "" {
		return "", fmt.Errorf("%w: %q is not an author key", ErrIdentity, key)
	}
	role, err := SpecialistRole(identity.Specialist)
	if err != nil {
		return "", err
	}
	if len(scope) == 0 {
		return "", fmt.Errorf("%w: the new scope names at least one path", ErrIdentity)
	}
	// Grants are the adoption's to check, but a target that is incomplete or
	// already holds a pending claim could never be told apart when it arrives.
	if !completeIdentity(identity) {
		return "", errIncompleteIdentity
	}
	identity.AttemptID, identity.HostSession, identity.Prelaunch = "", "", true
	if f.duplicateIdentity(identity, key) {
		return "", fmt.Errorf("%w: agent %q already holds a binding for unit %q", ErrIdentity, identity.AgentID, identity.WorkUnitID)
	}
	if err := f.guardInspectionClaimLocked(identity, key, scope); err != nil {
		return "", err
	}
	if err := f.admitReadsLocked(identity); err != nil {
		return "", err
	}
	if err := f.guardReadersLocked(identity, scope); err != nil {
		return "", err
	}
	owned := f.ownedScopeLocked(key)
	added := slices.DeleteFunc(slices.Clone(scope), func(path string) bool { return slices.Contains(owned, path) })
	if d := f.staged[key]; d != nil && slices.ContainsFunc(slices.Collect(maps.Keys(d.files)), func(path string) bool { return !slices.Contains(scope, path) }) {
		return f.handOverLocked(identity, key, scope, added)
	}
	bases := make(map[string]baseFile, len(added))
	for _, path := range added {
		if bases[path], err = f.physical(path); err != nil {
			return "", err
		}
	}
	if err = f.checkClaimScope(identity, added); err != nil {
		return "", err
	}
	hash, err := f.invariantsVersionLocked(identity.Invariants)
	if err != nil {
		return "", err
	}
	d := f.ensureDelta(key)
	held.SessionID, held.WorkUnitID, held.AgentID, held.Specialist, held.Role = identity.SessionID, identity.WorkUnitID, identity.AgentID, identity.Specialist, role
	// The work awaits adoption again: no attempt until the history admits the
	// unit and its target binds, and no holder until then.
	held.AttemptID, held.Invariants, held.InvariantsHash, held.Prelaunch, held.HostSession = "", identity.Invariants, hash, true, ""
	held.Reads = identity.Reads
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
	return key, nil
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
