package opencode

import (
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/shared/testutil"
)

func TestNewManagedPaths(t *testing.T) {
	testutil.RunManagedPathsTests(t, NewManagedPaths, testutil.ManagedPathsTestConfig{
		Label:         "OpenCode",
		NativePaths:   nativeManagedPaths,
		DuplicatePath: ".config/opencode/opencode.json",
	})
}

func TestNewManagedPathsGeneratedPathIsIncluded(t *testing.T) {
	paths, err := NewManagedPaths([]string{".config/opencode/agents/takt-dev.md"})
	if err != nil {
		t.Fatalf("NewManagedPaths() error = %v", err)
	}
	want := []string{".config/opencode/agents/takt-dev.md", ".config/opencode/opencode.json"}
	if !slices.Equal(paths, want) {
		t.Fatalf("NewManagedPaths() = %v, want %v", paths, want)
	}
}
