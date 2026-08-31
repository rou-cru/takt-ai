package opencode

import (
	"slices"
	"strings"
	"testing"
)

func TestNewManagedPaths(t *testing.T) {
	tests := []struct {
		name      string
		generated []string
		want      []string
		wantErr   string
	}{
		{
			name: "native paths are always managed",
			want: []string{".config/opencode/opencode.json"},
		},
		{
			name:      "generated paths are normalized and sorted",
			generated: []string{"./.config/opencode/agents/takt-dev.md", ".config/opencode/agents/takt-init.md"},
			want:      []string{".config/opencode/agents/takt-dev.md", ".config/opencode/agents/takt-init.md", ".config/opencode/opencode.json"},
		},
		{
			name:      "empty path is rejected",
			generated: []string{""},
			wantErr:   "path is empty",
		},
		{
			name:      "absolute path is rejected",
			generated: []string{"/tmp/user.md"},
			wantErr:   "path must be relative",
		},
		{
			name:      "parent path is rejected",
			generated: []string{".config/../user.md"},
			wantErr:   "path must not contain '..'",
		},
		{
			name:      "duplicate normalized path is rejected",
			generated: []string{".config/opencode/opencode.json"},
			wantErr:   "duplicate OpenCode managed path .config/opencode/opencode.json",
		},
		{
			name:      "generated path is included",
			generated: []string{".config/opencode/agents/takt-dev.md"},
			want:      []string{".config/opencode/agents/takt-dev.md", ".config/opencode/opencode.json"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paths, err := NewManagedPaths(tc.generated)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("NewManagedPaths() error = %v, want substring %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("NewManagedPaths() error = %v", err)
			}
			if !slices.Equal(paths, tc.want) {
				t.Fatalf("NewManagedPaths() = %v, want %v", paths, tc.want)
			}
		})
	}
}
