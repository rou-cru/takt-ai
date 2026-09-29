// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package filemerge

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// File modes merged writes use; private modes protect merge scratch space.
const (
	// DefaultFileMode is the owner-writable mode used when callers omit a mode.
	DefaultFileMode fs.FileMode = 0o644
	// PrivateDirectoryMode keeps temporary merge directories owner-only.
	PrivateDirectoryMode fs.FileMode = 0o700
	// ExecutableDirectoryMode preserves traversal and execution for installed tools.
	ExecutableDirectoryMode fs.FileMode = 0o755
)

// SyncDir flushes a directory entry to disk, tolerating filesystems that
// reject directory synchronization (EINVAL).
func SyncDir(dir string) error {
	fd, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("open parent directory %q: %w", dir, err)
	}
	defer func() { _ = fd.Close() }()
	if err := fd.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return fmt.Errorf("sync parent directory %q: %w", dir, err)
	}
	return nil
}

// StageTempFile writes content to a temp file in dir with the given mode and
// returns its path. The caller owns the rename; on error nothing is left behind.
func StageTempFile(dir, pattern string, content []byte, perm fs.FileMode) (_ string, err error) {
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if _, err = tmp.Write(content); err != nil {
		return "", err
	}
	if err = tmp.Chmod(perm); err != nil {
		return "", err
	}
	if err = tmp.Sync(); err != nil {
		return "", err
	}
	if err = tmp.Close(); err != nil {
		return "", err
	}
	return name, nil
}

// syncDirFn is the directory-sync seam atomic writes call; tests override it.
var syncDirFn = SyncDir

const maxAtomicFileSize = 16 << 20

// WriteResult reports the outcome of an atomic file write.
type WriteResult struct {
	Changed bool // true when the file content differs from what was on disk
}

// WriteFileAtomic writes content to path atomically using a temporary file
// and rename; identical content skips the write and reports Changed false.
func WriteFileAtomic(path string, content []byte, perm fs.FileMode) (WriteResult, error) {
	if perm == 0 {
		perm = DefaultFileMode
	}

	existing, err := readComparableFile(path)
	if err == nil {
		if bytes.Equal(existing, content) {
			return WriteResult{}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return WriteResult{}, fmt.Errorf("read existing file %q: %w", path, err)
	}

	dir := filepath.Dir(path)
	if err := ensureAtomicParentDir(dir, path); err != nil {
		return WriteResult{}, err
	}

	tmpPath, err := StageTempFile(dir, ".takt-ai-*.tmp", content, perm)
	if err != nil {
		return WriteResult{}, fmt.Errorf("stage temp file for %q: %w", path, err)
	}

	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := os.Rename(tmpPath, path); err != nil {
		return WriteResult{}, fmt.Errorf("replace %q atomically: %w", path, err)
	}

	// Sync the parent directory to flush the new directory entry to disk.
	if err := syncDirFn(dir); err != nil {
		return WriteResult{}, fmt.Errorf("sync parent directory for %q: %w", path, err)
	}

	cleanup = false
	return WriteResult{Changed: true}, nil
}

func readComparableFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("refusing to read symlink %q", path)
	}
	if info.Size() > maxAtomicFileSize {
		return nil, fmt.Errorf("file %q exceeds max atomic compare size %d bytes", path, maxAtomicFileSize)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxAtomicFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxAtomicFileSize {
		return nil, fmt.Errorf("file %q exceeds max atomic compare size %d bytes", path, maxAtomicFileSize)
	}
	return data, nil
}

func ensureAtomicParentDir(dir, path string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, PrivateDirectoryMode); err != nil {
			return fmt.Errorf("create parent directories for %q: %w", path, err)
		}
		info, err = os.Lstat(dir)
	}
	if err != nil {
		return fmt.Errorf("stat parent directory for %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		// Parent is a symlink (e.g. ~/.config/opencode/agents → dotfiles repo).
		// Resolve the target and continue checks against the real directory.
		resolved, err := filepath.EvalSymlinks(dir)
		if err != nil {
			return fmt.Errorf("resolving symlink parent %q for %q: %w", dir, path, err)
		}
		info, err = os.Stat(resolved)
		if err != nil {
			return fmt.Errorf("stat symlink target %q for %q: %w", resolved, path, err)
		}
		dir = resolved
	}
	if !info.IsDir() {
		return fmt.Errorf("parent path %q for %q is not a directory", dir, path)
	}
	if info.Mode().Perm()&0o200 == 0 {
		if err := os.Chmod(dir, ExecutableDirectoryMode); err != nil {
			return fmt.Errorf("relax parent directory permissions for %q: %w", path, err)
		}
	}
	return nil
}
