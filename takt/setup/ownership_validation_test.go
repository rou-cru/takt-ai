package setup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validPriorSHA is a well-formed 64-hex digest; its value is irrelevant to validation.
const validPriorSHA = "0000000000000000000000000000000000000000000000000000000000000000"

func TestOwnershipTargetFor(t *testing.T) {
	got, err := OwnershipTargetFor("skills")
	if err != nil || got != TargetSkills {
		t.Fatalf("OwnershipTargetFor(skills) = %q, %v; want %q", got, err, TargetSkills)
	}
	if _, err := OwnershipTargetFor("cursor"); err == nil || !strings.Contains(err.Error(), "unsupported target") {
		t.Fatalf("OwnershipTargetFor(cursor) error = %v, want unsupported target", err)
	}
}

func TestNewOwnershipEntryRejectsInvalidInput(t *testing.T) {
	const mode = os.FileMode(0o644)
	tests := []struct {
		name    string
		path    string
		content string
		mode    os.FileMode
		pre     bool
		prior   string
		targets []OwnershipTarget
		wantErr string
	}{
		{"escaping path", "../x", "c", mode, false, "", []OwnershipTarget{TargetOpenCode}, "invalid ownership entry path"},
		{"empty content", "a.md", "", mode, false, "", []OwnershipTarget{TargetOpenCode}, "requires managed content"},
		{"zero mode", "a.md", "c", 0, false, "", []OwnershipTarget{TargetOpenCode}, "non-zero file mode"},
		{"pre-existing without prior hash", "a.md", "c", mode, true, "", []OwnershipTarget{TargetOpenCode}, "pre-existing requires a valid prior SHA-256"},
		{"prior hash not hex", "a.md", "c", mode, false, "zz", []OwnershipTarget{TargetOpenCode}, "requires a valid prior SHA-256"},
		{"prior hash wrong length", "a.md", "c", mode, false, "abcd", []OwnershipTarget{TargetOpenCode}, "requires a valid prior SHA-256"},
		{"unknown target", "a.md", "c", mode, false, "", []OwnershipTarget{"cursor"}, "unknown target"},
		{"duplicate target", "a.md", "c", mode, false, "", []OwnershipTarget{TargetSkills, TargetSkills}, "duplicates target"},
		{"no targets", "a.md", "c", mode, false, "", nil, "at least one target"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewOwnershipEntry(tt.path, []byte(tt.content), tt.mode, tt.pre, tt.prior, "", tt.targets...)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestNewOwnershipEntrySortsTargetsAndRecordsPrior(t *testing.T) {
	entry, err := NewOwnershipEntry("a/./b.md", []byte("c"), 0o100640, true, validPriorSHA, "backup/b.md", TargetSkills, TargetOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Path != "a/b.md" || entry.Mode != 0o640 || entry.PriorSHA256 != validPriorSHA || entry.BackupPath != "backup/b.md" || !entry.PreExisting {
		t.Fatalf("entry = %+v", entry)
	}
	if len(entry.Targets) != 2 || entry.Targets[0] != TargetOpenCode || entry.Targets[1] != TargetSkills {
		t.Fatalf("targets = %v, want sorted [opencode skills]", entry.Targets)
	}
}

func TestManifestAddRejectsForeignVersion(t *testing.T) {
	manifest := NewOwnershipManifest()
	manifest.Version = OwnershipManifestVersion + 1
	if err := manifest.Add(OwnershipEntry{Path: "a"}); err == nil || !strings.Contains(err.Error(), "cannot add entries") {
		t.Fatalf("Add() error = %v, want version rejection", err)
	}
}

func TestLoadOwnershipManifestRejectsEscapingEntryAndDefaultsEntries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, OwnershipManifestFilename)
	if err := os.WriteFile(path, []byte(`{"version":1,"entries":{"../victim.txt":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOwnershipManifest(root); err == nil || !strings.Contains(err.Error(), "escapes deployment root") {
		t.Fatalf("error = %v, want escape rejection", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	manifest, err := LoadOwnershipManifest(root)
	if err != nil || manifest.Entries == nil {
		t.Fatalf("manifest = %+v, err = %v; want non-nil Entries", manifest, err)
	}
}

func TestSafeJoin(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"", "  ", "/abs", "../up", "a/../../up", "link/file"} {
		if _, err := SafeJoin(root, rel); err == nil {
			t.Errorf("SafeJoin(%q) succeeded, want error", rel)
		}
	}
	if _, err := SafeJoin(filepath.Join(root, "missing"), "a"); err == nil {
		t.Error("SafeJoin with unresolvable root succeeded, want error")
	}
	got, err := SafeJoin(root, "new/dir/file")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, filepath.Join("new", "dir", "file")) {
		t.Errorf("SafeJoin() = %q", got)
	}
}
