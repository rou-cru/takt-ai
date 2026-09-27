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
	"os"
	"path/filepath"
	"testing"
)

func TestWriteFileAtomicReadOnlyDirRelaxesOwnerWritePermission(t *testing.T) {
	base := t.TempDir()
	skillDir := filepath.Join(base, "analyst")
	if err := os.Mkdir(skillDir, 0o555); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	path := filepath.Join(skillDir, "SKILL.md")
	content := []byte("# SDD Init\n")

	_, err := WriteFileAtomic(path, content, 0o644)
	if err != nil {
		t.Fatalf("WriteFileAtomic() error = %v, want successful write with permission relaxation", err)
	}

	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("ReadFile() error = %v", readErr)
	}
	if string(got) != string(content) {
		t.Fatalf("file content = %q, want %q", string(got), string(content))
	}
}

func TestWriteFileAtomicCreatesAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	content := []byte("{\"ok\":true}\n")

	first, err := WriteFileAtomic(path, content, 0o644)
	if err != nil {
		t.Fatalf("WriteFileAtomic() first write error = %v", err)
	}

	if !first.Changed {
		t.Fatalf("WriteFileAtomic() first write result = %+v", first)
	}

	second, err := WriteFileAtomic(path, content, 0o644)
	if err != nil {
		t.Fatalf("WriteFileAtomic() second write error = %v", err)
	}

	if second.Changed {
		t.Fatalf("WriteFileAtomic() second write result = %+v", second)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	if string(got) != string(content) {
		t.Fatalf("file content = %q", string(got))
	}
}

func TestWriteFileAtomicRejectsExistingSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("old\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(target) error = %v", err)
	}
	path := filepath.Join(dir, "linked.txt")
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}

	_, err := WriteFileAtomic(path, []byte("new\n"), 0o644)
	if err == nil || err.Error() == "" {
		t.Fatalf("WriteFileAtomic(symlink) error = %v, want rejection", err)
	}
	if got, readErr := os.ReadFile(target); readErr != nil || string(got) != "old\n" {
		t.Fatalf("target content changed through symlink: got %q err=%v", got, readErr)
	}
}

func TestWriteFileAtomicRejectsOversizedExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "big.txt")
	data := make([]byte, maxAtomicFileSize+1)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(big) error = %v", err)
	}

	_, err := WriteFileAtomic(path, []byte("small\n"), 0o644)
	if err == nil {
		t.Fatal("WriteFileAtomic(big) error = nil, want max-size rejection")
	}
}

func TestWriteFileAtomicFollowsSymlinkParentDirectory(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatalf("Mkdir(realDir) error = %v", err)
	}
	linkDir := filepath.Join(base, "linked")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("Symlink(linkDir) error = %v", err)
	}

	content := []byte("value\n")
	path := filepath.Join(linkDir, "config.txt")
	_, err := WriteFileAtomic(path, content, 0o644)
	if err != nil {
		t.Fatalf("WriteFileAtomic() via symlink parent error = %v, want success", err)
	}

	// Verify the file was written to the real directory.
	realPath := filepath.Join(realDir, "config.txt")
	got, readErr := os.ReadFile(realPath)
	if readErr != nil {
		t.Fatalf("ReadFile(realPath) error = %v", readErr)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

// TestWriteFileAtomicPropagatesSyncDirError verifies that any syncDirFn
// error is propagated — no silent swallowing.
func TestWriteFileAtomicPropagatesSyncDirError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.json")
	content := []byte("{\"ok\":true}\n")

	origSyncDir := syncDirFn
	t.Cleanup(func() {
		syncDirFn = origSyncDir
	})

	syncDirFn = func(string) error { return os.ErrPermission }

	_, err := WriteFileAtomic(path, content, 0o644)
	if err == nil {
		t.Fatal("WriteFileAtomic() error = nil, want sync parent directory failure")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("WriteFileAtomic() error = %v, want ErrPermission", err)
	}
}
