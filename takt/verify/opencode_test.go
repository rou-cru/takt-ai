// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package verify_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// Drive the installed-inventory entry point through an isolated CLI environment.
func collectInventory(t *testing.T, responses map[string]string) []verify.CheckResult {
	t.Helper()
	root, bin := t.TempDir(), t.TempDir()
	writeManifest(t, root, `{"version":1,"entries":{"a.txt":{"path":"a.txt","sha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855","mode":420,"targets":["opencode"]}}}`)
	script := "#!/bin/sh\ncase \"$*\" in\n"
	for path, body := range responses {
		script += "'api GET " + path + "') cat <<'RESPONSE'\n" + body + "\nRESPONSE\n;;\n"
	}
	script += "*) exit 1 ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return verify.Collect(context.Background(), root).Checks
}

// envelope wraps items in the OpenCode list envelope.
func envelope(items ...string) string {
	return `{"location":{},"data":[` + strings.Join(items, ",") + `]}`
}

func pluginEntry(id, status string) string {
	return fmt.Sprintf(`{"id":%q,"source":{"type":"local"},"features":{"server":true},"state":{"status":%q}}`, id, status)
}

func agentEntry(id, mode string) string {
	return fmt.Sprintf(`{"id":%q,"mode":%q,"hidden":false}`, id, mode)
}

// catalogSubagents returns the native subagent IDs the catalog expects.
func catalogSubagents(t *testing.T) []string {
	t.Helper()
	content, err := catalog.LoadNativeContent()
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(content))
	for id := range content {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func checkByID(t *testing.T, checks []verify.CheckResult, id string) verify.CheckResult {
	t.Helper()
	for _, check := range checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("no check %q in %+v", id, checks)
	return verify.CheckResult{}
}

// healthyResponses serves a full roster and every Takt plugin as active.
func healthyResponses(t *testing.T) map[string]string {
	t.Helper()
	agents := []string{agentEntry("takt", "primary")}
	for _, id := range catalogSubagents(t) {
		agents = append(agents, agentEntry(id, "subagent"))
	}
	return map[string]string{
		"/api/agent": envelope(agents...),
		// Server API IDs, observed independently of the expected inventory.
		"/api/plugin": envelope(pluginEntry("takt.memory", "active"), pluginEntry("takt.vfs", "active")),
	}
}

func TestNativePluginsCheckRequiresTaktPlugins(t *testing.T) {
	for _, tc := range []struct {
		name    string
		plugins []string
		want    verify.State
		detail  string
	}{
		{name: "all present", want: verify.Verified},
		{name: "memory absent", plugins: []string{pluginEntry("takt.vfs", "active")}, want: verify.NotVerified, detail: "takt.memory"},
		{name: "vfs absent", plugins: []string{pluginEntry("takt.memory", "active")}, want: verify.NotVerified, detail: "takt.vfs"},
		{name: "empty inventory", plugins: []string{}, want: verify.NotVerified, detail: "takt.memory, takt.vfs"},
		{name: "file stems are not IDs", plugins: []string{pluginEntry("takt-memory", "active"), pluginEntry("takt-vfs", "active")}, want: verify.NotVerified},
		{name: "failed plugin", plugins: []string{pluginEntry("takt.vfs", "failed")}, want: verify.NotVerified, detail: "OpenCode reports a failed plugin: takt.vfs"},
		{name: "unrelated failure", plugins: []string{pluginEntry("takt.memory", "active"), pluginEntry("takt.vfs", "active"), pluginEntry("other", "failed")}, want: verify.NotVerified, detail: "other"},
		{name: "invalid status", plugins: []string{pluginEntry("takt.vfs", "unknown")}, want: verify.NotVerifiable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			responses := healthyResponses(t)
			if tc.plugins != nil {
				responses["/api/plugin"] = envelope(tc.plugins...)
			}
			check := checkByID(t, collectInventory(t, responses), "plugins:opencode")
			if check.State != tc.want {
				t.Fatalf("State = %q, want %q (%s)", check.State, tc.want, check.Explanation)
			}
			if tc.detail != "" && !strings.Contains(check.Explanation, tc.detail) {
				t.Fatalf("Explanation = %q, want it to contain %q", check.Explanation, tc.detail)
			}
		})
	}
}

func TestNativeAgentsCheckReportsMissingSubagents(t *testing.T) {
	missing := catalogSubagents(t)[0]
	responses := healthyResponses(t)
	var agents []string
	for _, id := range catalogSubagents(t)[1:] {
		agents = append(agents, agentEntry(id, "subagent"))
	}
	responses["/api/agent"] = envelope(append(agents, agentEntry("takt", "primary"))...)

	check := checkByID(t, collectInventory(t, responses), "agents:opencode")
	if check.State != verify.NotVerified {
		t.Fatalf("State = %q, want %q (%s)", check.State, verify.NotVerified, check.Explanation)
	}
	if !strings.Contains(check.Explanation, missing) {
		t.Fatalf("Explanation = %q, want it to name the missing agent %q", check.Explanation, missing)
	}
}

func TestNativeAgentsCheckVerifiesFullRoster(t *testing.T) {
	check := checkByID(t, collectInventory(t, healthyResponses(t)), "agents:opencode")
	if check.State != verify.Verified {
		t.Fatalf("State = %q, want %q (%s)", check.State, verify.Verified, check.Explanation)
	}
}

func TestNativeInventoryUnavailableIsNotVerifiable(t *testing.T) {
	checks := collectInventory(t, nil)
	for _, id := range []string{"agents:opencode", "plugins:opencode"} {
		if check := checkByID(t, checks, id); check.State != verify.NotVerifiable {
			t.Fatalf("%s: %+v", id, check)
		}
	}
}

func TestNativeAgentsMustBeDelegatable(t *testing.T) {
	responses := healthyResponses(t)
	id := catalogSubagents(t)[0]
	responses["/api/agent"] = strings.Replace(responses["/api/agent"], agentEntry(id, "subagent"), agentEntry(id, "primary"), 1)
	check := checkByID(t, collectInventory(t, responses), "agents:opencode")
	if check.State != verify.NotVerified || !strings.Contains(check.Explanation, id) {
		t.Fatalf("non-delegatable agent accepted: %+v", check)
	}
}

func TestServerInventoryMatchesDeployedPluginIDs(t *testing.T) {
	artifacts := []opencode.Artifact{opencode.TaktMemoryPluginArtifact("/bin/takt-ai"), opencode.TaktVFSPluginArtifact("/bin/takt-ai", true)}
	ids := opencode.TaktPluginIDs()
	if len(ids) != len(artifacts) {
		t.Fatalf("server inventory: %v", ids)
	}
	for i, artifact := range artifacts {
		if !strings.Contains(string(artifact.Content), `id: "`+ids[i]+`"`) {
			t.Fatalf("%s does not declare %s", artifact.Path, ids[i])
		}
	}
}
