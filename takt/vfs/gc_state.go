package vfs

import (
	"encoding/json"
	"errors"
	"slices"
)

// Maintenance is stored with bindings, shell calls and retained rollback state,
// not in a second coordinator file. The GC algorithm owns no durable copy.
func (f *FS) Maintenance() json.RawMessage {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return slices.Clone(f.maintenance)
}
func (f *FS) SetMaintenance(raw json.RawMessage) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	if !json.Valid(raw) {
		return errors.New("gc: malformed maintenance state refused; retry the same coordination call")
	}
	f.maintenance = slices.Clone(raw)
	f.finishLocked(&err)
	return err
}

// guardMaintenanceLocked applies the maintenance barrier and the active
// cycle's lane rules to one command acting through key. attempt is the bind's
// history-issued attempt: a unit already admitted keeps binding during the
// barrier, since the cycle waits for exactly those units (PR-MNT-3).
func (f *FS) guardMaintenanceLocked(command, attempt, action, path string, key AgentID) error {
	var state struct {
		Cycle *struct {
			Plan struct {
				CycleID string `json:"cycle_id"`
			} `json:"plan"`
			Phase string `json:"phase"`
		} `json:"cycle"`
	}
	if len(f.maintenance) > 0 {
		if err := json.Unmarshal(f.maintenance, &state); err != nil {
			return err
		}
	}
	if command == "bind" && state.Cycle != nil && attempt == "" {
		return errors.New("gc: barrier holds new bindings; retry vfs_bind after maintenance ends")
	}
	b, ok := f.bindings[key]
	if !ok || b.CycleID == "" {
		return nil
	}
	if state.Cycle == nil || state.Cycle.Plan.CycleID != b.CycleID {
		return errors.New("gc: cycle no longer active; return to the orchestrator")
	}
	switch command {
	case "verify", "consolidate":
		return errors.New("gc: maintenance changes use gc_verdict, gc_acceptance or gc_no_change, not ordinary VFS closure")
	case "op", "shell-prepare":
		if action == "read" {
			return nil
		}
		if state.Cycle.Phase != "collect" {
			return errors.New("gc: mutation refused outside collector phase; call gc_authorize before vfs_write or vfs_delete")
		}
		author := key
		if b.GateAuthorKey != "" {
			author = b.GateAuthorKey
		}
		if command == "op" && !slices.Contains(f.scopeLocked(author), path) {
			return errors.New("gc: path outside authorized scope; mutate only files gc_authorize returned")
		}
	}
	return nil
}
