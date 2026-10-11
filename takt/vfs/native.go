package vfs

import (
	"fmt"
	"path/filepath"
	"strings"
)

// GuardNativeEdit decides against current ownership, including prior roots.
// The plugin transports this decision; it never reconstructs it from claims.
func (f *FS) GuardNativeEdit(session string, resources []string) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if err := f.readyLocked(); err != nil {
		return err
	}
	for _, resource := range resources {
		path := filepath.Clean(resource)
		if filepath.IsAbs(path) {
			rel, err := filepath.Rel(f.rootDir, path)
			if err != nil || !filepath.IsLocal(rel) {
				continue
			}
			path = rel
		}
		path = filepath.ToSlash(path)
		for held, key := range f.owners {
			if !strings.EqualFold(path, held) {
				continue
			}
			b := f.bindings[key]
			return fmt.Errorf("edit refused: %s is held by claim_key %s (root session %s, work unit %s, agent %s); %s", path, key, b.SessionID, b.WorkUnitID, b.AgentID, f.releaseExitLocked(session, key, b))
		}
	}
	return nil
}

// releaseExitLocked names the call that frees key's paths for session's
// orchestrator, chosen by what the claim holds and whose root it is.
func (f *FS) releaseExitLocked(session string, key AgentID, b Identity) string {
	switch {
	case b.GateAuthorKey != "":
		return fmt.Sprintf("it is a verifier gate over author_key %s; it is freed with that author's work", b.GateAuthorKey)
	case f.stagedLocked(key) && b.SessionID != session:
		return errOtherRoot(key, b, f.stagedPathsLocked(key)).Error()
	case f.stagedLocked(key):
		return fmt.Sprintf("it holds staged work: vfs_consolidate or vfs_discard author_key %s once its unit returns", key)
	default:
		return fmt.Sprintf("it holds no staged work: claim_release claim_key %s (an active claim of this session needs the user's agreement and confirmed true)", key)
	}
}
