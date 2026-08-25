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

package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	testPriorSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testSHA      = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func testEntries(t *testing.T) []OwnershipEntry {
	t.Helper()
	managed, err := NewOwnershipEntry(".claude/settings.json", []byte("settings"), 0o644, true, testPriorSHA, "", TargetClaude, TargetOpenCode)
	if err != nil {
		t.Fatalf("NewOwnershipEntry managed: %v", err)
	}
	codex, err := NewOwnershipEntry(".codex/config.toml", []byte("config"), 0o600, false, "", "", TargetCodex)
	if err != nil {
		t.Fatalf("NewOwnershipEntry codex: %v", err)
	}
	return []OwnershipEntry{managed, codex}
}

func TestOwnershipManifestRoundtrip(t *testing.T) {
	root := t.TempDir()
	manifest := NewOwnershipManifest()
	if err := manifest.Add(testEntries(t)...); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := manifest.Save(root); err != nil {
		t.Fatalf("Save: %v", err)
	}

	firstWrite, err := os.ReadFile(filepath.Join(root, OwnershipManifestFilename))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	if err := manifest.Save(root); err != nil {
		t.Fatalf("idempotent Save: %v", err)
	}
	secondWrite, err := os.ReadFile(filepath.Join(root, OwnershipManifestFilename))
	if err != nil {
		t.Fatalf("re-read manifest: %v", err)
	}
	if string(firstWrite) != string(secondWrite) {
		t.Errorf("Save is not deterministic:\nfirst:  %s\nsecond: %s", firstWrite, secondWrite)
	}

	loaded, err := LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("LoadOwnershipManifest: %v", err)
	}
	if loaded.Version != OwnershipManifestVersion {
		t.Errorf("loaded version = %d, want %d", loaded.Version, OwnershipManifestVersion)
	}
	if !reflect.DeepEqual(loaded.Entries, manifest.Entries) {
		t.Errorf("roundtrip mismatch:\nwant: %+v\ngot:  %+v", manifest.Entries, loaded.Entries)
	}
}

func TestLoadOwnershipManifestErrors(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name:    "unknown future version",
			content: `{"version":99,"entries":{}}`,
			wantErr: "unsupported ownership manifest version 99",
		},
		{
			name:    "corrupt json",
			content: `{"version":1,"entries":`,
			wantErr: "parse ownership manifest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, OwnershipManifestFilename), []byte(tt.content), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			_, err := LoadOwnershipManifest(root)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}

	t.Run("missing file reports ErrNotExist", func(t *testing.T) {
		_, err := LoadOwnershipManifest(t.TempDir())
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("error = %v, want os.ErrNotExist", err)
		}
	})
}

func TestOwnershipManifestIsManaged(t *testing.T) {
	manifest := NewOwnershipManifest()
	if err := manifest.Add(testEntries(t)...); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "exact path", path: ".claude/settings.json", want: true},
		{name: "uncleaned relative path", path: "./.codex/config.toml", want: true},
		{name: "unmanaged path", path: ".claude/agents/x.md", want: false},
		{name: "empty path", path: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := manifest.IsManaged(tt.path); got != tt.want {
				t.Errorf("IsManaged(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}

func TestOwnershipManifestEntriesForTarget(t *testing.T) {
	manifest := NewOwnershipManifest()
	if err := manifest.Add(testEntries(t)...); err != nil {
		t.Fatalf("Add: %v", err)
	}
	tests := []struct {
		name      string
		target    OwnershipTarget
		wantPaths []string
	}{
		{name: "shared entry appears for claude", target: TargetClaude, wantPaths: []string{".claude/settings.json"}},
		{name: "shared entry appears for opencode", target: TargetOpenCode, wantPaths: []string{".claude/settings.json"}},
		{name: "single owner", target: TargetCodex, wantPaths: []string{".codex/config.toml"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := manifest.EntriesForTarget(tt.target)
			paths := make([]string, 0, len(got))
			for _, entry := range got {
				paths = append(paths, entry.Path)
			}
			if !reflect.DeepEqual(paths, tt.wantPaths) {
				t.Errorf("EntriesForTarget(%q) paths = %v, want %v", tt.target, paths, tt.wantPaths)
			}
		})
	}

	t.Run("unknown target yields empty slice", func(t *testing.T) {
		if got := manifest.EntriesForTarget("nope"); len(got) != 0 {
			t.Errorf("expected empty slice, got %d entries", len(got))
		}
	})
}

func TestNewOwnershipEntryValidation(t *testing.T) {
	digest := sha256.Sum256([]byte("prior"))
	validSHA := hex.EncodeToString(digest[:])
	tests := []struct {
		name    string
		path    string
		content []byte
		mode    os.FileMode
		pre     bool
		prior   string
		targets []OwnershipTarget
		wantErr string
	}{
		{name: "backslash path", path: `.claude\x.md`, content: []byte("x"), mode: 0o644, targets: []OwnershipTarget{TargetClaude}, wantErr: "invalid ownership entry path"},
		{name: "parent traversal", path: "../escape.md", content: []byte("x"), mode: 0o644, targets: []OwnershipTarget{TargetClaude}, wantErr: "invalid ownership entry path"},
		{name: "absolute path", path: "/etc/passwd", content: []byte("x"), mode: 0o644, targets: []OwnershipTarget{TargetClaude}, wantErr: "invalid ownership entry path"},
		{name: "empty content", path: ".claude/a.md", content: nil, mode: 0o644, targets: []OwnershipTarget{TargetClaude}, wantErr: "requires managed content"},
		{name: "zero mode", path: ".claude/a.md", content: []byte("x"), mode: 0, targets: []OwnershipTarget{TargetClaude}, wantErr: "non-zero file mode"},
		{name: "pre-existing without prior hash", path: ".claude/a.md", content: []byte("x"), mode: 0o644, pre: true, targets: []OwnershipTarget{TargetClaude}, wantErr: "valid prior SHA-256"},
		{name: "prior hash without pre-existing", path: ".claude/a.md", content: []byte("x"), mode: 0o644, prior: validSHA, targets: []OwnershipTarget{TargetClaude}, wantErr: "not marked pre-existing"},
		{name: "no targets", path: ".claude/a.md", content: []byte("x"), mode: 0o644, targets: nil, wantErr: "at least one target"},
		{name: "unknown target", path: ".claude/a.md", content: []byte("x"), mode: 0o644, targets: []OwnershipTarget{"cursor"}, wantErr: "unknown target"},
		{name: "duplicate target", path: ".claude/a.md", content: []byte("x"), mode: 0o644, targets: []OwnershipTarget{TargetClaude, TargetClaude}, wantErr: "duplicates target"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewOwnershipEntry(tt.path, tt.content, tt.mode, tt.pre, tt.prior, "", tt.targets...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}

	t.Run("computes sha256 of managed content", func(t *testing.T) {
		entry, err := NewOwnershipEntry("./.claude/a.md", []byte("hello"), 0o644, false, "", "", TargetOpenCode)
		if err != nil {
			t.Fatalf("NewOwnershipEntry: %v", err)
		}
		sum := sha256.Sum256([]byte("hello"))
		if entry.SHA256 != hex.EncodeToString(sum[:]) {
			t.Errorf("SHA256 = %q, want digest of content", entry.SHA256)
		}
		if entry.Path != ".claude/a.md" {
			t.Errorf("Path = %q, want normalized .claude/a.md", entry.Path)
		}
	})
}

func TestOwnershipManifestAddUpsertsByPath(t *testing.T) {
	manifest := NewOwnershipManifest()
	original, err := NewOwnershipEntry(".claude/a.md", []byte("one"), 0o644, false, "", "", TargetClaude)
	if err != nil {
		t.Fatalf("NewOwnershipEntry: %v", err)
	}
	replacement, err := NewOwnershipEntry(".claude/a.md", []byte("two"), 0o600, false, "", "", TargetCodex)
	if err != nil {
		t.Fatalf("NewOwnershipEntry replacement: %v", err)
	}
	if err := manifest.Add(original); err != nil {
		t.Fatalf("Add original: %v", err)
	}
	if err := manifest.Add(replacement); err != nil {
		t.Fatalf("Add replacement: %v", err)
	}
	if got := len(manifest.Entries); got != 1 {
		t.Fatalf("entry count = %d, want 1 (upsert by path)", got)
	}
	if !reflect.DeepEqual(manifest.Entries[".claude/a.md"], replacement) {
		t.Errorf("entry was not replaced: %+v", manifest.Entries[".claude/a.md"])
	}
}

func TestOwnershipManifestSchemaExample(t *testing.T) {
	manifest := NewOwnershipManifest()
	if err := manifest.Add(testEntries(t)...); err != nil {
		t.Fatalf("Add: %v", err)
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var probe struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal schema probe: %v", err)
	}
	if probe.Version != OwnershipManifestVersion {
		t.Errorf("serialized version = %d, want %d", probe.Version, OwnershipManifestVersion)
	}
}
