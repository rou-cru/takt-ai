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

// The deliver_result protocol: a malformed handoff corrected on retry must
// finish indistinguishably from a clean first try; a producer that never
// delivers gets exactly one bounded nudge before a distinct failure; and tpm,
// once omitted from the producer catalog, must be enforced like any other
// producer at runtime.
func TestTaktVFSPluginDeliveryProtocol(t *testing.T) {
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
	harness, err := os.ReadFile("testdata/vfs-delivery.mjs")
	if err != nil {
		t.Fatal(err)
	}

	run := func(t *testing.T, scenario string) {
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
		write("delivery.mjs", harness)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, node, "delivery.mjs")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "TAKT_VFS_TEST_SCENARIO="+scenario)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("rendered VFS plugin delivery protocol regression: %v\n%s", err, output)
		}
	}

	// A deliver_result call that fails validation and is retried with valid
	// IDs in the same delegation must finish exactly like a clean first try:
	// same finish request shape, no side channel marking the earlier failure.
	t.Run("retry_indistinguishable_from_clean", func(t *testing.T) { run(t, "retry") })
	t.Run("async_delivery_waits_for_child", func(t *testing.T) { run(t, "async_delivery") })
	t.Run("async_missing_waits_for_child", func(t *testing.T) { run(t, "async_missing") })
	t.Run("wait_error_settles_and_cleans_up", func(t *testing.T) { run(t, "wait_error") })
	t.Run("prompt_error_does_not_wait", func(t *testing.T) { run(t, "prompt_error") })
	t.Run("delivered_or_failed_calls_do_not_wait", func(t *testing.T) { run(t, "no_unnecessary_wait") })
	t.Run("concurrent_deliveries_wait_for_their_own_child", func(t *testing.T) { run(t, "concurrent_delivery") })

	// A producer that never calls deliver_result gets exactly one bounded
	// nudge, then a failure textually distinct from a validate_results
	// transport failure.
	t.Run("bounded_fallback_distinct_error", func(t *testing.T) { run(t, "fallback") })

	// tpm was hardcoded out of the producer list before; it must now be
	// enforced like analyst/pm/architect/product-designer/spec.
	t.Run("tpm_enforced_as_producer", func(t *testing.T) { run(t, "tpm") })
}
