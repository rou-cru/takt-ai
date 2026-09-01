package claude

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/shared/testutil"
)

func TestNewManagedPaths(t *testing.T) {
	testutil.RunManagedPathsTests(t, NewManagedPaths, testutil.ManagedPathsTestConfig{
		Label:         "Claude",
		NativePaths:   nativeManagedPaths,
		DuplicatePath: ".claude/CLAUDE.md",
	})
}

func TestNewManagedPathsGeneratedPathIsIncluded(t *testing.T) {
	paths, err := NewManagedPaths([]string{".claude/agents/takt-dev.md"})
	if err != nil {
		t.Fatalf("NewManagedPaths() error = %v", err)
	}
	want := []string{".claude/CLAUDE.md", ".claude/agents/takt-dev.md", ".claude/settings.json"}
	if !slices.Equal(paths, want) {
		t.Fatalf("NewManagedPaths() = %v, want %v", paths, want)
	}
}
