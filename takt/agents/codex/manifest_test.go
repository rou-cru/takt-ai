package codex

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/shared/testutil"
)

func TestNewManagedPaths(t *testing.T) {
	testutil.RunManagedPathsTests(t, NewManagedPaths, testutil.ManagedPathsTestConfig{
		Label:         "Codex",
		NativePaths:   nativeManagedPaths,
		DuplicatePath: ".codex/AGENTS.md",
	})
}

func TestNewManagedPathsGeneratedPathIsIncluded(t *testing.T) {
	paths, err := NewManagedPaths([]string{".codex/agents/takt-dev.toml"})
	if err != nil {
		t.Fatalf("NewManagedPaths() error = %v", err)
	}
	want := []string{".codex/AGENTS.md", ".codex/agents/takt-dev.toml", ".codex/config.toml"}
	if !slices.Equal(paths, want) {
		t.Fatalf("NewManagedPaths() = %v, want %v", paths, want)
	}
}
