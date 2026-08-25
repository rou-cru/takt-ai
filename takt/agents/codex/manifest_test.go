package codex

import (
	"slices"
	"strings"
	"testing"
)

func TestNewManifest(t *testing.T) {
	tests := []struct {
		name      string
		generated []string
		want      []string
		wantErr   string
	}{
		{
			name: "native paths are always managed",
			want: []string{".codex/AGENTS.md", ".codex/config.toml"},
		},
		{
			name:      "generated paths are normalized and sorted",
			generated: []string{"./.codex/agents/takt-dev.toml", ".codex/agents/takt-init.toml"},
			want:      []string{".codex/AGENTS.md", ".codex/agents/takt-dev.toml", ".codex/agents/takt-init.toml", ".codex/config.toml"},
		},
		{
			name:      "empty path is rejected",
			generated: []string{""},
			wantErr:   "path is empty",
		},
		{
			name:      "absolute path is rejected",
			generated: []string{"/tmp/user.toml"},
			wantErr:   "path must be relative",
		},
		{
			name:      "parent path is rejected",
			generated: []string{".codex/../user.toml"},
			wantErr:   "path must not contain '..'",
		},
		{
			name:      "duplicate normalized path is rejected",
			generated: []string{".codex/AGENTS.md"},
			wantErr:   "duplicate Codex managed path .codex/AGENTS.md",
		},
		{
			name:      "generated path is included",
			generated: []string{".codex/agents/takt-dev.toml"},
			want:      []string{".codex/AGENTS.md", ".codex/agents/takt-dev.toml", ".codex/config.toml"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			manifest, err := NewManifest(tc.generated)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("NewManifest() error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewManifest() error = %v", err)
			}
			if got := manifest.ManagedPaths(); !slices.Equal(got, tc.want) {
				t.Fatalf("ManagedPaths() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestManifestManagedPathsReturnsCopy(t *testing.T) {
	manifest, err := NewManifest([]string{".codex/agents/takt-dev.toml"})
	if err != nil {
		t.Fatal(err)
	}
	paths := manifest.ManagedPaths()
	paths[0] = "mutated"
	if got := manifest.ManagedPaths(); got[0] == "mutated" {
		t.Fatal("ManagedPaths() leaked internal state")
	}
}
