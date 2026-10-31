package setup

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"
)

// SetAsideEntry is managed content moved into BackupDir before Takt replaces
// it, so a replacement that does not go ahead can put it back.
type SetAsideEntry struct {
	root string
	// path is the root-relative slash path the content was moved from.
	path string
	// copy is the root-relative slash path of the content under BackupDir.
	copy string
}

// SetAside moves the file or directory at root/rel into BackupDir with a
// single rename. An existing backup at that path is never replaced: the copy
// gains a UTC timestamp suffix instead.
func SetAside(root, rel string) (SetAsideEntry, error) {
	source, err := SafeJoin(root, filepath.FromSlash(rel))
	if err != nil {
		return SetAsideEntry{}, fmt.Errorf("set aside %q: %w", rel, err)
	}
	copyRel := path.Join(BackupDir, rel)
	dest, err := SafeJoin(root, filepath.FromSlash(copyRel))
	if err != nil {
		return SetAsideEntry{}, fmt.Errorf("set aside %q: %w", rel, err)
	}
	if _, statErr := os.Lstat(dest); statErr == nil {
		copyRel += "." + time.Now().UTC().Format("20060102T150405Z")
		if dest, err = SafeJoin(root, filepath.FromSlash(copyRel)); err != nil {
			return SetAsideEntry{}, fmt.Errorf("set aside %q: %w", rel, err)
		}
		if _, statErr := os.Lstat(dest); statErr == nil {
			return SetAsideEntry{}, fmt.Errorf("set aside %q: %s already holds other content", rel, copyRel)
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), backupDirectoryMode); err != nil {
		return SetAsideEntry{}, fmt.Errorf("set aside %q: %w", rel, err)
	}
	if err := renameFile(source, dest); err != nil {
		return SetAsideEntry{}, fmt.Errorf("set aside %q: %w", rel, err)
	}
	entry := SetAsideEntry{root: root, path: rel, copy: copyRel}
	if err := errors.Join(syncDir(filepath.Dir(source)), syncDir(filepath.Dir(dest))); err != nil {
		return entry, fmt.Errorf("set aside %q: %w", rel, err)
	}
	return entry, nil
}

// Restore removes whatever now occupies the set-aside path, such as a partial
// replacement, and renames the copy back into place.
func (entry SetAsideEntry) Restore() error {
	source := filepath.Join(entry.root, filepath.FromSlash(entry.path))
	if err := os.RemoveAll(source); err != nil {
		return fmt.Errorf("restore %q: %w", entry.path, err)
	}
	if err := renameFile(filepath.Join(entry.root, filepath.FromSlash(entry.copy)), source); err != nil {
		return fmt.Errorf("restore %q from %s: %w", entry.path, entry.copy, err)
	}
	return syncDir(filepath.Dir(source))
}

// Backup reports the set-aside path with the copy that holds its content.
func (entry SetAsideEntry) Backup() Backup {
	return Backup{Path: entry.path, Copy: entry.copy}
}
