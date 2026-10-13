package vfs

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/catalog"
)

// nativeInspection is a reservation over a file or subtree, not a shell plan.
// Empty Path denotes the workspace; External denotes no workspace access.
type nativeInspection struct {
	Path     string
	Tree     bool
	External bool
}

// StartNativeInspection atomically authorizes and reserves the native tool's
// complete execution. The caller must end it only after native execution exits.
func (f *FS) StartNativeInspection(c Caller, callID, tool, resource string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	if !isVFSAuthor(c.Agent) {
		return nil
	}
	c, err = f.currentCallerLocked(c, false)
	if err != nil {
		return err
	}
	if callID == "" {
		return ErrIdentity
	}
	index := shellCallKey(c.SessionID, callID)
	if _, exists := f.shellCalls[index]; exists {
		return ErrDuplicateCall
	}
	key, err := f.heldAuthorLocked(c, "")
	if err == errNoHeldBinding {
		err = nil
	}
	if err != nil {
		return err
	}
	inspection, err := f.nativeScope(tool, resource)
	if err != nil {
		return err
	}
	for path, owner := range f.owners {
		if owner != key && inspection.contains(path) {
			return errForeignAuthor(owner, f.bindings[key].Invariants)
		}
	}
	f.shellCalls[index] = shellCall{Key: key, Caller: c, Started: true, Inspection: &inspection}
	f.finishLocked(&err)
	return err
}

// EndNativeInspection releases a completed native call, idempotently. It uses
// the recorded identity even if the execution has since ended.
func (f *FS) EndNativeInspection(c Caller, callID string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	index := shellCallKey(c.SessionID, callID)
	call, ok := f.shellCalls[index]
	if !ok {
		return nil
	}
	if call.Inspection == nil || call.Caller.HostSession != c.HostSession || call.Caller.Agent != c.Agent {
		return ErrIdentity
	}
	delete(f.shellCalls, index)
	f.finishLocked(&err)
	return err
}

func (n nativeInspection) contains(path string) bool {
	return !n.External && (n.Path == "" || strings.EqualFold(path, n.Path) || (n.Tree && strings.HasPrefix(strings.ToLower(path), strings.ToLower(n.Path)+"/")))
}

func (f *FS) nativeScope(tool, resource string) (nativeInspection, error) {
	n := nativeInspection{Tree: tool != "read"}
	switch {
	case strings.Contains(strings.ToLower(tool), "codegraph"):
		return n, nil
	case tool == "grep" || tool == "glob":
		if resource == "" {
			return n, nil
		}
	case tool == "read":
		// Preserve native validation errors, while conservatively reserving the
		// workspace for malformed or absent path input.
		if resource == "" {
			n.Tree = true
			return n, nil
		}
	default:
		return n, fmt.Errorf("%w: unsupported native inspection tool", ErrIdentity)
	}
	abs := resource
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(f.rootDir, abs)
	}
	abs = filepath.Clean(abs)
	// Resolve the nearest existing ancestor too: a missing file below an
	// outside symlink into the workspace still needs an in-workspace reservation.
	resolved := abs
	var tail []string
	for {
		target, e := filepath.EvalSymlinks(resolved)
		if e == nil {
			resolved = target
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			break
		}
		if !os.IsNotExist(e) {
			return n, e
		}
		parent := filepath.Dir(resolved)
		if parent == resolved {
			break
		}
		tail = append(tail, filepath.Base(resolved))
		resolved = parent
	}
	rel, err := filepath.Rel(f.rootDir, resolved)
	if err != nil {
		return n, err
	}
	if rel != "." && !filepath.IsLocal(rel) {
		n.External = true
		return n, nil
	}
	info, e := os.Stat(resolved)
	isDir := e == nil && info.IsDir()
	if isDir {
		n.Tree = true
	}
	if rel != "." {
		n.Path = filepath.ToSlash(rel)
		if err := f.validateInspectionPath(n.Path); err != nil {
			return n, err
		}
		if sensitiveInspection(n.Path, isDir) {
			return n, fmt.Errorf("%w: sensitive path; use declared non-secret inputs", ErrScopeDenied)
		}
	}
	return n, nil
}

func (f *FS) guardCodeQueryLocked(c Caller) error {
	if !isVFSAuthor(c.Agent) {
		return nil
	}
	c, err := f.currentCallerLocked(c, false)
	if err != nil {
		return err
	}
	key, err := f.heldAuthorLocked(c, "")
	if err != nil && err != errNoHeldBinding {
		return err
	}
	for _, owner := range f.owners {
		if owner != key {
			return fmt.Errorf("%w: local code query has no verifiable scope while other authors hold files; read your inputs by ID or permitted files with read/vfs_read instead", ErrScopeDenied)
		}
	}
	return nil
}

func isVFSAuthor(agent AgentID) bool {
	packages, err := catalog.LoadPackages()
	return err == nil && agent != OrchestratorInstance && slices.Contains(packages.VFSAuthors(), string(agent))
}

func (f *FS) foreignReadPathsLocked(key AgentID) []string {
	var paths []string
	for path, owner := range f.owners {
		if owner != key {
			paths = append(paths, filepath.Join(f.rootDir, filepath.FromSlash(path)))
		}
	}
	slices.Sort(paths)
	return paths
}

// guardInspectionClaimLocked prevents a new claim from invalidating the read
// isolation of a running inspection. Retry after shell completion releases it.
func (f *FS) guardInspectionClaimLocked(identity Identity, key AgentID, scope []string) error {
	for _, call := range f.shellCalls {
		if !call.Started || (call.Inspection == nil && call.Plan.Cwd != f.rootDir) || !isVFSAuthor(call.Caller.Agent) {
			continue
		}
		if key == call.Key && identity.SessionID == call.Caller.SessionID && identity.WorkUnitID == call.Caller.WorkUnitID && identity.AgentID == call.Caller.Agent {
			continue
		}
		for _, path := range scope {
			abs := filepath.Join(f.rootDir, filepath.FromSlash(path))
			if (call.Inspection != nil && call.Inspection.contains(path)) || (call.Inspection == nil && !slices.Contains(call.Plan.Private, abs)) {
				return fmt.Errorf("%w: claim would expose %s to an active governed inspection; retry claim_assign after that inspection completes", ErrScopeDenied, path)
			}
		}
	}
	return nil
}

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

// NativeInspectionCall identifies a durable native call for host reconciliation.
type NativeInspectionCall struct {
	CallID     string  `json:"call_id"`
	Session    string  `json:"session_id"`
	Host       string  `json:"caller_session"`
	Agent      AgentID `json:"caller_agent"`
	ToolCallID string  `json:"tool_call_id,omitempty"`
}

// NativeInspections returns pending calls, never inferring completion from age
// or delegation cancellation. The host must observe each exact call terminal.
func (f *FS) NativeInspections() []NativeInspectionCall {
	f.mu.RLock()
	defer f.mu.RUnlock()
	calls := []NativeInspectionCall{}
	for index, call := range f.shellCalls {
		if call.Inspection == nil {
			continue
		}
		_, id, _ := strings.Cut(index, "\x00")
		calls = append(calls, NativeInspectionCall{CallID: id, Session: call.Caller.SessionID, Host: call.Caller.HostSession, Agent: call.Caller.Agent, ToolCallID: call.HostCallID})
	}
	return calls
}

func (f *FS) validateInspectionPath(rel string) error {
	if !filepath.IsLocal(filepath.FromSlash(rel)) || rel == "" || !lexicallyCanonical(rel) {
		return ErrInvalidPath
	}
	return f.validatePathParts(strings.Split(rel, "/"), true)
}
