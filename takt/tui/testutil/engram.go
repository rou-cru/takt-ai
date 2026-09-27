package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/engram"
)

// executableFileMode is the mode required by fake command fixtures on PATH.
const executableFileMode os.FileMode = 0o755

// FakeEngramScript is the single fake `engram version` stub every test uses,
// so version-output drift fails in one place instead of four.
func FakeEngramScript(version string) []byte {
	return []byte("#!/bin/sh\necho 'engram " + version + "'\n")
}

// FakeOpenCodeScript provides the V2 read/reload surface used by lifecycle
// verification tests without invoking a developer's real OpenCode process.
func FakeOpenCodeScript() []byte {
	return []byte(`#!/bin/sh
case "$*" in
  'api GET /api/info') printf '%s' '{"version":"2.0.16"}' ;;
  'api GET /api/model'|'api GET /api/model/default'|'api GET /api/agent'|'api GET /api/skill'|'api GET /api/mcp'|'api GET /api/plugin') printf '%s' '{"location":{},"data":[]}' ;;
  'service restart'|'api POST /api/location/reload') exit 0 ;;
  *) echo "unexpected OpenCode invocation: $*" >&2; exit 1 ;;
esac
`)
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
	if err := os.WriteFile(filepath.Join(dir, "codegraph"), []byte("#!/bin/sh\necho 1.6.0\n"), executableFileMode); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := os.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return m.Run()
}
