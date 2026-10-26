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

package filemerge_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

// TestMergeJSONFileKeepsOwnerOnlyMode checks that merging into a config the
// user made owner-only, which may hold MCP credentials, keeps it owner-only.
func TestMergeJSONFileKeepsOwnerOnlyMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	if err := os.WriteFile(path, []byte(`{"mcp":{"x":{"environment":{"TOKEN":"s"}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := filemerge.MergeJSONFile(path, []byte(`{"theme":"dark"}`)); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, %v; want 0600", info, err)
	}
}

// TestEditFileAtomicCreatesWithDefaultMode checks a new file gets the default mode.
func TestEditFileAtomicCreatesWithDefaultMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if _, err := filemerge.EditFileAtomic(path, []byte("x")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != filemerge.DefaultFileMode {
		t.Fatalf("mode = %v, %v; want %v", info, err, filemerge.DefaultFileMode)
	}
}

// TestWriteFileAtomicAddsOnlyOwnerWriteToParent checks that writing into a
// read-only directory adds owner write without opening it to other users.
func TestWriteFileAtomicAddsOnlyOwnerWriteToParent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, err := filemerge.WriteFileAtomic(filepath.Join(dir, "f"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("directory mode = %v, %v; want 0700", info, err)
	}
}
