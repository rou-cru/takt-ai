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

import "os"

// The flush, recovery and persistence mechanics the black-box fault tests
// drive one step at a time; production reaches them only through the locked
// commands that compose them.

// BaseFile is the physical state of one workspace path.
type BaseFile = baseFile

// RecoveryManifest is the durable record of one in-progress flush.
type RecoveryManifest = recoveryManifest

// RecoveryItem is one path of a recovery manifest.
type RecoveryItem = recoveryItem

// AgentDelta is one binding's staged work.
type AgentDelta = agentDelta

// RecoveryDirectoryMode is the mode of directories a flush materializes.
const RecoveryDirectoryMode = recoveryDirectoryMode

// NewDelta is staged work with files and the bases they were staged over.
func NewDelta(files map[string][]byte, bases map[string]BaseFile) *AgentDelta {
	return &agentDelta{files: files, bases: bases}
}

// HashOf is the content hash the journal records for a path.
func HashOf(content []byte, present bool) string { return hashOf(content, present) }

// RemoveLocked unlinks rel under the parent directory fd.
func RemoveLocked(fd int, rel string, parent *os.File) error { return removeLocked(fd, rel, parent) }

// WriteLocked writes value to temp and renames it over rel under parent fd.
func WriteLocked(fd int, rel, temp string, value BaseFile, parent *os.File) error {
	return writeLocked(fd, rel, temp, value, parent)
}

// EnsurePrivateFile creates path private to the owner, or checks it is.
func EnsurePrivateFile(path string) error { return ensurePrivateFile(path) }

// SetBinding records identity under key.
func (f *FS) SetBinding(key AgentID, identity Identity) { f.bindings[key] = identity }

// SetOwner records key as the owner of path.
func (f *FS) SetOwner(path string, key AgentID) { f.owners[path] = key }

// StageFile stages content for path in agent's delta.
func (f *FS) StageFile(agent AgentID, path string, content []byte) {
	f.ensureDelta(agent).files[path] = content
}

// StagedBase is the base agent's delta captured for path.
func (f *FS) StagedBase(agent AgentID, path string) (BaseFile, bool) {
	d, ok := f.staged[agent]
	if !ok {
		return BaseFile{}, false
	}
	base, ok := d.bases[path]
	return base, ok
}

func (f *FS) StageLocked(agent AgentID, path string, content []byte) error {
	return f.stageLocked(agent, path, content)
}
func (f *FS) ReadLockedAs(agent AgentID, path string, authorizedStagedView bool) ([]byte, error) {
	return f.readLockedAs(agent, path, authorizedStagedView)
}
func (f *FS) ConsolidateLocked(agent AgentID) error { return f.consolidateLocked(agent) }
func (f *FS) ReadMergedLocked(agent AgentID, rel string) ([]byte, bool, error) {
	return f.readMergedLocked(agent, rel)
}
func (f *FS) ClaimLocked(identity Identity, scope []string) (AgentID, error) {
	return f.claimLocked(identity, scope)
}
func (f *FS) PlanFlush(agent AgentID, d *AgentDelta) (*RecoveryManifest, error) {
	return f.planFlush(agent, d)
}
func (f *FS) NoteMissingDirs(m *RecoveryManifest, rel string) error { return f.noteMissingDirs(m, rel) }
func (f *FS) CreateDirs(dirs []string) error                        { return f.createDirs(dirs) }
func (f *FS) ReplaceItems(m *RecoveryManifest) error                { return f.replaceItems(m) }
func (f *FS) ReplaceLocked(rel, temp string, value BaseFile) error {
	return f.replaceLocked(rel, temp, value)
}
func (f *FS) OpenParent(rel string) (*os.File, error)         { return f.openParent(rel) }
func (f *FS) SyncParent(rel string) error                     { return f.syncParent(rel) }
func (f *FS) RemoveIfPresentLocked(rel string) error          { return f.removeIfPresentLocked(rel) }
func (f *FS) RemoveLeftovers(m *RecoveryManifest) error       { return f.removeLeftovers(m) }
func (f *FS) CheckSiblings(parent, part string) (bool, error) { return f.checkSiblings(parent, part) }
func (f *FS) CheckEntry(rel string, last bool) (bool, error)  { return f.checkEntry(rel, last, false) }
func (f *FS) Physical(rel string) (BaseFile, error)           { return f.physical(rel) }
func (f *FS) CaptureBaseLocked(agent AgentID, rel string) error {
	return f.captureBaseLocked(agent, rel)
}
func (f *FS) AppendJournalLocked(entry JournalEntry) { f.appendJournalLocked(entry) }
func (f *FS) PersistLocked() error                   { return f.persistLocked() }
func (f *FS) LoadState() error                       { return f.loadState() }
func (f *FS) ReplayJournal(count int) error          { return f.replayJournal(count) }

// CloseDB closes the store's database under the open handle.
func (f *FS) CloseDB() error { return f.db.Close() }

// QueryInt runs a single-value integer query against the store's database.
func (f *FS) QueryInt(query string) (n int, err error) {
	err = f.db.QueryRow(query).Scan(&n)
	return n, err
}

// StateSchema creates the store's tables in an empty database.
const StateSchema = stateSchema

// StateFileMode and StateDirectoryMode are the modes of the private store.
const (
	StateFileMode      = stateFileMode
	StateDirectoryMode = stateDirectoryMode
)
