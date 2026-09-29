// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package verify_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/verify"
)

// Collect's managedMCPChecks branch for a missing OpenCode config file is
// deterministic (no network, no external process): a manifest that names
// OpenCode as a target but has no opencode.json on disk makes
// installedServer's os.ReadFile fail, and every managed MCP check reports
// NotVerifiable for that reason alone.
func TestCollectManagedMCPMissingConfigFile(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, `{"version": 1, "entries": {
		"a.txt": {"path": "a.txt", "sha256": "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855", "mode": 420, "targets": ["opencode"]}
	}}`)

	report := verify.Collect(context.Background(), root)

	found := false
	for _, check := range report.Checks {
		if !strings.HasPrefix(check.ID, "mcp:opencode:") {
			continue
		}
		found = true
		if check.State != verify.NotVerifiable {
			t.Errorf("check %q State = %q, want %q (config file is absent)", check.ID, check.State, verify.NotVerifiable)
		}
		if !strings.Contains(check.Explanation, "Cannot read installed MCP configuration") {
			t.Errorf("check %q Explanation = %q, want it to explain the unreadable config", check.ID, check.Explanation)
		}
	}
	if !found {
		t.Fatal("Collect() produced no mcp:opencode:* checks for a manifest naming OpenCode as a target")
	}
	if report.Ready {
		t.Error("Collect() Ready = true, want false: managed MCP checks could not be verified")
	}
}
