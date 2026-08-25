package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestApplyTargetPlans(t *testing.T) {
	tests := []struct {
		name  string
		plans []TargetPlan
		want  []string
	}{
		{
			name: "Claude only",
			plans: []TargetPlan{{
				Target:       "claude",
				ManagedPaths: []string{".claude/CLAUDE.md"},
				Artifacts:    []Artifact{{Path: ".claude/CLAUDE.md", Content: []byte("claude\n")}},
			}},
			want: []string{".claude/CLAUDE.md"},
		},
		{
			name: "Codex only",
			plans: []TargetPlan{{
				Target:       "codex",
				ManagedPaths: []string{".codex/AGENTS.md"},
				Artifacts:    []Artifact{{Path: ".codex/AGENTS.md", Content: []byte("codex\n")}},
			}},
			want: []string{".codex/AGENTS.md"},
		},
		{
			name: "both targets",
			plans: []TargetPlan{
				{Target: "codex", ManagedPaths: []string{".codex/AGENTS.md"}, Artifacts: []Artifact{{Path: ".codex/AGENTS.md", Content: []byte("codex\n")}}},
				{Target: "claude", ManagedPaths: []string{".claude/CLAUDE.md"}, Artifacts: []Artifact{{Path: ".claude/CLAUDE.md", Content: []byte("claude\n")}}},
			},
			want: []string{".claude/CLAUDE.md", ".codex/AGENTS.md"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			result, err := Apply(root, tc.plans)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if !slices.Equal(result.Changed, tc.want) {
				t.Errorf("Apply() changed = %v, want %v", result.Changed, tc.want)
			}
			second, err := Apply(root, tc.plans)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			if len(second.Changed) != 0 || !slices.Equal(second.Unchanged, tc.want) {
				t.Errorf("second Apply() result = %#v, want unchanged %v", second, tc.want)
			}
		})
	}
}

func TestApplyIsDeterministicAndPreservesUnrelatedFiles(t *testing.T) {
	root := t.TempDir()
	userPath := filepath.Join(root, "user.txt")
	if err := os.WriteFile(userPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(root, "stale.txt")
	if err := os.WriteFile(stalePath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}

	plans := []TargetPlan{
		{Target: "codex", ManagedPaths: []string{"b.txt", "stale.txt"}, Artifacts: []Artifact{{Path: "b.txt", Content: []byte("b")}}},
		{Target: "claude", ManagedPaths: []string{"a.txt"}, Artifacts: []Artifact{{Path: "a.txt", Content: []byte("a")}}},
	}
	result, err := Apply(root, plans)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Changed, []string{"a.txt", "b.txt"}) {
		t.Errorf("changed paths = %v, want sorted paths", result.Changed)
	}
	if got, err := os.ReadFile(userPath); err != nil || string(got) != "keep" {
		t.Errorf("user file = %q, error = %v", got, err)
	}
	if got, err := os.ReadFile(stalePath); err != nil || string(got) != "stale" {
		t.Errorf("stale managed file = %q, error = %v", got, err)
	}
}

func TestApplyRejectsPlansBeforeWriting(t *testing.T) {
	valid := TargetPlan{
		Target:       "claude",
		ManagedPaths: []string{"valid.txt"},
		Artifacts:    []Artifact{{Path: "valid.txt", Content: []byte("must not write")}},
	}
	tests := []struct {
		name  string
		plans []TargetPlan
		want  string
	}{
		{name: "no plans", want: "at least one target plan"},
		{name: "missing identity", plans: []TargetPlan{{ManagedPaths: []string{"file"}}}, want: "identity is required"},
		{name: "empty manifest", plans: []TargetPlan{{Target: "claude"}}, want: "no managed paths"},
		{name: "duplicate targets", plans: []TargetPlan{{Target: "claude", ManagedPaths: []string{"a"}}, {Target: "claude", ManagedPaths: []string{"b"}}}, want: "duplicate target plan"},
		{name: "unmanaged artifact", plans: []TargetPlan{{Target: "claude", ManagedPaths: []string{"managed"}, Artifacts: []Artifact{{Path: "user", Content: []byte("bad")}}}}, want: "is not managed"},
		{name: "duplicate managed paths", plans: []TargetPlan{{Target: "claude", ManagedPaths: []string{"shared"}}, {Target: "codex", ManagedPaths: []string{"shared"}}}, want: "belongs to targets"},
		{name: "cross-target artifact collision", plans: []TargetPlan{{Target: "claude", ManagedPaths: []string{"shared"}, Artifacts: []Artifact{{Path: "shared", Content: []byte("a")}}}, {Target: "codex", ManagedPaths: []string{"shared"}, Artifacts: []Artifact{{Path: "shared", Content: []byte("b")}}}}, want: "belongs to targets"},
		{name: "invalid plan after valid plan", plans: []TargetPlan{valid, {Target: "codex", ManagedPaths: []string{"../escape"}, Artifacts: []Artifact{{Path: "../escape", Content: []byte("bad")}}}}, want: "path must not contain '..'"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			_, err := Apply(root, tc.plans)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Apply() error = %v, want substring %q", err, tc.want)
			}
			entries, readErr := os.ReadDir(root)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if len(entries) != 0 {
				t.Fatalf("rejected deployment created entries: %v", entries)
			}
		})
	}
}

func TestApplyRecordsOwnershipManifest(t *testing.T) {
	root := t.TempDir()
	preExisting := filepath.Join(root, "shared", "keep.md")
	if err := os.MkdirAll(filepath.Dir(preExisting), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(preExisting, []byte("user bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	plans := []TargetPlan{{
		Target:       "claude",
		ManagedPaths: []string{"shared/keep.md", "takt/new.md"},
		Artifacts: []Artifact{
			{Path: "shared/keep.md", Content: []byte("takt bytes")},
			{Path: "takt/new.md", Content: []byte("created")},
		},
	}}
	if _, err := Apply(root, plans); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	manifest, err := LoadOwnershipManifest(root)
	if err != nil {
		t.Fatalf("LoadOwnershipManifest() error = %v", err)
	}
	takenOver, exists := manifest.Entries["shared/keep.md"]
	if !exists {
		t.Fatal("manifest has no entry for shared/keep.md")
	}
	priorDigest := sha256.Sum256([]byte("user bytes"))
	if !takenOver.PreExisting || takenOver.PriorSHA256 != hex.EncodeToString(priorDigest[:]) {
		t.Errorf("taken-over entry = %+v, want pre-existing with prior hash of user bytes", takenOver)
	}
	if takenOver.Mode != 0o600 {
		t.Errorf("taken-over mode = %#o, want %#o", takenOver.Mode, 0o600)
	}
	created, exists := manifest.Entries["takt/new.md"]
	if !exists {
		t.Fatal("manifest has no entry for takt/new.md")
	}
	if created.PreExisting || created.PriorSHA256 != "" {
		t.Errorf("created entry = %+v, want non-pre-existing without prior hash", created)
	}
	if !slices.Equal(created.Targets, []OwnershipTarget{TargetClaude}) {
		t.Errorf("created targets = %v, want [claude]", created.Targets)
	}
}

func TestUninstallPreservesPreExistingAndRemovesCreated(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "shared", "keep.md"), []byte("user bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	plans := []TargetPlan{{
		Target:       "claude",
		ManagedPaths: []string{"shared/keep.md", "takt/deep/new.md"},
		Artifacts: []Artifact{
			{Path: "shared/keep.md", Content: []byte("takt bytes")},
			{Path: "takt/deep/new.md", Content: []byte("created")},
		},
	}}
	if _, err := Apply(root, plans); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}

	result, err := Uninstall(root, TargetClaude)
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if !slices.Equal(result.Preserved, []string{"shared/keep.md"}) {
		t.Errorf("preserved = %v, want [shared/keep.md]", result.Preserved)
	}
	if !slices.Equal(result.Removed, []string{"takt/deep/new.md"}) {
		t.Errorf("removed = %v, want [takt/deep/new.md]", result.Removed)
	}
	kept, err := os.ReadFile(filepath.Join(root, "shared", "keep.md"))
	if err != nil || string(kept) != "takt bytes" {
		t.Errorf("pre-existing file = %q, error = %v; want managed bytes left in place", kept, err)
	}
	info, err := os.Stat(filepath.Join(root, "shared", "keep.md"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("pre-existing mode = %v, error = %v; want original 0600 untouched by uninstall", info, err)
	}
	if _, err := os.Stat(filepath.Join(root, "takt")); !os.IsNotExist(err) {
		t.Errorf("emptied parent dir stat error = %v, want removed", err)
	}
	_, manifestErr := LoadOwnershipManifest(root)
	if manifestErr == nil || !errors.Is(manifestErr, os.ErrNotExist) {
		t.Errorf("manifest load error = %v, want removed when no entries remain", manifestErr)
	}
}

func TestUninstallScopesToSelectedTargets(t *testing.T) {
	tests := []struct {
		name        string
		uninstall   OwnershipTarget
		wantRemoved []string
		wantKept    map[string]string
	}{
		{
			name:        "other target's files intact",
			uninstall:   TargetClaude,
			wantRemoved: []string{"claude/a.md"},
			wantKept:    map[string]string{"codex/b.md": "codex\n"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			plans := []TargetPlan{
				{Target: "claude", ManagedPaths: []string{"claude/a.md"}, Artifacts: []Artifact{{Path: "claude/a.md", Content: []byte("claude\n")}}},
				{Target: "codex", ManagedPaths: []string{"codex/b.md"}, Artifacts: []Artifact{{Path: "codex/b.md", Content: []byte("codex\n")}}},
			}
			if _, err := Apply(root, plans); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}

			result, err := Uninstall(root, tc.uninstall)
			if err != nil {
				t.Fatalf("Uninstall() error = %v", err)
			}
			if !slices.Equal(result.Removed, tc.wantRemoved) {
				t.Errorf("removed = %v, want %v", result.Removed, tc.wantRemoved)
			}
			for path, want := range tc.wantKept {
				got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
				if err != nil || string(got) != want {
					t.Errorf("file %q = %q, error = %v; want %q", path, got, err, want)
				}
			}
			manifest, err := LoadOwnershipManifest(root)
			if err != nil {
				t.Fatalf("manifest should survive partial uninstall: %v", err)
			}
			for _, entry := range manifest.Entries {
				if slices.Contains(entry.Targets, tc.uninstall) {
					t.Errorf("entry %q still owned by uninstalled target %q", entry.Path, tc.uninstall)
				}
			}
		})
	}

	t.Run("shared entry keeps file for remaining owner", func(t *testing.T) {
		root := t.TempDir()
		plans := []TargetPlan{
			{Target: "claude", ManagedPaths: []string{"claude/a.md"}, Artifacts: []Artifact{{Path: "claude/a.md", Content: []byte("claude\n")}}},
		}
		if _, err := Apply(root, plans); err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, "shared"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "shared", "c.md"), []byte("shared\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		entry, err := NewOwnershipEntry("shared/c.md", []byte("shared\n"), 0o644, false, "", "", TargetClaude, TargetCodex)
		if err != nil {
			t.Fatal(err)
		}
		manifest, err := LoadOwnershipManifest(root)
		if err != nil {
			t.Fatal(err)
		}
		if err := manifest.Add(entry); err != nil {
			t.Fatal(err)
		}
		if err := manifest.Save(root); err != nil {
			t.Fatal(err)
		}

		result, err := Uninstall(root, TargetClaude)
		if err != nil {
			t.Fatalf("Uninstall() error = %v", err)
		}
		if !slices.Equal(result.Removed, []string{"claude/a.md"}) {
			t.Errorf("removed = %v, want [claude/a.md]", result.Removed)
		}
		got, err := os.ReadFile(filepath.Join(root, "shared", "c.md"))
		if err != nil || string(got) != "shared\n" {
			t.Errorf("shared file = %q, error = %v; want kept for codex", got, err)
		}
		manifest, err = LoadOwnershipManifest(root)
		if err != nil {
			t.Fatal(err)
		}
		kept := manifest.Entries["shared/c.md"]
		if !slices.Equal(kept.Targets, []OwnershipTarget{TargetCodex}) {
			t.Errorf("remaining owners = %v, want [codex]", kept.Targets)
		}
	})
}

func TestUninstallMissingManifestIsNoOp(t *testing.T) {
	result, err := Uninstall(t.TempDir(), TargetClaude, TargetCodex)
	if err != nil {
		t.Fatalf("Uninstall() error = %v", err)
	}
	if len(result.Removed) != 0 || len(result.Preserved) != 0 {
		t.Errorf("result = %#v, want empty", result)
	}
}

func TestUninstallIsIdempotent(t *testing.T) {
	root := t.TempDir()
	plans := []TargetPlan{
		{Target: "claude", ManagedPaths: []string{"a.md"}, Artifacts: []Artifact{{Path: "a.md", Content: []byte("a")}}},
	}
	if _, err := Apply(root, plans); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	first, err := Uninstall(root, TargetClaude)
	if err != nil {
		t.Fatalf("first Uninstall() error = %v", err)
	}
	if !slices.Equal(first.Removed, []string{"a.md"}) {
		t.Errorf("first removed = %v, want [a.md]", first.Removed)
	}
	second, err := Uninstall(root, TargetClaude)
	if err != nil {
		t.Fatalf("second Uninstall() error = %v", err)
	}
	if len(second.Removed) != 0 || len(second.Preserved) != 0 {
		t.Errorf("second uninstall result = %#v, want empty no-op", second)
	}
}

func TestUninstallRejectsBadInput(t *testing.T) {
	tests := []struct {
		name    string
		content string
		targets []OwnershipTarget
		wantErr string
	}{
		{name: "corrupt manifest", content: "{not json", targets: []OwnershipTarget{TargetClaude}, wantErr: "parse ownership manifest"},
		{name: "unknown target", targets: []OwnershipTarget{"cursor"}, wantErr: "unknown ownership target"},
		{name: "no targets", targets: nil, wantErr: "at least one ownership target"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.content != "" {
				if err := os.WriteFile(filepath.Join(root, OwnershipManifestFilename), []byte(tc.content), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			_, err := Uninstall(root, tc.targets...)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Uninstall() error = %v, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestSyncPreservesUserEditedFiles(t *testing.T) {
	tests := []struct {
		name          string
		mutate        func(t *testing.T, root string)
		wantChanged   []string
		wantUnchanged []string
	}{
		{
			name: "user edit is never clobbered",
			mutate: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("user edit"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantUnchanged: []string{"a.md", "b.md"},
		},
		{
			name: "locally deleted file is redeployed",
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "a.md")); err != nil {
					t.Fatal(err)
				}
			},
			wantChanged: []string{"a.md"},
			wantUnchanged: []string{"b.md"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			plans := []TargetPlan{{
				Target:       "claude",
				ManagedPaths: []string{"a.md", "b.md"},
				Artifacts: []Artifact{
					{Path: "a.md", Content: []byte("installed a")},
					{Path: "b.md", Content: []byte("installed b")},
				},
			}}
			if _, err := Apply(root, plans); err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			tc.mutate(t, root)

			result, err := Sync(root, plans)
			if err != nil {
				t.Fatalf("Sync() error = %v", err)
			}
			if len(tc.wantChanged) > 0 && !slices.Equal(result.Changed, tc.wantChanged) {
				t.Errorf("changed = %v, want %v", result.Changed, tc.wantChanged)
			}
			if len(tc.wantUnchanged) > 0 && !slices.Equal(result.Unchanged, tc.wantUnchanged) {
				t.Errorf("unchanged = %v, want %v", result.Unchanged, tc.wantUnchanged)
			}
			if tc.name == "user edit is never clobbered" {
				got, err := os.ReadFile(filepath.Join(root, "a.md"))
				if err != nil || string(got) != "user edit" {
					t.Errorf("edited file = %q, error = %v; want user edit preserved", got, err)
				}
				manifest, err := LoadOwnershipManifest(root)
				if err != nil {
					t.Fatal(err)
				}
				digest := sha256.Sum256([]byte("installed a"))
				if entry := manifest.Entries["a.md"]; entry.SHA256 != hex.EncodeToString(digest[:]) {
					t.Errorf("manifest hash = %q, want last installed content hash preserved", entry.SHA256)
				}
			}
		})
	}
}
