// Package testutil provides shared test helpers for agent manifest tests.
package testutil

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// ManagedPathsTestConfig holds the parameters for testing NewManagedPaths
// through a target adapter. Each agent package calls RunManagedPathsTests
// with its own native paths and expected error patterns.
type ManagedPathsTestConfig struct {
	// Label is the target adapter name (e.g. "Codex", "Claude", "OpenCode").
	Label string
	// NativePaths are the adapter's hardcoded managed paths.
	NativePaths []string
	// DuplicatePath is a native path to use for the duplicate rejection test.
	DuplicatePath string
}

// RunManagedPathsTests runs the shared validation tests for NewManagedPaths
// using the given function under test. Each agent package should call this
// to validate the common contract without duplicating test logic.
func RunManagedPathsTests(t *testing.T, fn func([]string) ([]string, error), config ManagedPathsTestConfig) {
	t.Helper()

	t.Run("native paths are always managed", func(t *testing.T) {
		paths, err := fn(nil)
		if err != nil {
			t.Fatalf("NewManagedPaths() error = %v", err)
		}
		if !slices.Equal(paths, config.NativePaths) {
			t.Fatalf("NewManagedPaths() = %v, want %v", paths, config.NativePaths)
		}
	})

	t.Run("empty path is rejected", func(t *testing.T) {
		_, err := fn([]string{""})
		if err == nil || !strings.Contains(err.Error(), "path is empty") {
			t.Fatalf("NewManagedPaths() error = %v, want substring %q", err, "path is empty")
		}
	})

	t.Run("absolute path is rejected", func(t *testing.T) {
		_, err := fn([]string{"/tmp/user"})
		if err == nil || !strings.Contains(err.Error(), "path must be relative") {
			t.Fatalf("NewManagedPaths() error = %v, want substring %q", err, "path must be relative")
		}
	})

	t.Run("parent path is rejected", func(t *testing.T) {
		_, err := fn([]string{config.NativePaths[0][:strings.LastIndex(config.NativePaths[0], "/")] + "/../user"})
		if err == nil || !strings.Contains(err.Error(), "path must not contain '..'") {
			t.Fatalf("NewManagedPaths() error = %v, want substring %q", err, "path must not contain '..'")
		}
	})

	t.Run("duplicate normalized path is rejected", func(t *testing.T) {
		wantErr := fmt.Sprintf("duplicate %s managed path %s", config.Label, config.DuplicatePath)
		_, err := fn([]string{config.DuplicatePath})
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Fatalf("NewManagedPaths() error = %v, want substring %q", err, wantErr)
		}
	})
}
