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
	"encoding/json"
	"maps"
	"slices"
)

// stagedLocked reports whether key holds at least one staged change.
func (f *FS) stagedLocked(key AgentID) bool {
	d := f.staged[key]
	return d != nil && len(d.files) > 0
}

func (f *FS) deltaHashLocked(key AgentID) string {
	d := f.staged[key]
	if d == nil {
		return hashOf([]byte("{}"), true)
	}
	// JSON map encoding orders keys and distinguishes nil deletion from empty.
	b, _ := json.Marshal(d.files)
	return hashOf(b, true)
}

// revisionOf is the staged revision of key, zero when it has no delta.
func (f *FS) revisionOf(key AgentID) uint64 {
	if d := f.staged[key]; d != nil {
		return d.revision
	}
	return 0
}

// rollbackLocked discards key's staged delta, journaling each dropped path.
func (f *FS) rollbackLocked(key AgentID) {
	if d := f.staged[key]; d != nil {
		for _, p := range slices.Sorted(maps.Keys(d.files)) {
			f.appendJournalLocked(JournalEntry{Agent: key, Path: p, Operation: OpRollback, BeforeHash: hashOf(d.files[p], d.files[p] != nil)})
		}
		d.files = map[string][]byte{}
		d.bases = map[string]baseFile{}
		d.verdicts = nil
		d.revision++
	}
	f.releaseOwnershipLocked(key)
}
