package opencode_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
)

// taktSandboxStub stands in for the deployed sandbox adapter: it records what the
// coordinator handed the wrapper and marks the command as wrapped and supervised.
const taktSandboxStub = `
globalThis.__taktWrap = []
export async function wrap(command, options) {
  globalThis.__taktWrap.push({ command, options })
  return "takt-sandbox " + command
}
export function supervise(wrapped, confirm) { return wrapped + " # supervised " + confirm }
`

// Execute the rendered artifact itself: shell capture must hold through the real
// plugin path — permission decision, sandbox wrapping, and delta import.
func TestTaktVFSPluginShellCapture(t *testing.T) {
	if testing.Short() {
		t.Skip("Node 24 plugin integration test")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("full plugin tests require Node 24 on PATH (use -short to skip)")
	}
	version, err := exec.Command(node, "--version").CombinedOutput()
	if err != nil || !strings.HasPrefix(strings.TrimSpace(string(version)), "v24.") {
		t.Fatalf("full plugin tests require Node 24; got %q (%v)", version, err)
	}
	for _, enforced := range []bool{true, false} {
		t.Run(fmt.Sprintf("enforced=%t", enforced), func(t *testing.T) {
			runShellHarness(t, node, enforced)
		})
	}
}

func runShellHarness(t *testing.T, node string, enforced bool) {
	dir := t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", `{"type":"module"}`)
	write("takt-vfs.ts", string(opencode.TaktVFSPluginArtifact("/test/takt-ai", enforced).Content))
	write("takt-sandbox.mjs", taktSandboxStub)
	write("node_modules/@opencode/plugin/package.json", `{"type":"module","exports":"./index.js"}`)
	write("node_modules/@opencode/plugin/index.js", "export const Plugin = { define: definition => definition }\n")
	harness, err := os.ReadFile("testdata/vfs-shell.mjs")
	if err != nil {
		t.Fatal(err)
	}
	write("shell.mjs", string(harness))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "shell.mjs")
	cmd.Dir = dir
	if !enforced {
		cmd.Env = append(os.Environ(), "TAKT_EXPECT_SHELL_OFF=1")
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered VFS plugin shell capture regression: %v\n%s", err, output)
	}
}
