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
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/model"
)

// The commands in this file serve a Caller: each resolves, under the store
// lock, which binding the caller holds, authorizes against it and acts in the
// same transaction. No binding identity, attempt or judged state is accepted
// from the caller.

var (
	// errNoHeldBinding refuses an author operation from a delegation holding no
	// author binding.
	errNoHeldBinding = fmt.Errorf("%w: no VFS binding in this delegation; call vfs_bind first", ErrIdentity)
	// errSeveralHeld refuses an operation that names no key while the
	// delegation holds more than one author binding.
	errSeveralHeld = fmt.Errorf("%w: this delegation holds several VFS bindings; name the author_key your vfs_bind returned", ErrIdentity)
	// errReadAgain refuses a verdict over a state the gate did not read.
	errReadAgain = fmt.Errorf("%w: verdict refused because staged work changed or was not read; read the author's files again with vfs_read, then call vfs_verify again", ErrInvalidVerdict)
	// errNotThisSession refuses consolidating, discarding or releasing a key no
	// binding has.
	errNotThisSession = fmt.Errorf("%w: author_key names no staged work; list claims with claim_list and use a key it reports", ErrIdentity)
	// errOrchestratorOnly refuses consolidation or discard by any other agent.
	errOrchestratorOnly = fmt.Errorf("%w: only the orchestrator consolidates or discards staged work; return the author_key to the orchestrator", ErrScopeDenied)
	// errStaleAcceptance refuses an acceptance given for another staged state.
	errStaleAcceptance = fmt.Errorf("%w: the staged work changed since the user accepted it; call vfs_consolidate again and ask the user about the state it refuses", ErrStaleRevision)
	// ErrNoAdmission is returned when a delegated unit binds or is given a gate
	// without an admitted delegation in the execution history.
	ErrNoAdmission = errors.New("vfs: no admitted delegation")
)

// errGateKey refuses closing a verifier gate's key in place of its author's.
func errGateKey(key, author AgentID) error {
	return fmt.Errorf("%w: %s is a verifier gate over author_key %s; name that author_key", ErrIdentity, key, author)
}

// errOtherRoot refuses closing work another root session staged, naming the
// claim_assign that continues it in the caller's root.
func errOtherRoot(key AgentID, b Identity, staged []string) error {
	return fmt.Errorf("%w: author_key %s belongs to root session %s (unit %s, agent %s); continue it in this session with claim_assign naming author_key %s and a scope that keeps its staged paths [%s], then vfs_consolidate or vfs_discard it", ErrIdentity, key, b.SessionID, b.WorkUnitID, b.Specialist, key, strings.Join(staged, ", "))
}

func errKeyNotHeld(key AgentID) error {
	return fmt.Errorf("%w: author_key %s is not held by this delegation; use the key your own vfs_bind returned, and when you hold none, return to the orchestrator", ErrIdentity, key)
}

// errForeignAuthor answers a read of another author's staged work with the
// memory entries the caller's own unit consumes, by ID.
func errForeignAuthor(key AgentID, invariants InvariantSet) error {
	var ids []string
	for _, invariant := range invariants {
		if id, ok := strings.CutPrefix(invariant, engramInvariantPrefix); ok {
			ids = append(ids, "#"+id)
		}
	}
	inputs := "consumes no memory entry: return to the orchestrator a blocker naming what you need"
	if len(ids) > 0 {
		inputs = "consumes memory entries " + strings.Join(ids, ", ") + ": read them with mem_get_observation, and return to the orchestrator a blocker naming what they leave unsettled"
	}
	return fmt.Errorf("%w: author_key %s is another unit's staged work, not an input of yours; your unit %s", ErrIdentity, key, inputs)
}

func errNotYourGate(key AgentID, gates []AgentID) error {
	authors := make([]string, len(gates))
	for i, gate := range gates {
		authors[i] = string(gate)
	}
	return fmt.Errorf("%w: author_key %s is not one of your gates: %s", ErrIdentity, key, strings.Join(authors, ", "))
}

func errUnadmitted(unit string) error {
	return fmt.Errorf("%w: work unit %s has no admitted delegation; the orchestrator delegates it before it binds", ErrNoAdmission, unit)
}

// heldLocked lists, in key order, the bindings c holds: its author bindings, or
// its gates (superseded ones included only when asked).
func (f *FS) heldLocked(c Caller, gates, superseded bool) []AgentID {
	var held []AgentID
	for _, key := range slices.Sorted(maps.Keys(f.bindings)) {
		b := f.bindings[key]
		if c.holds(b) && (b.GateAuthorKey != "") == gates && (superseded || !b.Superseded) {
			held = append(held, key)
		}
	}
	return held
}

// heldAuthorLocked resolves c's author binding: named is honored only when c
// holds it, and an empty name takes the single binding c holds.
func (f *FS) heldAuthorLocked(c Caller, named AgentID) (AgentID, error) {
	authors := f.heldLocked(c, false, false)
	switch {
	case named != "" && slices.Contains(authors, named):
		return named, nil
	case named != "" && len(authors) == 1 && f.bindings[named].AgentID != "":
		// Another author's staged work: the way forward is this unit's own inputs.
		return "", errForeignAuthor(named, f.bindings[authors[0]].Invariants)
	case named != "":
		return "", errKeyNotHeld(named)
	case len(authors) == 0:
		return "", errNoHeldBinding
	case len(authors) > 1:
		return "", errSeveralHeld
	}
	return authors[0], nil
}

// heldGateLocked resolves the gate c reads through: the gate over named, or
// else the gate whose author owns path, or else the first gate.
func (f *FS) heldGateLocked(c Caller, named AgentID, path string) (AgentID, error) {
	gates := f.heldLocked(c, true, false)
	judged := make([]AgentID, len(gates))
	for i, gate := range gates {
		judged[i] = f.bindings[gate].GateAuthorKey
	}
	if named != "" {
		if i := slices.Index(judged, named); i >= 0 {
			return gates[i], nil
		}
		return "", errNotYourGate(named, judged)
	}
	if i := slices.Index(judged, f.owners[path]); i >= 0 && f.owners[path] != "" {
		return gates[i], nil
	}
	return gates[0], nil
}

// resolveLocked returns the binding key c acts through and the staged view it
// reads or writes. A read by a caller holding gates goes through a gate,
// unless it names c's own author binding.
func (f *FS) resolveLocked(c Caller, named AgentID, path string, read bool) (key, view AgentID, err error) {
	c, err = f.currentCallerLocked(c, false)
	if err != nil {
		return "", "", err
	}
	if read && len(f.heldLocked(c, true, false)) > 0 && (named == "" || !slices.Contains(f.heldLocked(c, false, false), named)) {
		key, err = f.heldGateLocked(c, named, path)
		if err != nil {
			return "", "", err
		}
		return key, f.bindings[key].GateAuthorKey, nil
	}
	key, err = f.heldAuthorLocked(c, named)
	return key, key, err
}

// BindResult is what a bind issued: the key, the attempt and invariant-set
// version it belongs to, and the staged state it continues when it holds any.
type BindResult struct {
	Key               AgentID
	AttemptID         string
	InvariantsVersion string
	Staged            bool
	Revision          uint64
	DeltaHash         string
}

// BindAs binds c's delegated unit. It adopts the unit's prelaunch claim for
// c's host session, stamping the attempt the execution history admitted;
// otherwise it answers the binding c already holds, takes over the unit's
// binding of that attempt no host session holds, or claims scope afresh. A
// unit without an in-flight admission is refused.
func (f *FS) BindAs(c Caller, scope []string, invariants InvariantSet) (result BindResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return result, err
	}
	defer f.finishLocked(&err)
	c, err = f.currentCallerLocked(c, true)
	if err != nil {
		return result, err
	}
	attempt := c.AttemptID
	if err = f.guardMaintenanceLocked("bind", attempt, "", "", ""); err != nil {
		return result, err
	}
	identity := Identity{SessionID: c.SessionID, WorkUnitID: c.WorkUnitID, AttemptID: attempt, AgentID: c.Agent,
		Specialist: string(c.Agent), Invariants: invariants, HostSession: c.HostSession}
	key, err := f.bindHeldLocked(c, identity, scope)
	if err != nil {
		return result, err
	}
	bound := f.bindings[key]
	result = BindResult{Key: key, AttemptID: bound.AttemptID, InvariantsVersion: bound.InvariantsHash}
	if f.stagedLocked(key) {
		result.Staged, result.Revision, result.DeltaHash = true, f.revisionOf(key), f.deltaHashLocked(key)
	}
	return result, nil
}

func (f *FS) bindHeldLocked(c Caller, identity Identity, scope []string) (AgentID, error) {
	if key, adopted, err := f.adoptPrelaunchLocked(identity, scope); adopted || err != nil {
		return key, err
	}
	for _, key := range slices.Sorted(maps.Keys(f.bindings)) {
		b := f.bindings[key]
		if !c.holds(b) || b.GateAuthorKey != "" {
			continue
		}
		if len(scope) > 0 && !sameScope(f.ownedScopeLocked(key), scope) {
			return "", fmt.Errorf("%w: this delegation already holds %q with another scope; reassign it with claim_assign and that author_key", ErrScopeDenied, key)
		}
		return key, nil
	}
	return f.bindLocked(identity, scope)
}

// AdoptedGate is one gate a judge holds: its key, the author it judges, the
// author's scope, and why it could not be adopted when it was not.
type AdoptedGate struct {
	Key       AgentID  `json:"key"`
	AuthorKey AgentID  `json:"author_key"`
	Scope     []string `json:"scope"`
	Error     string   `json:"error,omitempty"`
}

// AdoptGates adopts every pending gate of c's root, unit and agent into c's
// host session and lists them with the gates that session already holds. A
// delegated gate takes the attempt the execution history admitted.
func (f *FS) AdoptGates(c Caller) (gates []AdoptedGate, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return nil, err
	}
	defer f.finishLocked(&err)
	c, err = f.currentCallerLocked(c, true)
	if err != nil {
		return nil, err
	}
	attempt := c.AttemptID
	gates = []AdoptedGate{}
	for _, key := range slices.Sorted(maps.Keys(f.bindings)) {
		gate := f.bindings[key]
		if gate.GateAuthorKey == "" || gate.Superseded || gate.SessionID != c.SessionID || gate.AgentID != c.Agent {
			continue
		}
		entry := AdoptedGate{Key: key, AuthorKey: gate.GateAuthorKey, Scope: f.scopeLocked(gate.GateAuthorKey)}
		if !gate.Prelaunch {
			if c.holds(gate) {
				gates = append(gates, entry)
			}
			continue
		}
		if gate.WorkUnitID != c.WorkUnitID {
			continue
		}
		if adoptErr := f.adoptGateLocked(key, c, attempt); adoptErr != nil {
			entry.Error = adoptErr.Error()
		}
		gates = append(gates, entry)
	}
	return gates, nil
}

func (f *FS) adoptGateLocked(key AgentID, c Caller, attempt string) error {
	gate := f.bindings[key]
	if gate.CycleID == "" {
		if attempt == "" || gate.AttemptID != "" && gate.AttemptID != attempt {
			return errUnadmitted(c.WorkUnitID)
		}
		gate.AttemptID = attempt
	}
	if err := f.requireAdoptionGrantsLocked(gate); err != nil {
		return err
	}
	gate.Prelaunch, gate.HostSession = false, c.HostSession
	f.bindings[key] = gate
	return nil
}

// CallerOperation is one read, write or deletion; AuthorKey optionally names
// the binding or, for a judge, the author it reads.
type CallerOperation struct {
	CallID    string
	Action    OperationType
	Path      string
	Content   []byte
	AuthorKey AgentID
}

// Operate applies op through the binding c holds, at the current revision. A
// gate read records the author's state it read and reports the paths the gate
// read under an earlier one.
func (f *FS) Operate(c Caller, op CallerOperation) (result OperationResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return result, err
	}
	switch op.Action {
	case OpRead, OpCreate, OpPatch, OpDelete:
	default:
		return result, fmt.Errorf("%w: unsupported operation %q", ErrIdentity, op.Action)
	}
	key, view, err := f.resolveLocked(c, op.AuthorKey, op.Path, op.Action == OpRead)
	if err != nil {
		return result, err
	}
	if err = f.guardMaintenanceLocked("op", "", string(op.Action), op.Path, key); err != nil {
		return result, err
	}
	revision, hash := f.revisionOf(view), f.deltaHashLocked(view)
	result, err = f.applyLocked(Operation{Key: key, CallID: op.CallID, Action: op.Action, Path: op.Path, Content: op.Content}, view, false)
	if err != nil || view == key {
		return result, err
	}
	result.OutdatedPaths = f.recordJudgedLocked(key, revision, hash, op.Path)
	f.finishLocked(&err)
	return result, err
}

// recordJudgedLocked records gate's read of path at its author's revision and
// hash, returning the paths read under an earlier author state.
func (f *FS) recordJudgedLocked(key AgentID, revision uint64, hash, path string) []string {
	gate := f.bindings[key]
	moved := gate.JudgedHash != hash || gate.JudgedRevision != revision
	var outdated []string
	if moved {
		outdated = slices.DeleteFunc(slices.Clone(gate.JudgedPaths), func(p string) bool { return p == path })
		gate.JudgedPaths = []string{path}
	} else if !slices.Contains(gate.JudgedPaths, path) {
		gate.JudgedPaths = append(gate.JudgedPaths, path)
	}
	gate.JudgedRevision, gate.JudgedHash = revision, hash
	f.bindings[key] = gate
	return outdated
}

// VerifyAs attaches c's verdict over author through the gate c holds over it,
// at the author state that gate last read. A gate that never read, or whose
// author changed since, is refused.
func (f *FS) VerifyAs(c Caller, callID string, author AgentID, pass bool, finding string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	c, err = f.currentCallerLocked(c, false)
	if err != nil {
		return err
	}
	var gate AgentID
	var judged []AgentID
	for _, key := range f.heldLocked(c, true, true) {
		b := f.bindings[key]
		judged = append(judged, b.GateAuthorKey)
		if b.GateAuthorKey == author && (gate == "" || !b.Superseded) {
			gate = key
		}
	}
	if gate == "" {
		return errNotYourGate(author, judged)
	}
	if err = f.guardMaintenanceLocked("verify", "", "", "", gate); err != nil {
		return err
	}
	return f.verifyJudgedLocked(gate, author, callID, pass, finding)
}

// verifyJudgedLocked attaches gate's verdict over author at the author state
// the gate last read; a gate that never read, or whose author changed since,
// is refused.
func (f *FS) verifyJudgedLocked(gate, author AgentID, callID string, pass bool, finding string) error {
	g := f.bindings[gate]
	if g.JudgedHash == "" || g.JudgedRevision != f.revisionOf(author) || g.JudgedHash != f.deltaHashLocked(author) {
		return errReadAgain
	}
	return f.verifyLocked(gate, author, callID, g.JudgedRevision, g.JudgedHash, pass, finding)
}

// orchestratorKeyLocked authorizes the orchestrator's act over author: the
// caller is the orchestrator holding capability, and author is an author
// binding of its root.
func (f *FS) orchestratorKeyLocked(c Caller, author AgentID, capability model.VFSCapability) error {
	if c.HostSession != c.SessionID {
		return errOrchestratorOnly
	}
	if err := f.guardMaintenanceLocked("consolidate", "", "", "", author); err != nil {
		return err
	}
	if c.Agent != OrchestratorInstance {
		return errOrchestratorOnly
	}
	if err := RequireVFSCapability(OrchestratorInstance, capability); err != nil {
		return err
	}
	return f.closableKeyLocked(c.SessionID, author)
}

// closableKeyLocked refuses an author key the orchestrator of session cannot
// close as it stands, naming what would make it closable.
func (f *FS) closableKeyLocked(session string, author AgentID) error {
	b, ok := f.bindings[author]
	switch {
	case !ok:
		return errNotThisSession
	case b.GateAuthorKey != "":
		return errGateKey(author, b.GateAuthorKey)
	case b.SessionID != session:
		return errOtherRoot(author, b, f.stagedPathsLocked(author))
	}
	return nil
}

// stagedPathsLocked lists the paths key's staged work changes, sorted.
func (f *FS) stagedPathsLocked(key AgentID) []string {
	d := f.staged[key]
	if d == nil {
		return nil
	}
	return slices.Sorted(maps.Keys(d.files))
}

// StagedState names one exact staged state of an author key.
type StagedState struct {
	Revision  uint64
	DeltaHash string
}

// FailingVerdictError refuses a consolidation over a current failing verdict,
// naming the staged state it refused so the user's acceptance can cover
// exactly that state.
type FailingVerdictError struct {
	StagedState
	err error
}

func (e *FailingVerdictError) Error() string { return e.err.Error() }
func (e *FailingVerdictError) Unwrap() error { return e.err }

// ConsolidateAs consolidates author's staged work at its current revision for
// the orchestrator of its root and returns the journal position of the last
// entry it wrote. accepted carries the user's acceptance of a failing verdict,
// valid only for the staged state the refusal named.
func (f *FS) ConsolidateAs(c Caller, author AgentID, checkpoint string, accepted *StagedState) (seq int, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return 0, err
	}
	if err = f.orchestratorKeyLocked(c, author, model.VFSCapabilityConsolidate); err != nil {
		return 0, err
	}
	current := StagedState{Revision: f.revisionOf(author), DeltaHash: f.deltaHashLocked(author)}
	if accepted != nil && *accepted != current {
		return 0, errStaleAcceptance
	}
	err = f.consolidateCheckpointLocked(author, checkpoint, current.Revision, accepted != nil)
	if errors.Is(err, ErrFailingVerdict) {
		return 0, &FailingVerdictError{StagedState: current, err: err}
	}
	if err != nil {
		return 0, err
	}
	return len(f.journal) - 1, nil
}

// DiscardAs discards author's staged work for the orchestrator of its root,
// releasing the paths it owns.
func (f *FS) DiscardAs(c Caller, author AgentID) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	if err = f.orchestratorKeyLocked(c, author, model.VFSCapabilityDiscard); err != nil {
		return err
	}
	start := len(f.journal)
	defer func() {
		f.correlateLocked(start, author, author, "")
		f.finishLocked(&err)
	}()
	f.rollbackLocked(author)
	return nil
}

// ReleaseClaim frees the paths claim key holds for session's orchestrator. A
// claim holding staged work is refused, and so is a claim without staged work
// whose unit inFlight reports still running: release needs the unit's
// effective termination. inFlight runs under the FS lock with the claim's own
// identity and returns the running unit's dispatch, which the refusal names.
func (f *FS) ReleaseClaim(session string, key AgentID, inFlight func(Identity) (dispatch string, running bool)) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	defer f.finishLocked(&err)
	b, ok := f.bindings[key]
	if !ok {
		return errNotThisSession
	}
	if f.stagedLocked(key) {
		if b.SessionID != session {
			return errors.Join(ErrStagedWork, errOtherRoot(key, b, f.stagedPathsLocked(key)))
		}
	} else if dispatch, running := inFlight(b); running {
		return fmt.Errorf("%w: its unit %s (agent %s, root %s) is still running under dispatch %s; wait for its delivery or end it with dispatch_abandon dispatch %s, then claim_release again", ErrScopeDenied, b.WorkUnitID, b.AgentID, b.SessionID, dispatch, dispatch)
	}
	return f.revokeLocked(key)
}
