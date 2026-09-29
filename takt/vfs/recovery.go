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
	"crypto/rand"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"maps"
	"os"
	"path"
	"slices"
)

const (
	// recoveryDirectoryMode gives newly materialized parent directories owner
	// write access while allowing all users to traverse them.
	recoveryDirectoryMode os.FileMode = 0755
	// recoveryRandomIDBytes is the entropy buffer size for staged sibling names.
	recoveryRandomIDBytes = 16
)

type recoveryItem struct {
	Path   string
	Temp   string
	Before baseFile
	After  baseFile
}
type recoveryManifest struct {
	Agent AgentID
	Items []recoveryItem
	// Intent is durably advanced BEFORE each replacement. Recovery considers
	// only this prefix; untouched paths must never be restored over user edits.
	Intent int
	Dirs   []string
}

func (f *FS) checkpointLocked(point string) error {
	if err := f.persistLocked(); err != nil {
		f.fault = err
		return errors.Join(ErrStoreFailed, err)
	}
	if f.failpoint != nil {
		return f.failpoint(point)
	}
	return nil
}

func (f *FS) materializeLocked(agent AgentID, d *agentDelta) error {
	// Frozen declared bases include files the unit did not ultimately edit.
	for rel, base := range d.bases {
		if err := f.checkBase(rel, base); err != nil {
			return err
		}
	}
	m, err := f.planFlush(agent, d)
	if err != nil {
		return err
	}
	return f.applyManifest(agent, m)
}

func (f *FS) applyManifest(agent AgentID, m *recoveryManifest) error {
	f.recovery = m
	if err := partial(f.checkpointLocked("prepared")); err != nil {
		return err
	}
	if err := f.createDirs(m.Dirs); err != nil {
		return err
	}
	if err := f.replaceItems(m); err != nil {
		return err
	}
	for _, item := range m.Items {
		if err := f.checkBase(item.Path, item.After); err != nil {
			return err
		}
	}
	if err := partial(f.checkpointLocked("verified")); err != nil {
		return err
	}
	for _, item := range m.Items {
		f.appendJournalLocked(JournalEntry{Agent: agent, Path: item.Path, Operation: OpFlush, BeforeHash: hashOf(item.Before.Content, item.Before.Present), AfterHash: hashOf(item.After.Content, item.After.Present)})
	}
	// A maintenance cycle stays discardable until it closes, so what it
	// overwrote outlives the manifest that is about to go (PR-MNT-16).
	f.retainCycleLocked(agent, m)
	// Removal of the manifest and staged delta is committed together by the
	// caller's finishLocked. A crash before that commit rolls back on Recover.
	f.recovery = nil
	return nil
}

// partial marks a failure after physical mutation may have started.
func partial(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrFlushPartial, err)
}

// checkBase fails unless rel still holds exactly want on disk.
func (f *FS) checkBase(rel string, want baseFile) error {
	current, err := f.physical(rel)
	if err != nil || !sameFile(current, want) {
		return errors.Join(ErrBaseChanged, err)
	}
	return nil
}

// tempName is the private sibling a replacement is staged in.
func tempName(rel string) string {
	return path.Join(path.Dir(rel), fmt.Sprintf("%s%x", tempPrefix, randomID()))
}

// planFlush records what each staged path holds before and after the flush.
func (f *FS) planFlush(agent AgentID, d *agentDelta) (*recoveryManifest, error) {
	m := &recoveryManifest{Agent: agent}
	for _, rel := range slices.Sorted(maps.Keys(d.files)) {
		before, err := f.physical(rel)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrFlushPartial, err)
		}
		if original, ok := d.bases[rel]; ok && !sameFile(original, before) {
			return nil, ErrBaseChanged
		}
		after := baseFile{Content: d.files[rel], Present: d.files[rel] != nil, Mode: before.Mode}
		if !before.Present {
			after.Mode = 0644
		}
		if !after.Present {
			after.Mode = 0
		}
		m.Items = append(m.Items, recoveryItem{rel, tempName(rel), before, after})
		if err := f.noteMissingDirs(m, rel); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(m.Dirs, func(a, b string) int { return cmp.Compare(len(a), len(b)) })
	return m, nil
}

// noteMissingDirs adds to m the ancestors of rel that do not exist yet.
func (f *FS) noteMissingDirs(m *recoveryManifest, rel string) error {
	for dir := path.Dir(rel); dir != "."; dir = path.Dir(dir) {
		_, err := f.root.Stat(dir)
		if errors.Is(err, os.ErrNotExist) {
			if !slices.Contains(m.Dirs, dir) {
				m.Dirs = append(m.Dirs, dir)
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (f *FS) createDirs(dirs []string) error {
	for _, dir := range dirs {
		if err := f.root.Mkdir(dir, recoveryDirectoryMode); err != nil && !errors.Is(err, os.ErrExist) {
			return partial(err)
		}
		if err := partial(f.syncParent(dir)); err != nil {
			return err
		}
	}
	return nil
}

// replaceItems applies each planned replacement, advancing the durable intent
// before each one so Recover restores only what may have been touched.
func (f *FS) replaceItems(m *recoveryManifest) error {
	for i, item := range m.Items {
		if err := f.checkBase(item.Path, item.Before); err != nil {
			return err
		}
		m.Intent = i + 1
		if err := partial(f.checkpointLocked("before/" + item.Path)); err != nil {
			return err
		}
		if err := partial(f.replaceLocked(item.Path, item.Temp, item.After)); err != nil {
			return err
		}
		if err := partial(f.checkpointLocked("after/" + item.Path)); err != nil {
			return err
		}
	}
	return nil
}

func randomID() []byte {
	b := make([]byte, recoveryRandomIDBytes)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return b
}

func (f *FS) replaceLocked(rel, temp string, value baseFile) (err error) {
	if err := f.validatePath(rel); err != nil {
		return err
	}
	parent, err := f.openParent(rel)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	fd := int(parent.Fd())
	if !value.Present {
		return removeLocked(fd, rel, parent)
	}
	return writeLocked(fd, rel, temp, value, parent)
}

func removeLocked(fd int, rel string, parent *os.File) error {
	if err := unix.Unlinkat(fd, path.Base(rel), 0); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return parent.Sync()
}

func writeLocked(fd int, rel, temp string, value baseFile, parent *os.File) (err error) {
	tempFD, err := unix.Openat(fd, path.Base(temp), unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(value.Mode))
	if err != nil {
		return err
	}
	out := os.NewFile(uintptr(tempFD), temp)
	if err = out.Chmod(value.Mode); err == nil {
		_, err = out.Write(value.Content)
	}
	if err == nil {
		err = out.Sync()
	}
	err = errors.Join(err, out.Close())
	if err != nil {
		return err
	}
	if err = unix.Renameat(fd, path.Base(temp), fd, path.Base(rel)); err != nil {
		return err
	}
	return parent.Sync()
}

// Recover restores the pre-flush snapshot after checking for incompatible writes.
func (f *FS) Recover() (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fault != nil {
		return errors.Join(ErrStoreFailed, f.fault)
	}
	m := f.recovery
	if m == nil {
		return nil
	}
	touched := m.Items[:m.Intent]
	if err := f.checkTouched(touched); err != nil {
		return err
	}
	for _, item := range touched {
		if err := f.restoreItem(m, item); err != nil {
			return err
		}
	}
	if err := f.removeLeftovers(m); err != nil {
		return err
	}
	f.recovery = nil
	f.appendJournalLocked(JournalEntry{Agent: m.Agent, Operation: OperationType("recovery"), Outcome: "restored"})
	f.finishLocked(&err)
	return err
}

// checkTouched fails when a touched path holds neither its original nor its flushed content.
func (f *FS) checkTouched(items []recoveryItem) error {
	for _, item := range items {
		current, err := f.physical(item.Path)
		if err != nil {
			return err
		}
		if !sameFile(current, item.Before) && !sameFile(current, item.After) {
			return ErrBaseChanged
		}
	}
	return nil
}

// restoreItem puts item's original content back.
func (f *FS) restoreItem(m *recoveryManifest, item recoveryItem) error {
	current, err := f.physical(item.Path)
	if err != nil {
		return err
	}
	if !sameFile(current, item.Before) {
		if err := f.rewriteOriginal(m, item); err != nil {
			return err
		}
	}
	restored, err := f.physical(item.Path)
	if err != nil || !sameFile(restored, item.Before) {
		return errors.Join(ErrRecoveryRequired, err)
	}
	return f.checkpointLocked("restored/" + item.Path)
}

// rewriteOriginal writes item.Before through a fresh temporary.
func (f *FS) rewriteOriginal(m *recoveryManifest, item recoveryItem) error {
	temp := tempName(item.Path)
	if err := f.removeIfPresentLocked(item.Temp); err != nil {
		return err
	}
	for i := range m.Items {
		if m.Items[i].Path == item.Path {
			m.Items[i].Temp = temp
		}
	}
	if err := f.checkpointLocked("restore-before/" + item.Path); err != nil {
		return err
	}
	return f.replaceLocked(item.Path, temp, item.Before)
}

// removeLeftovers deletes the temporaries and directories the flush created.
func (f *FS) removeLeftovers(m *recoveryManifest) error {
	for _, item := range m.Items {
		if err := f.removeIfPresentLocked(item.Temp); err != nil {
			return err
		}
	}
	for _, dir := range slices.Backward(m.Dirs) {
		if err := f.removeIfPresentLocked(dir); err != nil {
			return err
		}
	}
	return nil
}

// removeIfPresentLocked removes rel and makes the removal durable.
func (f *FS) removeIfPresentLocked(rel string) error {
	if err := f.root.Remove(rel); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	return f.syncParent(rel)
}
