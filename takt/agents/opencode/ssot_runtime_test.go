package opencode_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
)

// Only native OpenCode and the CodeGraph index transport are doubles. Every
// admission, assignment, gate adoption, read, verdict and consolidation runs
// the rendered plugin against a fresh CLI process and durable core/history.
func TestRenderedVFSUsesLiveAuthorities(t *testing.T) {
	if testing.Short() {
		t.Skip("rendered plugin and real CLI integration")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "takt-ai")
	cmd := exec.Command("go", "build", "-o", bin, "../../cli")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	write := func(path string, data []byte) {
		t.Helper()
		path = filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", []byte(`{"type":"module"}`))
	write("takt-vfs.ts", opencode.TaktVFSPluginArtifact(bin, true).Content)
	write("node_modules/@opencode/plugin/package.json", []byte(`{"type":"module","exports":"./index.js"}`))
	write("node_modules/@opencode/plugin/index.js", []byte(`export const Plugin = { define: d => d }`))
	data, err := os.ReadFile("testdata/ssot.mjs")
	if err != nil {
		t.Fatal(err)
	}
	write("ssot.mjs", data)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd = exec.CommandContext(ctx, node, "ssot.mjs", bin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+filepath.Join(dir, "home"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("SSoT rendered integration: %v\n%s", err, out)
	}
}

// Faults are injected only at the persisted-state/host boundary. The rendered
// plugin and all operation, ownership, repair and admission decisions are real.
func TestRenderedNormalFlowRepairsUseRealCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("rendered plugin and real CLI fault scenarios")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "takt-ai")
	if out, err := exec.Command("go", "build", "-o", bin, "../../cli").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	write := func(path string, data []byte) {
		t.Helper()
		path = filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("package.json", []byte(`{"type":"module"}`))
	write("takt-vfs.ts", opencode.TaktVFSPluginArtifact(bin, true).Content)
	write("node_modules/@opencode/plugin/package.json", []byte(`{"type":"module","exports":"./index.js"}`))
	write("node_modules/@opencode/plugin/index.js", []byte(`export const Plugin={define:d=>d}`))
	write("node_modules/@opencode/client/package.json", []byte(`{"type":"module","exports":{".":"./index.js","./service":"./service.js"}}`))
	write("node_modules/@opencode/client/index.js", []byte(`export const OpenCode={make:()=>({permission:{create:r=>globalThis.createPermission(r)}})}`))
	write("node_modules/@opencode/client/service.js", []byte(`export const Service={discover:async()=>({url:"http://fixture"}),headers:()=>({})}`))
	data, err := os.ReadFile("testdata/flow-recovery.mjs")
	if err != nil {
		t.Fatal(err)
	}
	write("flow-recovery.mjs", data)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "flow-recovery.mjs", bin)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "HOME="+filepath.Join(dir, "home"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("normal flow recovery integration: %v\n%s", err, out)
	}
}
