// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package verify_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// memoryServerID is the check of the crew's memory MCP registration.
const memoryServerID = "mcp:opencode:" + memory.ServerKey

// readyWait bounds how long a test waits for its in-process memory service to open its socket.
const readyWait = 10 * time.Second

// ownerManifest names OpenCode as an installed target, so the managed checks run.
const ownerManifest = `{"version":1,"entries":{"a.txt":{"path":"a.txt","sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","mode":420,"targets":["opencode"]}}}`

// collectWithConfig verifies an install whose opencode.json holds config, against a fake OpenCode that
// answers each route with its body.
func collectWithConfig(t *testing.T, config string, responses map[string]string) []verify.CheckResult {
	t.Helper()
	root, bin := t.TempDir(), t.TempDir()
	writeManifest(t, root, ownerManifest)
	path := model.OpenCodeConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ncase \"$*\" in\n"
	for route, body := range responses {
		script += "'api GET " + route + "') cat <<'RESPONSE'\n" + body + "\nRESPONSE\n;;\n"
	}
	script += "*) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return verify.Collect(context.Background(), root).Checks
}

func TestCollectRejectsLegacyOpenCodeMCPShapes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "flat server entry",
			config:  `{"mcp":{"takt_memory":{"command":["takt-ai","memory","mcp","--crew"],"type":"local","disabled":false}}}`,
			wantErr: "flat MCP entries",
		},
		{
			name:    "missing type",
			config:  `{"mcp":{"servers":{"takt_memory":{"command":["takt-ai","memory","mcp","--crew"],"disabled":false}}}}`,
			wantErr: "missing required type",
		},
		{
			name:    "local command string",
			config:  `{"mcp":{"servers":{"takt_memory":{"type":"local","command":"takt-ai memory mcp --crew","disabled":false}}}}`,
			wantErr: "command must be an array",
		},
		{
			name:    "legacy enabled switch",
			config:  `{"mcp":{"servers":{"takt_memory":{"type":"local","command":["takt-ai","memory","mcp","--crew"],"enabled":true}}}}`,
			wantErr: "legacy enabled",
		},
		{
			name:    "separate args",
			config:  `{"mcp":{"servers":{"takt_memory":{"type":"local","command":["takt-ai"],"args":["memory","mcp","--crew"],"disabled":false}}}}`,
			wantErr: "legacy args",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			check := checkByID(t, collectWithConfig(t, tc.config, nil), memoryServerID)
			if check.State != verify.NotVerifiable {
				t.Fatalf("State = %q, want %q (%s)", check.State, verify.NotVerifiable, check.Explanation)
			}
			if !strings.Contains(check.Explanation, tc.wantErr) {
				t.Fatalf("Explanation = %q, want substring %q", check.Explanation, tc.wantErr)
			}
		})
	}
}

func TestCollectVerifiesTheCanonicalMemoryServerRegistration(t *testing.T) {
	config := `{"mcp":{"servers":{"takt_memory":{"type":"local","command":["/bin/takt-ai","memory","mcp","--crew"],"disabled":false}}}}`
	connected := envelope(`{"name":"takt_memory","status":{"status":"connected"}}`)
	check := checkByID(t, collectWithConfig(t, config, map[string]string{"/api/mcp": connected}), memoryServerID)
	if check.State != verify.Verified {
		t.Fatalf("State = %q, want %q (%s)", check.State, verify.Verified, check.Explanation)
	}
	failed := envelope(`{"name":"takt_memory","status":{"status":"failed","error":"spawn failed"}}`)
	check = checkByID(t, collectWithConfig(t, config, map[string]string{"/api/mcp": failed}), memoryServerID)
	if check.State != verify.NotVerified {
		t.Fatalf("a failed server was reported %q (%s)", check.State, check.Explanation)
	}
}

func TestCollectAbsentMemoryServerIsNotVerified(t *testing.T) {
	check := checkByID(t, collectWithConfig(t, `{"mcp":{"servers":{}}}`, nil), memoryServerID)
	if check.State != verify.NotVerified {
		t.Fatalf("State = %q, want %q (%s)", check.State, verify.NotVerified, check.Explanation)
	}
}

// serveMemory runs the memory service of a fresh short directory in this process until the test ends,
// and points TAKT_MEMORY_DIR at it.
func serveMemory(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tkt")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(memory.EnvDir, dir)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func startService(t *testing.T, dir, version string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- memory.Serve(ctx, dir, version) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	deadline := time.Now().Add(readyWait)
	for {
		if _, err := os.Stat(filepath.Join(dir, memory.SocketFile)); err == nil {
			return
		}
		select {
		case err := <-done:
			t.Fatalf("Serve returned early: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatal("the memory socket never appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestCollectReportsTheMemoryServiceStatus(t *testing.T) {
	const config = `{"mcp":{"servers":{}}}`
	const checkID = "memory:service"

	t.Run("unreachable", func(t *testing.T) {
		serveMemory(t)
		check := checkByID(t, collectWithConfig(t, config, nil), checkID)
		if check.State != verify.NotVerified {
			t.Fatalf("State = %q, want %q (%s)", check.State, verify.NotVerified, check.Explanation)
		}
	})
	t.Run("not ready without a store", func(t *testing.T) {
		dir := serveMemory(t)
		startService(t, dir, "test-version")
		check := checkByID(t, collectWithConfig(t, config, nil), checkID)
		if check.State != verify.NotVerified || !strings.Contains(check.Explanation, memory.StateNoStore) {
			t.Fatalf("check = %+v, want not verified, naming %s", check, memory.StateNoStore)
		}
	})
}
