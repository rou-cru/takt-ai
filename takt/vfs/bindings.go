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
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/model"
)

// Identity is supplied by the trusted coordinator's dispatch.
type Identity struct {
	SessionID  string
	WorkUnitID string
	// AttemptID is never chosen by the caller. A delegated binding carries none
	// while prelaunch and the attempt the execution history admitted once
	// adopted; a maintenance binding joins the attempt a live binding of its
	// unit holds open, or opens the next one (PR-DAG-REP-3).
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
	// HostSession is the host session holding the binding. Adoption stamps it;
	// a reassignment, a supersession or a release clears it. Empty in stores
	// written before it existed.
	HostSession string `json:",omitempty"`
	// JudgedRevision, JudgedHash and JudgedPaths record a gate's last read of
	// its author's staged view: the author's revision and delta hash at that
	// read, and the paths read under that same state.
	JudgedRevision uint64   `json:",omitempty"`
	JudgedHash     string   `json:",omitempty"`
	JudgedPaths    []string `json:",omitempty"`
	// Reads are the selectors the orchestrator stated for what this unit may
	// read beyond its own claim; empty means its own claim only.
	Reads []string `json:",omitempty"`
}

// Caller is the harness-delivered identity of the process running one VFS
// command. No field is ever model-supplied.
type Caller struct {
	Dispatch string
	// SessionID is the root session.
	SessionID string
	// HostSession is the host session running the tool: the delegated child,
	// or the root for the orchestrator.
	HostSession string
	// Agent is the host-delivered agent name.
	Agent AgentID
	// WorkUnitID is the unit the caller executes.
	WorkUnitID string
	AttemptID  string
}

// holds reports whether c holds b: same root, host session and agent.
func (c Caller) holds(b Identity) bool {
	return c.HostSession != "" && b.HostSession == c.HostSession && b.SessionID == c.SessionID && b.AgentID == c.Agent && b.WorkUnitID == c.WorkUnitID && b.AttemptID == c.AttemptID && !b.Prelaunch
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
	return f.bindLocked(identity, scope)
}

// bindLocked adopts identity's prelaunch assignment, answers the binding its
// attempt already holds, or claims scope under a fresh key.
func (f *FS) bindLocked(identity Identity, scope []string) (AgentID, error) {
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
	identity, err := f.bindableIdentity(identity, scope)
	if err != nil {
		return "", err
	}
	if err = f.checkScope(identity, scope); err != nil {
		return "", err
	}
	return f.claimLocked(identity, scope)
}

// adoptPrelaunchLocked adopts the prelaunch assignment matching identity, if
// any, for identity.HostSession. It reports adopted=false when no assignment
// matches. A delegated assignment carries no attempt: adoption stamps the one
// identity brings, which the execution history issued.
func (f *FS) adoptPrelaunchLocked(identity Identity, scope []string) (AgentID, bool, error) {
	for assignedKey, assigned := range f.bindings {
		if !assigned.Prelaunch || assigned.SessionID != identity.SessionID || assigned.WorkUnitID != identity.WorkUnitID || assigned.AgentID != identity.AgentID {
			continue
		}
		// A verifier unit holds one gate per author it judges; a gate preassigned
		// to another author, or a bind that names no author, is not this gate's
		// adoption.
		if assigned.GateAuthorKey != "" && assigned.GateAuthorKey != identity.GateAuthorKey {
			continue
		}
		if assigned.Specialist != identity.Specialist || assigned.GateAuthorKey != identity.GateAuthorKey ||
			(assigned.CycleID != "" && identity.AttemptID != "" && assigned.AttemptID != identity.AttemptID) ||
			(len(identity.Invariants) > 0 && !slices.Equal(assigned.Invariants, identity.Invariants)) ||
			(len(scope) > 0 && !sameScope(f.ownedScopeLocked(assignedKey), scope)) {
			return "", true, ErrScopeDenied
		}
		if err := f.requireAdoptionGrantsLocked(assigned); err != nil {
			return "", true, err
		}
		if assigned.CycleID == "" {
			assigned.AttemptID = identity.AttemptID
			if f.duplicateIdentity(assigned, assignedKey) {
				return "", true, fmt.Errorf("%w: agent %q already holds a binding for attempt %q of unit %q", ErrIdentity, identity.AgentID, identity.AttemptID, identity.WorkUnitID)
			}
		}
		assigned.Prelaunch, assigned.HostSession = false, identity.HostSession
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
	return f.assignVerifierLocked(identity, authorKey)
}

func (f *FS) assignVerifierLocked(identity Identity, authorKey AgentID) (key AgentID, err error) {
	if !completeIdentity(identity) || identity.Specialist == "" || authorKey == "" {
		return "", fmt.Errorf("%w: a verifier gate names its session, unit, agent and specialist, and the author_key it judges", ErrIdentity)
	}
	for _, capability := range []model.VFSCapability{model.VFSCapabilityRead, model.VFSCapabilityVerify} {
		if err = RequireVFSCapability(identity.Specialist, capability); err != nil {
			return "", err
		}
	}
	identity.GateAuthorKey = authorKey
	identity.Prelaunch, identity.HostSession = true, ""

	if identity.AttemptID != "" {
		if pending, ok := f.pendingGateLocked(identity, authorKey); ok {
			return pending, nil
		}
	}
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
	return f.issueVerifierLocked(identity, authorKey), nil
}

func (f *FS) issueVerifierLocked(identity Identity, authorKey AgentID) AgentID {
	if identity.AttemptID != "" {
		if key, ok := f.pendingGateLocked(identity, authorKey); ok {
			return key
		}
	}

	// A new assignment of this judge over this author supersedes every earlier
	// gate of it, adopted or not and under any unit: the judge speaks through
	// its latest delegation only, so a run it replaced cannot overwrite its
	// verdict, and a pending one is never adopted by map iteration order.
	for previousKey, previous := range f.bindings {
		if previous.SessionID == identity.SessionID && previous.AgentID == identity.AgentID &&
			previous.GateAuthorKey == authorKey && !previous.Superseded {
			previous.Prelaunch, previous.Superseded, previous.HostSession = false, true, ""
			f.bindings[previousKey] = previous
		}
	}
	key := AgentID(fmt.Sprintf("%x", randomID()))
	f.bindings[key] = identity
	f.ensureDelta(key)
	return key
}

func (f *FS) pendingGateLocked(identity Identity, authorKey AgentID) (AgentID, bool) {
	for key, gate := range f.bindings {
		if gate.Prelaunch && !gate.Superseded && gate.sameUnit(identity) && gate.AgentID == identity.AgentID && gate.Specialist == identity.Specialist && gate.GateAuthorKey == authorKey && (identity.AttemptID == "" || gate.AttemptID == identity.AttemptID) {
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
	identity.Invariants = f.attemptInvariantsLocked(identity)
	if identity.InvariantsHash, err = f.invariantsVersionLocked(identity.Invariants); err != nil {
		return identity, err
	}
	if f.duplicateIdentity(identity, "") {
		return identity, fmt.Errorf("%w: agent %q already holds a binding for this attempt of unit %q", ErrIdentity, identity.AgentID, identity.WorkUnitID)
	}
	return identity, nil
}

// attemptInvariantsLocked inherits invariant references from the same issued
// attempt. The attempt itself is always supplied by execution history.
func (f *FS) attemptInvariantsLocked(identity Identity) InvariantSet {
	if identity.AttemptID != "" {
		if existing, ok := f.joinedAttempt(identity); ok {
			return existing.Invariants
		}
	}
	return identity.Invariants
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
	for key, existing := range f.bindings {
		if !f.sameLiveIdentityLocked(key, existing, identity) {
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
		if !sameScope(f.ownedScopeLocked(key), scope) || !sameScope(existing.Reads, identity.Reads) {
			return "", true, fmt.Errorf("%w: unit %q already holds %q for this attempt with another scope; reassign it with claim_assign and that author_key", ErrScopeDenied, identity.WorkUnitID, key)
		}
		return key, true, nil
	}
	return "", false, nil
}

func completeIdentity(identity Identity) bool {
	return identity.SessionID != "" && identity.WorkUnitID != "" && identity.AgentID != ""
}

// duplicateIdentity reports whether a binding other than self already holds
// identity's attempt for the same agent and author link.
func (f *FS) duplicateIdentity(identity Identity, self AgentID) bool {
	for key, existing := range f.bindings {
		if key != self && f.sameLiveIdentityLocked(key, existing, identity) {
			return true
		}
	}
	return false
}

// sameLiveIdentityLocked reports whether existing (under key) holds identity's
// attempt for the same agent and author link. A delegated binding's attempt is
// empty while prelaunch, or the one the history issued; an empty attempt that
// is neither prelaunch nor owning paths was released before adoption and
// holds nothing. A superseded gate holds nothing either, and a delegated gate
// assignment supersedes the judge's earlier gate over its author instead of
// colliding with it.
func (f *FS) sameLiveIdentityLocked(key AgentID, existing, identity Identity) bool {
	if !existing.sameAttempt(identity) || existing.AgentID != identity.AgentID || existing.GateAuthorKey != identity.GateAuthorKey || existing.Superseded {
		return false
	}
	if identity.Prelaunch && identity.CycleID == "" && identity.GateAuthorKey != "" {
		return false
	}
	return existing.AttemptID != "" || existing.Prelaunch || len(f.ownedScopeLocked(key)) > 0
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
