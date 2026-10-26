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

package setup_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// restoreFixtureHash digests content the way the ownership manifest records digests.
func restoreFixtureHash(t *testing.T, content []byte) string {
	t.Helper()
	digest := sha256.Sum256(content)
	return hex.EncodeToString(digest[:])
}

// restoreFixtureWrite stores content at a root-relative path, creating parent directories.
func restoreFixtureWrite(t *testing.T, root, rel string, content []byte) {
	t.Helper()
	destination := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", rel, err)
	}
	if err := os.WriteFile(destination, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", rel, err)
	}
}

// restoreFixtureRead returns the current bytes at a root-relative path.
func restoreFixtureRead(t *testing.T, root, rel string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", rel, err)
	}
	return string(content)
}

// restoreFixtureEntry builds a pre-existing ownership entry whose backup holds the
// prior content and whose deployed content Takt recorded at takeover.
func restoreFixtureEntry(t *testing.T, path, backupRel, deployed, prior string) setup.OwnershipEntry {
	t.Helper()
	entry, err := setup.NewOwnershipEntry(path, []byte(deployed), 0o644, true, restoreFixtureHash(t, []byte(prior)), backupRel, setup.TargetOpenCode)
	if err != nil {
		t.Fatalf("NewOwnershipEntry(%q) error = %v", path, err)
	}
	return entry
}

// restoreFixtureManifest builds a manifest from validated entries.
func restoreFixtureManifest(t *testing.T, entries ...setup.OwnershipEntry) *setup.OwnershipManifest {
	t.Helper()
	manifest := setup.NewOwnershipManifest()
	for _, entry := range entries {
		if err := manifest.Add(entry); err != nil {
			t.Fatalf("manifest.Add(%q) error = %v", entry.Path, err)
		}
	}
	return manifest
}

// An edit made after the backup survives restore, the conflict is
// named, and no complete restore is claimed.
func TestRestoreKeepsPostBackupEdit(t *testing.T) {
	root := t.TempDir()
	restoreFixtureWrite(t, root, ".takt-backups/config.yaml", []byte("user original"))
	restoreFixtureWrite(t, root, "config.yaml", []byte("user edit after backup"))
	manifest := restoreFixtureManifest(t, restoreFixtureEntry(t, "config.yaml", ".takt-backups/config.yaml", "takt deployed", "user original"))

	result, err := setup.Restore(root, manifest)
	if err != nil {
		t.Fatalf("Restore() error = %v, want the conflict reported in the result, not an error", err)
	}
	if len(result.Restored) != 0 {
		t.Errorf("Restore() Restored = %v, want none: a conflicted file is never silently restored", result.Restored)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("Restore() Conflicts = %+v, want exactly the edited path", result.Conflicts)
	}
	if conflict := result.Conflicts[0]; conflict.Path != "config.yaml" ||
		conflict.CurrentHash != restoreFixtureHash(t, []byte("user edit after backup")) ||
		conflict.ReplacementHash != restoreFixtureHash(t, []byte("user original")) ||
		conflict.Reason != setup.ConflictModifiedSinceInstall {
		t.Errorf("Restore() conflict = %+v, want the post-backup edit named with its exact hashes", conflict)
	}
	if got := restoreFixtureRead(t, root, "config.yaml"); got != "user edit after backup" {
		t.Errorf("after Restore, config.yaml = %q, want the post-backup edit to survive", got)
	}
}

// Unchanged and absent destinations are eligible, and an
// already-restored destination is a no-op even when the recorded installed
// digest is unknown: identical backup bytes need no write.
func TestRestoreDestinationVariants(t *testing.T) {
	t.Run("unchanged destination restores", func(t *testing.T) {
		root := t.TempDir()
		restoreFixtureWrite(t, root, ".takt-backups/a.txt", []byte("original a"))
		restoreFixtureWrite(t, root, "a.txt", []byte("deployed a"))
		manifest := restoreFixtureManifest(t, restoreFixtureEntry(t, "a.txt", ".takt-backups/a.txt", "deployed a", "original a"))

		result, err := setup.Restore(root, manifest)
		if err != nil {
			t.Fatalf("Restore() error = %v", err)
		}
		if !slices.Equal(result.Restored, []string{"a.txt"}) {
			t.Errorf("Restore() Restored = %v, want [a.txt]", result.Restored)
		}
		if got := restoreFixtureRead(t, root, "a.txt"); got != "original a" {
			t.Errorf("a.txt = %q, want the backed-up content back", got)
		}
	})
	t.Run("absent destination is restorable", func(t *testing.T) {
		root := t.TempDir()
		restoreFixtureWrite(t, root, ".takt-backups/nested-a.txt", []byte("original nested"))
		manifest := restoreFixtureManifest(t, restoreFixtureEntry(t, "conf/nested/a.txt", ".takt-backups/nested-a.txt", "deployed nested", "original nested"))

		result, err := setup.Restore(root, manifest)
		if err != nil {
			t.Fatalf("Restore() error = %v", err)
		}
		if !slices.Equal(result.Restored, []string{"conf/nested/a.txt"}) {
			t.Errorf("Restore() Restored = %v, want the missing destination restored", result.Restored)
		}
		if got := restoreFixtureRead(t, root, "conf/nested/a.txt"); got != "original nested" {
			t.Errorf("conf/nested/a.txt = %q, want the backed-up content written", got)
		}
	})
	t.Run("already-restored destination is a no-op", func(t *testing.T) {
		root := t.TempDir()
		restoreFixtureWrite(t, root, ".takt-backups/a.txt", []byte("original a"))
		restoreFixtureWrite(t, root, "a.txt", []byte("original a"))
		manifest := restoreFixtureManifest(t, restoreFixtureEntry(t, "a.txt", ".takt-backups/a.txt", "deployed a", "original a"))

		result, err := setup.Restore(root, manifest)
		if err != nil {
			t.Fatalf("Restore() error = %v", err)
		}
		if len(result.Restored) != 0 {
			t.Errorf("Restore() Restored = %v, want none: identical bytes are a no-op", result.Restored)
		}
		if !slices.Equal(result.Preserved, []string{"a.txt"}) {
			t.Errorf("Restore() Preserved = %v, want [a.txt]: the already-restored bytes stay", result.Preserved)
		}
		if got := restoreFixtureRead(t, root, "a.txt"); got != "original a" {
			t.Errorf("a.txt = %q, want the already-restored bytes unchanged", got)
		}
	})
	t.Run("already-restored destination with unknown installed hash stays a no-op", func(t *testing.T) {
		root := t.TempDir()
		restoreFixtureWrite(t, root, ".takt-backups/a.txt", []byte("original a"))
		restoreFixtureWrite(t, root, "a.txt", []byte("original a"))
		manifest := restoreFixtureManifest(t, setup.OwnershipEntry{
			Path:        "a.txt",
			Mode:        0o644,
			PreExisting: true,
			PriorSHA256: restoreFixtureHash(t, []byte("original a")),
			BackupPath:  ".takt-backups/a.txt",
			Targets:     []setup.OwnershipTarget{setup.TargetOpenCode},
		})

		result, err := setup.Restore(root, manifest)
		if err != nil {
			t.Fatalf("Restore() error = %v, want none: identical backup bytes are a no-op whatever the recorded digest", err)
		}
		if len(result.Restored) != 0 || len(result.Conflicts) != 0 || len(result.Incomplete) != 0 {
			t.Errorf("Restore() = %+v, want the already-restored path neither restored nor conflicted", result)
		}
		if !slices.Equal(result.Preserved, []string{"a.txt"}) {
			t.Errorf("Restore() Preserved = %v, want [a.txt]", result.Preserved)
		}
		if got := restoreFixtureRead(t, root, "a.txt"); got != "original a" {
			t.Errorf("a.txt = %q, want the already-restored bytes unchanged", got)
		}
	})
}

// An unknown installed hash is a conflict, never evidence of equality.
func TestUnknownInstalledHashIsAConflict(t *testing.T) {
	root := t.TempDir()
	restoreFixtureWrite(t, root, ".takt-backups/a.txt", []byte("original a"))
	restoreFixtureWrite(t, root, "a.txt", []byte("mystery content"))
	manifest := restoreFixtureManifest(t, setup.OwnershipEntry{
		Path:        "a.txt",
		Mode:        0o644,
		PreExisting: true,
		PriorSHA256: restoreFixtureHash(t, []byte("original a")),
		BackupPath:  ".takt-backups/a.txt",
		Targets:     []setup.OwnershipTarget{setup.TargetOpenCode},
	})

	result, err := setup.Restore(root, manifest)
	if err != nil {
		t.Fatalf("Restore() error = %v, want the conflict reported in the result, not an error", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Reason != setup.ConflictUnknownInstalledHash {
		t.Fatalf("Restore() Conflicts = %+v, want one unknown_installed_hash conflict", result.Conflicts)
	}
	if got := restoreFixtureRead(t, root, "a.txt"); got != "mystery content" {
		t.Errorf("a.txt = %q, want the unknown content kept", got)
	}
}

// A type-mismatched destination is a conflict and is never deleted.
func TestDestinationTypeConflictNeverDeletes(t *testing.T) {
	root := t.TempDir()
	restoreFixtureWrite(t, root, ".takt-backups/a.txt", []byte("original a"))
	if err := os.Mkdir(filepath.Join(root, "a.txt"), 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt", "inner.txt"), []byte("inner"), 0o644); err != nil {
		t.Fatalf("WriteFile() inner error = %v", err)
	}
	manifest := restoreFixtureManifest(t, restoreFixtureEntry(t, "a.txt", ".takt-backups/a.txt", "deployed a", "original a"))

	result, err := setup.Restore(root, manifest)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("Restore() Conflicts = %+v, want exactly the mismatched path", result.Conflicts)
	}
	if conflict := result.Conflicts[0]; conflict.Path != "a.txt" ||
		conflict.CurrentHash != "" ||
		conflict.ReplacementHash != restoreFixtureHash(t, []byte("original a")) ||
		conflict.Reason != setup.ConflictDestinationType {
		t.Errorf("Restore() conflict = %+v, want the type mismatch named with the replacement hash", conflict)
	}
	if _, err := os.Stat(filepath.Join(root, "a.txt", "inner.txt")); err != nil {
		t.Fatalf("the directory destination was removed: %v", err)
	}
}

// A path whose backup cannot be read is incomplete and fails the operation,
// while the remaining healthy entries still complete.
func TestMissingBackupIsIncompleteWithError(t *testing.T) {
	root := t.TempDir()
	restoreFixtureWrite(t, root, ".takt-backups/b.txt", []byte("original b"))
	restoreFixtureWrite(t, root, "a.txt", []byte("deployed a"))
	restoreFixtureWrite(t, root, "b.txt", []byte("deployed b"))
	manifest := restoreFixtureManifest(t,
		restoreFixtureEntry(t, "a.txt", ".takt-backups/a.txt", "deployed a", "original a"),
		restoreFixtureEntry(t, "b.txt", ".takt-backups/b.txt", "deployed b", "original b"),
	)

	result, err := setup.Restore(root, manifest)
	if err == nil {
		t.Fatal("Restore() error = nil, want an error naming the missing backup")
	}
	if !slices.Equal(result.Incomplete, []string{"a.txt"}) {
		t.Errorf("Restore() Incomplete = %v, want [a.txt]", result.Incomplete)
	}
	if !slices.Equal(result.Restored, []string{"b.txt"}) {
		t.Errorf("Restore() Restored = %v, want the healthy entry to complete", result.Restored)
	}
	if got := restoreFixtureRead(t, root, "a.txt"); got != "deployed a" {
		t.Errorf("a.txt = %q, want the failed entry untouched", got)
	}
	if got := restoreFixtureRead(t, root, "b.txt"); got != "original b" {
		t.Errorf("b.txt = %q, want the restored backup content", got)
	}
}

// Backups are retained: having or completing a restore never deletes them.
func TestRestoreRetainsBackups(t *testing.T) {
	root := t.TempDir()
	restoreFixtureWrite(t, root, ".takt-backups/a.txt", []byte("original a"))
	restoreFixtureWrite(t, root, "a.txt", []byte("deployed a"))
	manifest := restoreFixtureManifest(t, restoreFixtureEntry(t, "a.txt", ".takt-backups/a.txt", "deployed a", "original a"))

	result, err := setup.Restore(root, manifest)
	if err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if !slices.Equal(result.Restored, []string{"a.txt"}) {
		t.Errorf("Restore() Restored = %v, want [a.txt]", result.Restored)
	}
	if got := restoreFixtureRead(t, root, ".takt-backups/a.txt"); got != "original a" {
		t.Errorf("backup = %q, want the backup retained after the restore", got)
	}
}

// Replaying a restore changes nothing further: a restored file becomes a
// no-op and an edited one stays a conflict.
func TestRestoreReplayIsStable(t *testing.T) {
	root := t.TempDir()
	restoreFixtureWrite(t, root, ".takt-backups/a.txt", []byte("original a"))
	restoreFixtureWrite(t, root, ".takt-backups/b.txt", []byte("original b"))
	restoreFixtureWrite(t, root, "a.txt", []byte("edited a"))
	restoreFixtureWrite(t, root, "b.txt", []byte("deployed b"))
	manifest := restoreFixtureManifest(t,
		restoreFixtureEntry(t, "a.txt", ".takt-backups/a.txt", "deployed a", "original a"),
		restoreFixtureEntry(t, "b.txt", ".takt-backups/b.txt", "deployed b", "original b"),
	)

	first, err := setup.Restore(root, manifest)
	if err != nil {
		t.Fatalf("first Restore() error = %v", err)
	}
	if !slices.Equal(first.Restored, []string{"b.txt"}) || len(first.Conflicts) != 1 {
		t.Fatalf("first Restore() Restored = %v, Conflicts = %+v, want [b.txt] and a.txt in conflict", first.Restored, first.Conflicts)
	}

	second, err := setup.Restore(root, manifest)
	if err != nil {
		t.Fatalf("replayed Restore() error = %v", err)
	}
	if len(second.Restored) != 0 {
		t.Errorf("replayed Restore() Restored = %v, want no repeated writes", second.Restored)
	}
	if !slices.Equal(second.Preserved, []string{"b.txt"}) {
		t.Errorf("replayed Restore() Preserved = %v, want [b.txt]", second.Preserved)
	}
	if len(second.Conflicts) != 1 || second.Conflicts[0].Path != "a.txt" {
		t.Errorf("replayed Restore() Conflicts = %+v, want a.txt", second.Conflicts)
	}
	if got := restoreFixtureRead(t, root, "a.txt"); got != "edited a" {
		t.Errorf("a.txt = %q, want the edit kept", got)
	}
	if got := restoreFixtureRead(t, root, "b.txt"); got != "original b" {
		t.Errorf("b.txt = %q, want the restored content stable", got)
	}
}

// Restore requires a manifest; an empty manifest restores nothing.
func TestRestoreManifestEdgeCases(t *testing.T) {
	if _, err := setup.Restore(t.TempDir(), nil); err == nil {
		t.Fatal("Restore(nil manifest) error = nil, want an error")
	}
	result, err := setup.Restore(t.TempDir(), setup.NewOwnershipManifest())
	if err != nil {
		t.Fatalf("Restore(empty manifest) error = %v, want none", err)
	}
	if len(result.Restored)+len(result.Preserved)+len(result.Conflicts)+len(result.Incomplete) != 0 {
		t.Errorf("Restore(empty manifest) = %+v, want an empty result", result)
	}
}
