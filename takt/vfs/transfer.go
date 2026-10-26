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

// handOverLocked claims scope for identity under a new key, moving the paths
// it names of key's work there with their staged content and base. The paths
// it leaves out stay with key; a verdict over key no longer matches its delta.
func (f *FS) handOverLocked(identity Identity, key AgentID, scope, added []string) (AgentID, error) {
	if err := f.checkClaimScope(identity, added); err != nil {
		return "", err
	}
	if err := f.admitReadsLocked(identity); err != nil {
		return "", err
	}
	if err := f.guardReadersLocked(identity, added); err != nil {
		return "", err
	}
	// Like any assignment, the claim awaits adoption by its target.
	identity.AttemptID, identity.HostSession, identity.Prelaunch = "", "", true
	identity, err := f.bindableIdentity(identity, scope)
	if err != nil {
		return "", err
	}
	next, err := f.claimLocked(identity, added)
	if err != nil {
		return "", err
	}
	from, to := f.ensureDelta(key), f.ensureDelta(next)
	for _, path := range scope {
		if f.owners[path] != key {
			continue
		}
		transferStagedLocked(from, to, path)
		f.owners[path] = next
	}
	return next, nil
}

// transferStagedLocked moves one path's staged work, its content or
// tombstone with the captured base, between two claims' deltas, keeping the
// exact bytes. A sender with no delta, or one that never staged the path,
// leaves the path untouched.
func transferStagedLocked(from, to *agentDelta, path string) {
	if from == nil {
		return
	}
	if content, staged := from.files[path]; staged {
		to.files[path] = content
		delete(from.files, path)
	}
	if base, captured := from.bases[path]; captured {
		to.bases[path] = base
		delete(from.bases, path)
	}
}
