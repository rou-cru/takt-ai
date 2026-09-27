package gc

import (
	"errors"
	"slices"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// GuardVFS applies persisted harness authority at every transport boundary.
func GuardVFS(state, command string, identity vfs.Identity, action, path string, authorKey vfs.AgentID, fs *vfs.FS) error {
	c, e := LoadCoordinator(state)
	if e != nil {
		return e
	}
	if e := guardVFSBind(c, command, identity); e != nil {
		return e
	}
	id, ok := fs.BindingIdentity(authorKey)
	if !ok || id.CycleID == "" {
		if identity.CycleID != "" || identity.MandateClass != "" {
			return errors.New("gc: ordinary work cannot claim a maintenance cycle")
		}
		return nil
	}
	if e := guardVFSCycle(c, id.CycleID); e != nil {
		return e
	}
	return guardVFSCommand(c, command, action, path)
}

func guardVFSBind(c *Coordinator, command string, identity vfs.Identity) error {
	if command != "bind" {
		return nil
	}
	if identity.CycleID != "" || identity.MandateClass != "" {
		return errors.New("gc: collector bindings are issued only by coordinator authorization")
	}
	// The barrier halts new work, not work already running: a unit whose
	// delegation is in flight (the harness resolved its attempt at admission)
	// keeps binding until it finishes, since the cycle waits for exactly those
	// units (PR-MNT-3).
	if c.Held() && identity.AttemptID == "" {
		return errors.New("gc: barrier holds new ordinary bindings")
	}
	return nil
}

func guardVFSCycle(c *Coordinator, cycleID string) error {
	if c.Cycle == nil || c.Cycle.Plan.CycleID != cycleID {
		return errors.New("gc: cycle is not active")
	}
	return nil
}

func guardVFSCommand(c *Coordinator, command, action, path string) error {
	switch command {
	case "verify", "consolidate":
		return errors.New("gc: maintenance verdict and consolidation are coordinator-only")
	case "op":
		if c.Cycle.Phase != "collect" {
			return errors.New("gc: mutation outside collector phase")
		}
		if action != "read" && action != "rollback" && !slices.Contains(c.Cycle.Scope, path) {
			return errors.New("gc: path outside authorized scope")
		}
	}
	return nil
}
