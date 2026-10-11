package vfs

import (
	"fmt"
	"maps"
)

// PrepareMaintenance reserves the collector scope and independent verifier
// together. A failure cannot leave just one lane's reservation behind. Each
// lane carries the attempt the execution history issued when it attached.
func (f *FS) PrepareMaintenance(author, verifier Identity, scope []string) (authorKey, verifierKey AgentID, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return "", "", err
	}
	if author.AttemptID == "" || verifier.AttemptID == "" {
		return "", "", fmt.Errorf("%w: a maintenance lane has no attempt in the execution history; attach both lanes, then call gc_authorize again", ErrIdentity)
	}
	bindings, staged, owners := maps.Clone(f.bindings), maps.Clone(f.staged), maps.Clone(f.owners)
	authorKey, err = f.bindLocked(author, scope)
	if err == nil {
		verifierKey, err = f.assignVerifierLocked(verifier, authorKey)
	}
	if err != nil {
		f.bindings, f.staged, f.owners = bindings, staged, owners
		return "", "", err
	}
	f.finishLocked(&err)
	return authorKey, verifierKey, err
}
