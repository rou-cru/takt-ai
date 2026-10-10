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
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"

	"golang.org/x/sys/unix"
)

// discardedOutcome marks journal entries of a cycle undone in full.
const discardedOutcome = "discarded"

// cycleSnapshot retains what one maintenance cycle overwrote on disk.
type cycleSnapshot struct {
	MandateClass string
	// Agent is the last agent that consolidated for the cycle, so the restore
	// carries the cycle's own attribution (PR-MNT-31).
	Agent AgentID
	// Items map each path to what the cycle found (Before) and left (After).
	// Before is the state before the cycle's *first* consolidation: a cycle
	// consolidates more than one delta (PR-MNT-25) and discard targets all of it.
	Items map[string]recoveryItem
	// Dirs are the directories the cycle's flushes created, shallowest first.
	Dirs []string
	// Last is the journal position of the cycle's latest consolidation of each
	// path, so the cycle that last re-edited a path is the one it is traced to.
	Last map[string]int `json:",omitempty"`
	// Completed marks a cycle CompleteCycle has closed: it can no longer be
	// discarded, but ConsolidatedBy still attributes its paths so a later
	// re-edit traces back to this cycle and mandate class (PR-MNT-31).
	Completed bool
}

// retainCycleLocked keeps what a maintenance cycle just overwrote so the cycle stays discardable.
func (f *FS) retainCycleLocked(agent AgentID, m *recoveryManifest) {
	id, bound := f.bindings[agent]
	if !bound || id.CycleID == "" {
		return
	}
	snap, ok := f.cycles[id.CycleID]
	if !ok {
		snap = &cycleSnapshot{MandateClass: id.MandateClass, Items: make(map[string]recoveryItem)}
		f.cycles[id.CycleID] = snap
	}
	snap.Agent = agent
	if snap.Last == nil {
		snap.Last = make(map[string]int)
	}
	for _, item := range m.Items {
		snap.Last[item.Path] = len(f.journal)
		// A path this cycle already consolidated keeps its pre-cycle state:
		// the target is where the workspace stood before the cycle, not before
		// its latest delta.
		if prior, seen := snap.Items[item.Path]; seen {
			item.Before = prior.Before
		}
		snap.Items[item.Path] = item
	}
	for _, dir := range m.Dirs {
		if !slices.Contains(snap.Dirs, dir) {
			snap.Dirs = append(snap.Dirs, dir)
		}
	}
	slices.SortFunc(snap.Dirs, func(a, b string) int { return cmp.Compare(len(a), len(b)) })
}

// CompleteCycle releases cycleID's discard state after it closed without
// regressing acceptance, so DiscardCycle can no longer undo it. Idempotent.
// ConsolidatedBy still attributes the cycle's paths (PR-MNT-31).
func (f *FS) CompleteCycle(cycleID string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	defer f.finishLocked(&err)
	if snap, ok := f.cycles[cycleID]; ok {
		snap.Completed = true
	}
	return nil
}

// ConsolidatedBy reports the cycle and mandate class that last consolidated
// path across every retained cycle (open or completed), so a caller staging a
// write can attribute a later re-edit back to it (PR-MNT-31). path must match
// the slash-relative form JournalEntry.Path and recoveryItem.Path use. It
// scans every retained cycle per call; index by path if that ever grows.
func (f *FS) ConsolidatedBy(path string) (cycleID, mandateClass string, ok bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.consolidatedByLocked(path)
}

// consolidatedByLocked is ConsolidatedBy for a caller that already holds f.mu
// (locked for read or write) — appendJournalLocked runs under the write lock
// and must not re-enter it via the exported, self-locking method.
func (f *FS) consolidatedByLocked(path string) (cycleID, mandateClass string, ok bool) {
	latest := -1
	for _, id := range slices.Sorted(maps.Keys(f.cycles)) {
		snap := f.cycles[id]
		if _, present := snap.Items[path]; !present {
			continue
		}
		// Several cycles may have consolidated the path: the latest one is its
		// author. A store written before positions were kept ties at zero and
		// falls back to the cycle identities' order, never to map order.
		if at := snap.Last[path]; at >= latest {
			latest, cycleID, mandateClass, ok = at, id, snap.MandateClass, true
		}
	}
	return cycleID, mandateClass, ok
}

// DiscardCycle restores the workspace to the state before cycleID's
// consolidation, refusing to overwrite a path that changed since then.
func (f *FS) DiscardCycle(cycleID string) (restored []string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return nil, err
	}
	defer f.finishLocked(&err)
	snap, ok := f.cycles[cycleID]
	if !ok || snap.Completed {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCycle, cycleID)
	}
	m, restored, err := f.planDiscard(snap)
	if err != nil {
		return nil, err
	}
	if err = f.applyDiscard(snap, cycleID, m); err != nil {
		return nil, err
	}
	delete(f.cycles, cycleID)
	return restored, nil
}

func (f *FS) applyDiscard(snap *cycleSnapshot, cycleID string, m *recoveryManifest) error {
	return f.applyFlush(m, flushHooks{
		preparedLabel: "discard/prepared",
		verifiedLabel: "discard/verified",
		journalEntry: func(item recoveryItem) JournalEntry {
			return JournalEntry{
				Agent: snap.Agent, Path: item.Path, Operation: OpFlush, Outcome: discardedOutcome,
				CycleID: cycleID, MandateClass: snap.MandateClass,
				BeforeHash: hashOf(item.Before.Content, item.Before.Present),
				AfterHash:  hashOf(item.After.Content, item.After.Present),
			}
		},
		postStep: func(m *recoveryManifest) error { return f.cleanupCycleLocked(m, snap) },
	})
}

// planDiscard turns the snapshot into a flush manifest that puts the pre-cycle
// state back, after proving every path still holds the cycle's own output.
// Nothing is touched until the whole plan checks out.
func (f *FS) planDiscard(snap *cycleSnapshot) (*recoveryManifest, []string, error) {
	m := &recoveryManifest{Agent: snap.Agent}
	var restored []string
	for _, rel := range slices.Sorted(maps.Keys(snap.Items)) {
		item := snap.Items[rel]
		if err := f.checkBase(rel, item.After); err != nil {
			return nil, nil, errors.Join(ErrCycleDiverged, err)
		}
		// Swapped on purpose: the flush machinery checks Before and writes
		// After, so undoing the cycle is flushing its own reverse.
		m.Items = append(m.Items, recoveryItem{rel, tempName(rel), item.After, item.Before})
		if item.Before.Present {
			if err := f.noteMissingDirs(m, rel); err != nil {
				return nil, nil, err
			}
		}
		restored = append(restored, rel)
	}
	slices.SortFunc(m.Dirs, func(a, b string) int { return len(a) - len(b) })
	return m, restored, nil
}

// cleanupCycleLocked drops the temporaries and directories the cycle created.
func (f *FS) cleanupCycleLocked(m *recoveryManifest, snap *cycleSnapshot) error {
	for _, item := range m.Items {
		if err := f.removeIfPresentLocked(item.Temp); err != nil {
			return err
		}
	}
	for _, dir := range slices.Backward(snap.Dirs) {
		if err := f.removeIfPresentLocked(dir); err != nil && !errors.Is(err, unix.ENOTEMPTY) {
			return err
		}
	}
	return nil
}
