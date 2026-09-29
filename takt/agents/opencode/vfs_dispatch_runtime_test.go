package opencode_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
)

// The orchestrator's dispatch_* tools must reach `takt-ai dispatch`, under
// its own name, never `takt-ai gc coordinate`. Execute the rendered artifact
// itself: only Bun.spawn is a double.
func TestTaktVFSPluginDispatchTools(t *testing.T) {
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
	dir := t.TempDir()
	write := func(name string, content []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", []byte(`{"type":"module"}`))
	write("takt-vfs.ts", opencode.TaktVFSPluginArtifact("/test/takt-ai", true).Content)
	write("node_modules/@opencode/plugin/package.json", []byte(`{"type":"module","exports":"./index.js"}`))
	write("node_modules/@opencode/plugin/index.js", []byte(`
export const Plugin = { define: definition => definition }
`))
	harness, err := os.ReadFile("testdata/vfs-dispatch.mjs")
	if err != nil {
		t.Fatal(err)
	}
	write("dispatch.mjs", harness)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "dispatch.mjs")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered VFS plugin dispatch tools regression: %v\n%s", err, output)
	}
}

// Child context exposes only explicitly permitted VFS tools while an exact
// claim binds that child instance to its delegated work unit.
func TestTaktVFSPluginContextFiltering(t *testing.T) {
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
	dir := t.TempDir()
	write := func(name string, content []byte) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", []byte(`{"type":"module"}`))
	write("takt-vfs.ts", opencode.TaktVFSPluginArtifact("/test/takt-ai", true).Content)
	write("node_modules/@opencode/plugin/package.json", []byte(`{"type":"module","exports":"./index.js"}`))
	write("node_modules/@opencode/plugin/index.js", []byte(`export const Plugin = { define: definition => definition }`))
	harness, err := os.ReadFile("testdata/vfs-context.mjs")
	if err != nil {
		t.Fatal(err)
	}
	write("context.mjs", harness)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "context.mjs")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered VFS plugin context filtering regression: %v\n%s", err, output)
	}
}
