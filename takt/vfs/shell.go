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
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"unicode"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// Shell decisions. PR-HAR-8 resolves every governed action to exactly one of
// them, and PR-HAR-11 routes shell execution through the same three.
const (
	// ShellAllow runs the command inside the sandbox with no approval.
	ShellAllow = "allow"
	// ShellAsk holds the exact command for a single-execution approval.
	ShellAsk = "ask"
	// ShellDeny refuses the command instead of running it unstaged.
	ShellDeny = "deny"
)

// OpShell records one shell command as a single VFS transaction: its input
// revision and delta hash, the resulting delta hash, the identity, and the
// command's own outcome. PR-VFS-JRN §2.5 scopes journaling to the command, not
// to its internal reads or syscalls.
const OpShell OperationType = "shell"

const (
	// shellDirMode keeps the private projection unreadable to other users.
	shellDirMode os.FileMode = 0o700
	// shellFileMode is the projection copy's mode; the staged delta carries the
	// content, never this mode.
	shellFileMode os.FileMode = 0o600
	// shellRootHashLen is how much of the derived digest names the sandbox root.
	shellRootHashLen = 16
	// everythingWritable is the write grant an approved out-of-workspace command
	// receives. The workspace and the private state stay denied inside it, so an
	// approval can never become a way around PR-HAR-15.
	everythingWritable = "/"
)

// Shell errors mark outcomes the caller must treat as no-import, never retry blind.
var (
	// ErrShellUncapturable is returned when a caller that stages nothing (no
	// binding, or a verifier's empty scope) runs a workspace-mutating command:
	// PR-HAR-15 denies it rather than running it unstaged.
	ErrShellUncapturable = errors.New("this agent does not modify the workspace through the shell")

	// ErrShellDescendants is returned when a command's descendants were not
	// confirmed terminated. Its delta is retained, unimported: a surviving
	// process could still be writing into the projection.
	ErrShellDescendants = errors.New("vfs: shell descendants not confirmed terminated; delta retained unimported")
)

// ShellPlan is what the coordinator hands the sandbox wrapper for one command.
// Every path in it is resolved here from the binding's own scope; none of it is
// ever accepted from the model.
type ShellPlan struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	// Cwd is the directory the command runs in: the private projection for a
	// captured mutation, the workspace itself for inspection.
	Cwd string `json:"cwd,omitempty"`
	// Scratch is the sandbox-root directory the command may write freely; it is
	// wiped with the projection when the result is imported.
	Scratch string `json:"scratch,omitempty"`
	// Confirm is where the supervisor records the command's status once it has
	// confirmed no descendant survives. Its absence blocks the import.
	Confirm string `json:"confirm,omitempty"`
	// Writable, Protected and Private are the sandbox's write grant, its
	// write-denied paths, and the paths denied for both reading and writing.
	Writable  []string `json:"writable,omitempty"`
	Protected []string `json:"protected,omitempty"`
	Private   []string `json:"private,omitempty"`
	// Capture reports that the command's result must be imported as a VFS
	// transaction once it ends.
	Capture bool `json:"capture"`
}

// shellRoot is the private sandbox root for one command: a sibling of the state
// directory, derived from it and the call id so the import recomputes the path
// instead of trusting one from its caller. It sits beside the state rather than
// inside it, because a path denied to the sandbox must never be an ancestor of
// the projection.
func shellRoot(stateDir, callID string) string {
	digest := hashOf([]byte(stateDir+"\x00"+callID), true)
	return filepath.Join(filepath.Dir(stateDir), "shell-"+digest[:shellRootHashLen])
}

// PrepareShell classifies one command and lays out its private projection.
// key may be empty: an unbound caller may still inspect, never mutate.
// expected must match a bound caller's revision and is ignored when key is empty.
// For callers without file scope, quoted spans are ignored when detecting
// workspace mutations; the sandbox must still enforce the returned restrictions.
// Preparation creates scratch and, for allowed mutations, projection files,
// but does not execute the command. A policy denial returns a ShellDeny plan
// with a nil error. Missing callID or stateDir, a blank command, or an unknown
// nonempty key returns ErrIdentity; a revision mismatch returns ErrStaleRevision.
// Store readiness and scratch or projection filesystem errors propagate.
func (f *FS) PrepareShell(key AgentID, callID, command, stateDir string, expected uint64) (ShellPlan, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.readyLocked(); err != nil {
		return ShellPlan{}, err
	}
	if callID == "" || strings.TrimSpace(command) == "" || stateDir == "" {
		return ShellPlan{}, ErrIdentity
	}
	scope := f.scopeLocked(key)
	if key != "" {
		if _, ok := f.bindings[key]; !ok {
			return ShellPlan{}, ErrIdentity
		}
		if f.revisionOf(key) != expected {
			return ShellPlan{}, ErrStaleRevision
		}
	}
	// A caller with no scope can only inspect, so quoted text (a pattern, an
	// inline script) is data, not a write; a write hidden in quotes, such as
	// `sh -c '… > f'`, runs as an inspection and fails on the sandbox instead.
	mutates := mutatesWorkspace(command)
	if len(scope) == 0 {
		mutates = mutatesWorkspace(unquoted(command))
	}
	decision, reason := f.classify(key, command, mutates, len(scope) > 0)
	root := shellRoot(stateDir, callID)
	if key == "" {
		// An unbound caller can stage nothing, so its sandbox needs no per-call
		// identity: one reused scratch leaves nothing behind to clean up.
		root = shellRoot(stateDir, "")
	}
	plan := ShellPlan{
		Decision: decision, Reason: reason,
		Scratch: filepath.Join(root, "scratch"), Confirm: filepath.Join(root, "confirm"),
		Private: []string{stateDir}, Capture: decision != ShellDeny && key != "",
	}
	if decision == ShellDeny {
		return ShellPlan{Decision: decision, Reason: reason}, nil
	}
	if err := os.MkdirAll(plan.Scratch, shellDirMode); err != nil {
		return ShellPlan{}, err
	}
	if mutates && decision == ShellAllow {
		return f.projectLocked(key, root, scope, plan)
	}
	// Inspection reads the workspace in place and can write only to its scratch,
	// so no projection is needed. An approved command adds everything outside
	// the workspace, which is exactly what the user accepted: the workspace
	// itself stays write-denied, because no approval opens a way around the VFS.
	// It also reads in place, so the secrets the native read rules deny are
	// denied here too; a captured mutation never sees the workspace at all.
	plan.Cwd, plan.Writable, plan.Protected = f.rootDir, []string{plan.Scratch}, []string{f.rootDir}
	plan.Private = append(plan.Private, sensitivePaths(f.rootDir, userHome())...)
	// The workspace event store records every session's activity; it is the
	// orchestrator's to read, never a specialist's inspection.
	store := filepath.Join(f.rootDir, obs.StateDirName, obs.StoreFileName)
	if _, err := os.Stat(store); err == nil {
		plan.Private = append(plan.Private, store)
	}
	if decision == ShellAsk {
		plan.Writable = []string{everythingWritable}
	}
	return plan, nil
}

// projectLocked materializes the binding's scope, as the merged view shows it,
// into the private projection the command will mutate. The physical workspace is
// denied for both reading and writing, so the projection is the only view the
// command has of its scope.
func (f *FS) projectLocked(key AgentID, root string, scope []string, plan ShellPlan) (ShellPlan, error) {
	projection := filepath.Join(root, "projection")
	for _, rel := range scope {
		target := filepath.Join(projection, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), shellDirMode); err != nil {
			return ShellPlan{}, err
		}
		content, _, err := f.readMergedLocked(key, rel)
		if err != nil {
			return ShellPlan{}, err
		}
		// A staged-absent path is still written: the sandbox only grants writes to
		// declared files that already exist.
		if err := os.WriteFile(target, content, shellFileMode); err != nil {
			return ShellPlan{}, err
		}
		plan.Writable = append(plan.Writable, target)
	}
	plan.Cwd, plan.Private = projection, append(plan.Private, f.rootDir)
	return plan, nil
}

// ImportShell admits the command's result as one VFS transaction: the captured
// projection becomes staged content through the same path the file tools use.
// A failing command keeps its delta, retained and unverified (PR-VFS-CSL-5).
func (f *FS) ImportShell(key AgentID, callID, stateDir string, expected uint64) (result OperationResult, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return result, err
	}
	root := shellRoot(stateDir, callID)
	status, err := os.ReadFile(filepath.Join(root, "confirm"))
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrShellDescendants, err)
	}
	start := len(f.journal)
	defer func() {
		if err != nil && len(f.journal) == start {
			f.appendJournalLocked(JournalEntry{Agent: key, Operation: OpShell, Outcome: "denied"})
		}
		f.correlateLocked(start, key, key, callID)
		f.finishLocked(&err)
	}()
	if _, err = f.admitLocked(Operation{Key: key, CallID: callID, ExpectedRevision: expected, Action: OpShell}, key); err != nil {
		return result, err
	}
	before := f.deltaHashLocked(key)
	if err = f.captureLocked(key, filepath.Join(root, "projection")); err != nil {
		return result, err
	}
	// One entry per command: the input revision and delta hash, the resulting
	// delta hash, the identity correlateLocked stamps, and the command's status.
	f.appendJournalLocked(JournalEntry{
		Agent: key, Operation: OpShell, BeforeHash: before, AfterHash: f.deltaHashLocked(key),
		Outcome: "status " + strings.TrimSpace(string(status)) + " from revision " + strconv.FormatUint(expected, 10),
	})
	result.Revision, result.DeltaHash = f.revisionOf(key), f.deltaHashLocked(key)
	return result, os.RemoveAll(root)
}

// captureLocked stages every scope path whose projection copy diverged from the
// merged view. A projection that was never created captures nothing.
func (f *FS) captureLocked(key AgentID, projection string) error {
	if _, err := os.Stat(projection); err != nil {
		return nil
	}
	for _, rel := range f.scopeLocked(key) {
		before, existed, err := f.readMergedLocked(key, rel)
		if err != nil {
			return err
		}
		content, readErr := os.ReadFile(filepath.Join(projection, filepath.FromSlash(rel)))
		switch {
		case readErr != nil:
			if !existed {
				continue
			}
			err = f.stageLocked(key, rel, nil)
		case string(content) == string(before):
			continue
		default:
			err = f.stageLocked(key, rel, content)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// scopeLocked lists the paths key owns, in a stable order.
func (f *FS) scopeLocked(key AgentID) []string {
	if key == "" {
		return nil
	}
	var scope []string
	for rel, owner := range f.owners {
		if owner == key {
			scope = append(scope, rel)
		}
	}
	slices.Sort(scope)
	return scope
}

// ─── classification ───────────────────────────────────────────────────────────

// The word lists below route a command to a bucket; they are not a shell parser.
// The sandbox is what enforces the bucket, so an unnoticed mutation fails on its
// first write instead of reaching the workspace, and an inspection mistaken for
// a mutation only runs in a projection.
var (
	mutatingWords  = []string{"tee", "cp", "mv", "rm", "rmdir", "mkdir", "touch", "truncate", "dd", "ln", "install", "chmod", "chown", "patch", "sponge"}
	inPlaceWords   = []string{"sed", "perl", "gofmt", "goimports"}
	inPlaceFlags   = []string{"-i", "-w", "--write", "--fix", "--in-place"}
	privilegeWords = []string{"sudo", "doas", "su"}
	hostWideWords  = []string{"systemctl", "launchctl", "crontab", "brew", "apt", "apt-get", "dnf", "yum", "pacman", "mkfs", "shutdown", "reboot"}
	toolchainWords = []string{"npm", "pnpm", "yarn", "bun", "pip", "pip3", "gem", "cargo", "go", "docker", "kubectl", "helm", "git", "gh"}
	globalWords    = []string{"install", "uninstall", "publish", "push", "apply", "login", "config"}
	// discardedWrites write no file the VFS would have to capture.
	discardedWrites = []string{"2>&1", "1>&2", "2>/dev/null", "2> /dev/null", ">/dev/null", "> /dev/null", ">>/dev/null", ">> /dev/null"}
)

// classify resolves the command to allowed, approval-gated or denied.
func (f *FS) classify(key AgentID, command string, mutates, scoped bool) (string, string) {
	words := shellWords(command)
	if gitMutation(words) {
		// PR-VFS-GIT-1 denies mutating git outside the orchestrator; PR-VFS-GIT-5
		// gates the rest of its mutations on approval.
		if err := GuardGitMutation(f.bindings[key].Role); err != nil {
			return ShellDeny, err.Error()
		}
		return ShellAsk, "mutating git command: " + command
	}
	if reason, outside := outsideWorkspace(words, mutates, f.rootDir); outside {
		return ShellAsk, reason + ", which the VFS cannot capture: " + command
	}
	if mutates && !scoped {
		return ShellDeny, fmt.Sprintf("%v: %s", ErrShellUncapturable, command)
	}
	if mutates {
		return ShellAllow, "workspace mutation captured as a staged VFS delta: " + command
	}
	return ShellAllow, "inspection: " + command
}

// mutatesWorkspace reports whether the command writes files.
func mutatesWorkspace(command string) bool {
	words := shellWords(command)
	return capturableRedirect(command) || containsAny(words, mutatingWords) ||
		(containsAny(words, inPlaceWords) && containsAny(words, inPlaceFlags))
}

// unquoted drops single- and double-quoted spans, quotes included; a
// backslash escapes the next character outside single quotes.
func unquoted(command string) string {
	var out strings.Builder
	var quote rune
	escaped := false
	for _, r := range command {
		switch {
		case escaped:
			escaped = false
			if quote == 0 {
				out.WriteRune(r)
			}
		case r == '\\' && quote != '\'':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

// capturableRedirect reports a redirection whose target is a file, ignoring
// descriptor duplication and the null device.
func capturableRedirect(command string) bool {
	for _, discarded := range discardedWrites {
		command = strings.ReplaceAll(command, discarded, "")
	}
	return strings.Contains(command, ">")
}

// outsideWorkspace reports effects the VFS cannot capture: privilege
// escalation, host-wide package or service management, a toolchain's global
// operations, and a mutation aimed at an absolute path outside the workspace.
func outsideWorkspace(words []string, mutates bool, rootDir string) (string, bool) {
	switch {
	case containsAny(words, privilegeWords):
		return "privilege escalation", true
	case containsAny(words, hostWideWords):
		return "host-wide package or service management", true
	case containsAny(words, toolchainWords) && containsAny(words, globalWords):
		return "toolchain operation outside the workspace", true
	case mutates && writesOutside(words, rootDir):
		return "mutation of an absolute path outside the workspace", true
	}
	return "", false
}

// writesOutside reports an absolute or home-relative path that is not inside
// the workspace.
func writesOutside(words []string, rootDir string) bool {
	for _, word := range words {
		word = strings.TrimLeft(word, "<>&")
		if strings.HasPrefix(word, "~") {
			return true
		}
		if strings.HasPrefix(word, "/") && !strings.HasPrefix(filepath.Clean(word), rootDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// gitMutation reports a git invocation whose subcommand is not read-only.
func gitMutation(words []string) bool {
	readOnly := ReadOnlyGitSubcommands()
	for i, word := range words {
		if (word == "git" || strings.HasSuffix(word, "/git")) && i+1 < len(words) && !slices.Contains(readOnly, words[i+1]) {
			return true
		}
	}
	return false
}

// shellWords splits a command into words, treating shell operators as
// separators so `a&&b` yields both.
func shellWords(command string) []string {
	return strings.FieldsFunc(command, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("|;&()`'\"", r)
	})
}

func containsAny(words, wanted []string) bool {
	return slices.ContainsFunc(words, func(word string) bool { return slices.Contains(wanted, word) })
}
