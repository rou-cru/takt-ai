package vfs

import (
	"fmt"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/model"
)

// currentCallerLocked resolves identity from the execution linkage. An absent
// link can inspect unbound context, but can never adopt, read or mutate a key.
func (f *FS) currentCallerLocked(c Caller, required bool) (Caller, error) {
	if c.Agent == OrchestratorInstance && c.HostSession == c.SessionID {
		return c, nil
	}
	p, err := history.ReadProjection(f.stateDir)
	if err != nil {
		return c, err
	}
	d, ok := p.ExecutionForChild(c.HostSession, string(c.Agent))
	if !ok || d.Terminated {
		c.AttemptID = ""
		if required {
			return c, errUnadmitted(c.WorkUnitID)
		}
		c.WorkUnitID = ""
		return c, nil
	}
	if (c.Dispatch != "" && c.Dispatch != d.Dispatch) || (c.AttemptID != "" && c.AttemptID != d.Attempt) || c.SessionID != d.Root || (c.WorkUnitID != "" && c.WorkUnitID != d.Unit) {
		return c, fmt.Errorf("%w: caller does not execute root %s unit %s dispatch %s; use its current delegation", ErrIdentity, d.Root, d.Unit, d.Dispatch)
	}
	if d.Cycle == "" {
		if !d.Ready {
			return c, fmt.Errorf("launch preparation for dispatch %s is incomplete; return to the orchestrator and retry the same subagent call", d.Dispatch)
		}
		u, _ := p.Unit(d.Root, d.Unit)
		if u.State != history.StateInFlight || u.AttemptID != d.Attempt || u.Dispatch != d.Dispatch || u.Agent != d.Agent {
			return c, errUnadmitted(d.Unit)
		}
	}
	c.WorkUnitID, c.AttemptID, c.Dispatch = d.Unit, d.Attempt, d.Dispatch
	return c, nil
}

// PrepareDelegation validates live claims/authors and issues all gates as one
// core mutation. Failure restores every binding, including superseded gates.
// The caller holds the same workspace lock as the admission/history writer.
func (f *FS) PrepareDelegation(identity Identity, authors []AgentID, needsClaim bool, admit func() (string, error)) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	if needsClaim {
		matched := false
		for _, b := range f.bindings {
			if b.Prelaunch && b.GateAuthorKey == "" && b.SessionID == identity.SessionID && b.WorkUnitID == identity.WorkUnitID && b.AgentID == identity.AgentID {
				if !slices.Equal(b.Invariants, identity.Invariants) {
					return fmt.Errorf("launch refused: inputs for unit %s changed after claim assignment; call claim_assign with author_key and the exact scope, then retry delegation", identity.WorkUnitID)
				}
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%w: launch refused: no pending claim for root %s unit %s agent %s; call claim_assign for that exact unit and scope, then retry the same subagent call", ErrScopeDenied, identity.SessionID, identity.WorkUnitID, identity.AgentID)
		}
	}
	prepared := make(map[AgentID]Identity)
	if authors != nil {
		if len(authors) == 0 {
			return fmt.Errorf("%w: author_keys must list unique nonempty staged author keys; retry the same subagent call with its authors", ErrIdentity)
		}
		if err = RequireVFSCapability(identity.Specialist, model.VFSCapabilityVerify); err != nil {
			return err
		}
		seen := map[AgentID]bool{}
		for _, key := range authors {
			if strings.TrimSpace(string(key)) == "" || seen[key] {
				return fmt.Errorf("%w: author_keys must list unique nonempty staged author keys", ErrIdentity)
			}
			seen[key] = true
			judge := identity
			if len(judge.Invariants) == 0 {
				judge.Invariants = f.bindings[key].Invariants
			}
			judge, err = f.bindableIdentity(judge, nil)
			if err != nil {
				return err
			}
			if err = f.validateDelegatedAuthorLocked(judge, key); err != nil {
				return fmt.Errorf("%w; call claim_assign with author_key %s to continue that work under root %s, then retry the same subagent call", err, key, identity.SessionID)
			}
			prepared[key] = judge
		}
	}
	if admit != nil {
		// The workspace lock stays held. The callback may unwind core GC
		// state on this same handle before history issues the attempt.
		f.mu.Unlock()
		identity.AttemptID, err = admit()
		f.mu.Lock()
		if err != nil {
			return err
		}
		// The admission may have unwound a cycle's bindings: every author is
		// validated again against the store as it now stands.
		for key, judge := range prepared {
			if err = f.validateDelegatedAuthorLocked(judge, key); err != nil {
				return err
			}
		}
	}
	// A replay after an interrupted preparation may declare a different set.
	// The core owns that set: never leave surplus gates from the same attempt.
	for key, b := range f.bindings {
		if b.Prelaunch && b.GateAuthorKey != "" && b.SessionID == identity.SessionID && b.WorkUnitID == identity.WorkUnitID && b.AttemptID == identity.AttemptID && b.AgentID == identity.AgentID {
			if _, wanted := prepared[b.GateAuthorKey]; !wanted {
				delete(f.bindings, key)
				delete(f.staged, key)
			}
		}
	}
	for _, key := range authors {
		judge := prepared[key]
		judge.AttemptID = identity.AttemptID
		judge.GateAuthorKey = key
		judge.Prelaunch, judge.HostSession = true, ""
		f.issueVerifierLocked(judge, key)
	}
	f.finishLocked(&err)
	return err
}

// CancelPreparedDelegation releases only reservations and shell preparations
// issued for this attempt. Clean implementation claims remain reusable; no
// staged work is discarded.
func (f *FS) CancelPreparedDelegation(root, unit, attempt string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	for key, b := range f.bindings {
		if b.SessionID == root && b.WorkUnitID == unit && b.AttemptID == attempt && b.Prelaunch && b.GateAuthorKey != "" {
			delete(f.bindings, key)
			delete(f.staged, key)
		}
	}
	// ponytail: unit-less orchestrator shells are released by the host when its
	// session ends; a host crash leaves those few until the workspace is reset.
	for index, call := range f.shellCalls {
		if call.Inspection == nil && call.Caller.SessionID == root && call.Caller.WorkUnitID == unit && call.Caller.AttemptID == attempt {
			delete(f.shellCalls, index)
		}
	}
	f.finishLocked(&err)
	return err
}

// validateDelegatedAuthorLocked checks a delegated gate's author: a
// maintenance collector is judged only by its own cycle's verifier.
func (f *FS) validateDelegatedAuthorLocked(judge Identity, key AgentID) error {
	if f.bindings[key].CycleID != "" {
		return fmt.Errorf("%w: author_key %s is maintenance work, judged only within its cycle", ErrIdentity, key)
	}
	return f.validateVerifierAuthorLocked(judge, key)
}

// StateDirectory locates the shared execution history for the locked handle.
func (f *FS) StateDirectory() string { return f.stateDir }
