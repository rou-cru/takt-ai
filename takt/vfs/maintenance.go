package vfs

import (
	"fmt"
	"slices"
)

// PendingOrdinaryDeltas counts unresolved ordinary deltas across the workspace,
// not just the requesting session or the proposed maintenance scope.
func (f *FS) PendingOrdinaryDeltas() int {
	return len(f.PendingOrdinaryDeltaIdentities())
}

// PendingOrdinaryDeltaIdentities names the identity behind each unresolved
// ordinary delta PendingOrdinaryDeltas counts, so a stalled barrier can be
// explained instead of just measured.
func (f *FS) PendingOrdinaryDeltaIdentities() []string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	var out []string
	for key, delta := range f.staged {
		id := f.bindings[key]
		if id.CycleID == "" && len(delta.files) > 0 {
			out = append(out, fmt.Sprintf("session=%s unit=%s agent=%s", id.SessionID, id.WorkUnitID, key))
		}
	}
	slices.Sort(out)
	return out
}

// StagedView is the exact candidate given to an independent verifier.
type StagedView struct {
	Revision uint64             `json:"revision"`
	Hash     string             `json:"delta_hash"`
	Files    map[string]*string `json:"files"`
}

// InspectDelta returns a detached view; nil contents mean deletion.
func (f *FS) InspectDelta(key AgentID) StagedView {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := StagedView{Revision: f.revisionOf(key), Hash: f.deltaHashLocked(key), Files: map[string]*string{}}
	if d := f.staged[key]; d != nil {
		for p, b := range d.files {
			if b == nil {
				out.Files[p] = nil
			} else {
				s := string(b)
				out.Files[p] = &s
			}
		}
	}
	return out
}

// DropCycleStaging discards only bindings owned by the specified cycle.
// Consolidated content must separately pass DiscardCycle's divergence checks.
func (f *FS) DropCycleStaging(cycle string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	defer f.finishLocked(&err)
	var keys []AgentID
	for k, id := range f.bindings {
		if id.CycleID == cycle {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)
	for _, k := range keys {
		f.rollbackLocked(k)
		delete(f.bindings, k)
		delete(f.staged, k)
	}
	return nil
}
