package opencode_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
)

// dispatch_switch's identity protocol and rootSession()'s switch-created
// fallback are both regressions the fix must never reintroduce. Execute the
// rendered artifact itself: only Bun.spawn and the plugin ctx are doubles.
func TestTaktVFSPluginSwitchIdentity(t *testing.T) {
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
	harness, err := os.ReadFile("testdata/vfs-switch.mjs")
	if err != nil {
		t.Fatal(err)
	}
	write("switch.mjs", harness)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "switch.mjs")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rendered VFS plugin switch identity regression: %v\n%s", err, output)
	}
}

// GC's collector and verifier lanes must both keep memory tool access
// alongside their gc_* tools; a static check on the rendered source needs no
// Node process.
func TestTaktVFSPluginGCLaneToolsGrantMemoryAccess(t *testing.T) {
	source := string(opencode.TaktVFSPluginArtifact("/test/takt-ai", true).Content)
	block := regexp.MustCompile(`(?s)const GC_LANE_TOOLS: Record<string, string\[\]> = \{(.*?)\n\}`).FindStringSubmatch(source)
	if block == nil {
		t.Fatal("GC_LANE_TOOLS object not found in rendered plugin")
	}
	for _, role := range []string{"collector", "verifier"} {
		line := regexp.MustCompile(role + `:\s*\[([^\]]*)\]`).FindString(block[1])
		if line == "" {
			t.Fatalf("GC_LANE_TOOLS.%s not found", role)
		}
		for _, tool := range []string{"memory_record", "memory_continue_session", "memory_close_session"} {
			if !strings.Contains(line, `"`+tool+`"`) {
				t.Errorf("GC_LANE_TOOLS.%s missing %q: %s", role, tool, line)
			}
		}
	}
}
