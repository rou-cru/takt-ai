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
	"strings"
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

// TestRestoreByDestinationState covers each destination state restore
// distinguishes: only a missing or unchanged destination is overwritten,
// and the backup always survives.
func TestRestoreByDestinationState(t *testing.T) {
	const (
		path     = "conf/a.txt"
		backup   = ".takt-backups/a.txt"
		prior    = "original"
		deployed = "deployed"
		edited   = "edited"
	)
	tests := []struct {
		name string
		// onDisk is the destination content; empty leaves it missing.
		onDisk string
		// directory replaces the destination with a directory.
		directory bool
		// recordedDigest overrides the installed digest the manifest stores.
		recordedDigest func(string) string
		wantRestored   bool
		wantPreserved  bool
		wantReason     string
	}{
		{name: "missing", wantRestored: true},
		{name: "unchanged", onDisk: deployed, wantRestored: true},
		{name: "unchanged with uppercase recorded digest", onDisk: deployed, recordedDigest: strings.ToUpper, wantRestored: true},
		{name: "changed", onDisk: edited, wantReason: setup.ConflictUnverifiedSinceInstall},
		{name: "unknown recorded digest", onDisk: deployed, recordedDigest: func(string) string { return "not-a-digest" }, wantReason: setup.ConflictUnverifiedSinceInstall},
		{name: "not a regular file", directory: true, wantReason: setup.ConflictDestinationType},
		{name: "already holds the backup", onDisk: prior, recordedDigest: func(string) string { return "" }, wantPreserved: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			restoreFixtureWrite(t, root, backup, []byte(prior))
			if tt.onDisk != "" {
				restoreFixtureWrite(t, root, path, []byte(tt.onDisk))
			}
			if tt.directory {
				restoreFixtureWrite(t, root, path+"/inner", []byte(edited))
			}
			entry := restoreFixtureEntry(t, path, backup, deployed, prior)
			if tt.recordedDigest != nil {
				entry.SHA256 = tt.recordedDigest(entry.SHA256)
			}
			if err := restoreFixtureManifest(t, entry).Save(root); err != nil {
				t.Fatal(err)
			}
			manifest, err := setup.LoadOwnershipManifest(root)
			if err != nil {
				t.Fatal(err)
			}

			result, err := setup.Restore(root, manifest)
			if err != nil {
				t.Fatalf("Restore() error = %v", err)
			}
			if got := slices.Equal(result.Restored, []string{path}); got != tt.wantRestored {
				t.Errorf("Restored = %v, want restored %v", result.Restored, tt.wantRestored)
			}
			if got := slices.Equal(result.Preserved, []string{path}); got != tt.wantPreserved {
				t.Errorf("Preserved = %v, want preserved %v", result.Preserved, tt.wantPreserved)
			}
			switch {
			case tt.wantReason == "" && len(result.Conflicts) != 0:
				t.Errorf("Conflicts = %+v, want none", result.Conflicts)
			case tt.wantReason != "" && (len(result.Conflicts) != 1 || result.Conflicts[0].Reason != tt.wantReason ||
				result.Conflicts[0].ReplacementHash != restoreFixtureHash(t, []byte(prior))):
				t.Errorf("Conflicts = %+v, want one %s conflict naming the replacement", result.Conflicts, tt.wantReason)
			}
			switch {
			case tt.directory:
				if _, err := os.Stat(filepath.Join(root, path, "inner")); err != nil {
					t.Errorf("directory destination removed: %v", err)
				}
			case tt.wantReason != "":
				if got := restoreFixtureRead(t, root, path); got != tt.onDisk {
					t.Errorf("%s = %q, want %q kept", path, got, tt.onDisk)
				}
			default:
				if got := restoreFixtureRead(t, root, path); got != prior {
					t.Errorf("%s = %q, want the backup content %q", path, got, prior)
				}
			}
			if got := restoreFixtureRead(t, root, backup); got != prior {
				t.Errorf("backup = %q, want it retained", got)
			}
		})
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
