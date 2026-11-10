package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/engram"
)

// FakeEngramScript is the single fake `engram version` stub every test uses,
// so version-output drift fails in one place instead of four.
func FakeEngramScript(version string) []byte {
	return []byte("#!/bin/sh\necho 'engram " + version + "'\n")
}

// RunWithFakeEngram runs m with a compatible fake engram first on PATH, so
// tests never use the host binary or download one. Call it from TestMain.
func RunWithFakeEngram(m *testing.M) int {
	dir, err := os.MkdirTemp("", "takt-tui-engram-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}()
	script := FakeEngramScript(engram.EngramVersion)
	if err := os.WriteFile(filepath.Join(dir, "engram"), []byte(script), executableFileMode); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode"), FakeOpenCodeScript(), executableFileMode); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// A compatible codegraph on PATH is reused as-is, which keeps install/sync
	// off npm and the network without needing a Go-level seam here.
	if err := os.WriteFile(filepath.Join(dir, "codegraph"), FakeCodegraphScript(codegraph.CodegraphVersion), executableFileMode); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}
