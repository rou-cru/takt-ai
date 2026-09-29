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
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
	"golang.org/x/text/unicode/norm"
)

// Sentinel errors for path, scope, verdict and durability rejections.
var (
	// ErrInvalidPath is returned for non-canonical, aliased or unsupported paths.
	ErrInvalidPath = errors.New("vfs: non-canonical, aliased or unsupported path")
	// ErrScopeDenied is returned when a mutation falls outside declared ownership.
	ErrScopeDenied = errors.New("vfs: mutation outside declared ownership")
	// ErrInvalidVerdict is returned for invalid or stale verification verdicts.
	ErrInvalidVerdict = errors.New("vfs: invalid or stale verification verdict")
	// ErrBaseChanged is returned when the physical base diverged from the recorded one.
	ErrBaseChanged = errors.New("vfs: physical base changed; manual reconciliation required")
	// ErrRecoveryRequired is returned after an incomplete consolidation.
	ErrRecoveryRequired = errors.New("vfs: incomplete consolidation; recovery required")
	// ErrStoreFailed is returned when durable persistence failed and the store must be reopened.
	ErrStoreFailed = errors.New("vfs: durable persistence failed; reopen required")
)

// tempPrefix names Takt's private staging files; validatePath rejects it as a path.
const tempPrefix = ".takt-vfs-"

// reservedName reports whether a path component is Git metadata or one of
// Takt's private staging names.
func reservedName(part string) bool {
	return strings.EqualFold(part, ".git") || strings.HasPrefix(strings.ToLower(part), tempPrefix)
}

// validatePath rejects aliases rather than authorizing their target.
func (f *FS) validatePath(rel string) error {
	if !fs.ValidPath(rel) || rel == "." || strings.ContainsAny(rel, "\\\x00\r\n") || !norm.NFC.IsNormalString(rel) {
		return ErrInvalidPath
	}
	parts := strings.Split(rel, "/")
	// Lexical, so it holds below directories that do not exist yet.
	if slices.ContainsFunc(parts, reservedName) {
		return fmt.Errorf("%w: %q is a protected workspace-relative path", ErrInvalidPath, rel)
	}
	return f.validatePathParts(parts)
}

func (f *FS) validatePathParts(parts []string) error {
	for i, part := range parts {
		parent := strings.Join(parts[:i], "/")
		if parent == "" {
			parent = "."
		}
		if exists, err := f.checkSiblings(parent, part); !exists || err != nil {
			return err
		}
		if exists, err := f.checkEntry(strings.Join(parts[:i+1], "/"), i == len(parts)-1); !exists || err != nil {
			return err
		}
	}
	return nil
}

// checkSiblings rejects part when parent holds a case or normalization alias.
func (f *FS) checkSiblings(parent, part string) (exists bool, err error) {
	dir, err := f.root.Open(parent)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	entries, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	err = errors.Join(readErr, closeErr)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	for _, entry := range entries {
		if norm.NFC.String(entry.Name()) != part && strings.EqualFold(norm.NFC.String(entry.Name()), part) {
			return false, ErrInvalidPath
		}
	}
	return true, nil
}

// checkEntry accepts rel as a plain directory or single-link regular file.
func (f *FS) checkEntry(rel string, last bool) (exists bool, err error) {
	info, err := f.root.Lstat(rel)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, ErrInvalidPath
	}
	if !last {
		if !info.IsDir() {
			return false, ErrInvalidPath
		}
		return true, nil
	}
	if !info.Mode().IsRegular() {
		return false, ErrInvalidPath
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && stat.Nlink != 1 {
		return false, ErrInvalidPath
	}
	return true, nil
}

type baseFile struct {
	Content []byte      `json:"content"`
	Present bool        `json:"present"`
	Mode    os.FileMode `json:"mode"`
}

func (f *FS) physical(rel string) (result baseFile, err error) {
	if err := f.validatePath(rel); err != nil {
		return baseFile{}, err
	}
	file, err := f.root.Open(rel)
	if errors.Is(err, os.ErrNotExist) {
		return baseFile{}, nil
	}
	if err != nil {
		return baseFile{}, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	info, err := file.Stat()
	if err != nil {
		return baseFile{}, err
	}
	if !info.Mode().IsRegular() {
		return baseFile{}, ErrInvalidPath
	}
	// Read via the same descriptor that was checked, not another path lookup.
	if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Nlink != 1 {
		return baseFile{}, ErrInvalidPath
	}
	data, err := io.ReadAll(file)
	return baseFile{data, true, info.Mode().Perm()}, err
}

func (f *FS) captureBaseLocked(agent AgentID, rel string) error {
	d := f.ensureDelta(agent)
	if _, ok := d.bases[rel]; ok {
		return nil
	}
	b, err := f.physical(rel)
	if err != nil {
		return err
	}
	d.bases[rel] = b
	return nil
}

func sameFile(a, b baseFile) bool {
	return a.Present == b.Present && a.Mode == b.Mode && hashOf(a.Content, a.Present) == hashOf(b.Content, b.Present)
}

// syncParent makes a rename or unlink durable on supported native targets.
func (f *FS) syncParent(rel string) (err error) {
	dir, err := f.root.Open(path.Dir(rel))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, dir.Close()) }()
	err = dir.Sync()
	return err
}

// openParent walks directory descriptors without following symlinks.
func (f *FS) openParent(rel string) (*os.File, error) {
	dir, err := f.root.Open(".")
	if err != nil {
		return nil, err
	}
	if path.Dir(rel) == "." {
		return dir, nil
	}
	for _, part := range strings.Split(path.Dir(rel), "/") {
		fd, e := unix.Openat(int(dir.Fd()), part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		closeErr := dir.Close()
		if e != nil {
			return nil, errors.Join(e, closeErr)
		}
		if closeErr != nil {
			return nil, errors.Join(closeErr, unix.Close(fd))
		}
		dir = os.NewFile(uintptr(fd), part)
	}
	return dir, nil
}
