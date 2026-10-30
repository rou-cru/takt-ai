package vfs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
)

// Repair describes one incomplete effect, not permission to bypass an operation.
// State binds consent to the manifest/snapshot, staged evidence and physical files.
type Repair struct {
	Kind     string   `json:"kind"`
	ID       string   `json:"id"`
	Author   AgentID  `json:"author"`
	Paths    []string `json:"paths"`
	State    string   `json:"state"`
	Preserve bool     `json:"preserve"`
	Reason   string   `json:"reason"`
}

// RepairApproval is private harness IPC from a concrete native confirmation.
// It cannot authorize the operation that is evaluated again after the repair.
type RepairApproval struct {
	Kind     string `json:"kind"`
	ID       string `json:"id"`
	State    string `json:"state"`
	Preserve bool   `json:"preserve"`
}

type interruptedEvidence struct {
	Manifest *recoveryManifest `json:",omitempty"`
	Cycle    *cycleSnapshot    `json:",omitempty"`
	Physical map[string]baseFile
	Bindings map[AgentID]Identity
	Deltas   map[AgentID]storedDelta
}

// StateHash is the hex sha256 of value's JSON encoding: the exact state a
// repair approval names.
func StateHash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (f *FS) repairEvidenceLocked(manifest *recoveryManifest, cycle *cycleSnapshot, authors []AgentID, paths []string) (interruptedEvidence, error) {
	e := interruptedEvidence{Manifest: manifest, Cycle: cycle, Physical: map[string]baseFile{}, Bindings: map[AgentID]Identity{}, Deltas: map[AgentID]storedDelta{}}
	for _, path := range paths {
		current, err := f.physical(path)
		if err != nil {
			return e, fmt.Errorf("repair cannot inspect %s: %w; retain the evidence and restore access to that file before requesting repair", path, err)
		}
		e.Physical[path] = current
	}
	for _, key := range authors {
		e.Bindings[key] = f.bindings[key]
		if d := f.staged[key]; d != nil {
			e.Deltas[key] = storedDelta{Files: d.files, Bases: d.bases, Verdicts: d.verdicts, Revision: d.revision}
		}
	}
	// The author may continue staged work after a manifest is closed. Archived
	// evidence must not share those mutable maps, slices or bindings.
	raw, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	var detached interruptedEvidence
	err = json.Unmarshal(raw, &detached)
	return detached, err
}

func (f *FS) recoveryRepairLocked() (*Repair, interruptedEvidence, error) {
	m := f.recovery
	if m == nil {
		return nil, interruptedEvidence{}, nil
	}
	paths := make([]string, 0, len(m.Items))
	for _, item := range m.Items {
		paths = append(paths, item.Path)
	}
	slices.Sort(paths)
	e, err := f.repairEvidenceLocked(m, nil, []AgentID{m.Agent}, paths)
	if err != nil {
		return nil, e, err
	}
	diverged := false
	for _, item := range m.Items[:m.Intent] {
		current := e.Physical[item.Path]
		if !sameFile(current, item.Before) && !sameFile(current, item.After) {
			diverged = true
		}
	}
	reason := "incomplete consolidation; approve restoring its pre-flush snapshot, then evaluate the original operation again"
	if diverged {
		reason = "incomplete consolidation diverged; approve keeping the current physical files and closing this effect as interrupted; manifest and staged evidence are retained"
	}
	return &Repair{Kind: "manifest", ID: StateHash(m), Author: m.Agent, Paths: paths, State: StateHash(e), Preserve: diverged, Reason: reason}, e, nil
}

// RecoveryRepair can diagnose an incomplete manifest while ordinary mutations
// remain blocked by readyLocked. It never clears that barrier.
func (f *FS) RecoveryRepair() (*Repair, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	r, _, err := f.recoveryRepairLocked()
	return r, err
}

// RepairRecovery revalidates the exact consent under the existing workspace
// lock. Safe restoration reuses Recover; divergent closure retains evidence.
func (f *FS) RepairRecovery(a RepairApproval) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fault != nil {
		return errors.Join(ErrStoreFailed, f.fault)
	}
	r, evidence, err := f.recoveryRepairLocked()
	if err != nil {
		return err
	}
	if r == nil || a.Kind != "manifest" || r.ID != a.ID || r.State != a.State || r.Preserve != a.Preserve {
		return errors.New("manifest repair approval is stale; no files changed; request approval for the current manifest and physical state")
	}
	if !r.Preserve {
		return f.recoverLocked()
	}
	f.interrupted["manifest/"+r.ID] = evidence
	f.recovery = nil
	f.appendJournalLocked(JournalEntry{Agent: r.Author, Operation: OpRecovery, Outcome: "interrupted"})
	f.finishLocked(&err)
	return err
}

func (f *FS) cycleRepairLocked(cycle, state string) (*Repair, interruptedEvidence, error) {
	snap, ok := f.cycles[cycle]
	if !ok || snap.Completed {
		return nil, interruptedEvidence{}, ErrUnknownCycle
	}
	pathsSet := map[string]bool{}
	for path := range snap.Items {
		pathsSet[path] = true
	}
	var authors []AgentID
	for key, b := range f.bindings {
		if b.CycleID == cycle {
			authors = append(authors, key)
			for _, path := range f.scopeLocked(key) {
				pathsSet[path] = true
			}
		}
	}
	paths := slices.Sorted(maps.Keys(pathsSet))
	slices.Sort(authors)
	e, err := f.repairEvidenceLocked(nil, snap, authors, paths)
	if err != nil {
		return nil, e, err
	}
	return &Repair{Kind: "cycle", ID: cycle, Author: snap.Agent, Paths: paths, State: StateHash(struct {
		Evidence interruptedEvidence
		State    string
	}{e, state}), Preserve: true, Reason: "maintenance rollback diverged; approve keeping current files and closing this exact cycle as interrupted, retaining snapshots and deltas without acceptance"}, e, nil
}

// CycleRepair describes only the divergent cycle encountered by normal abort.
func (f *FS) CycleRepair(cycle, state string) (*Repair, error) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	r, _, err := f.cycleRepairLocked(cycle, state)
	return r, err
}

// PreserveInterruptedCycle records the approved closure before releasing any
// ownership. If closure is interrupted, the archive makes it resumable.
func (f *FS) PreserveInterruptedCycle(cycle, state string, a RepairApproval) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	r, evidence, err := f.cycleRepairLocked(cycle, state)
	if err != nil {
		return err
	}
	if a.Kind != "cycle" || a.ID != r.ID || a.State != r.State || !a.Preserve {
		return errors.New("cycle repair approval is stale; request approval for the current cycle and physical state")
	}
	if _, _, err := f.planDiscard(f.cycles[cycle]); !errors.Is(err, ErrCycleDiverged) {
		return errors.New("cycle is no longer divergent; use its normal safe abort")
	}
	f.interrupted["cycle/"+cycle] = evidence
	delete(f.cycles, cycle)
	f.appendJournalLocked(JournalEntry{Agent: r.Author, CycleID: cycle, Operation: OpRecovery, Outcome: "interrupted"})
	f.finishLocked(&err)
	return err
}

// CycleInterrupted distinguishes approved interrupted closure from acceptance.
func (f *FS) CycleInterrupted(cycle string) bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	_, ok := f.interrupted["cycle/"+cycle]
	return ok
}
