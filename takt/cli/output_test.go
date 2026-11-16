// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main_test

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

// Exercise the public command, including the real catalog-derived summary,
// without installing tools or opening the memory store.
func TestSetupPlanStatesTheInstalledCapabilities(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the CLI in an isolated HOME")
	}
	bin := buildTaktAI(t)
	request, body := planRequest(t)
	for _, command := range []string{"install", "sync"} {
		t.Run(command, func(t *testing.T) {
			root := t.TempDir()
			preview, err := lifecycle.PreviewLifecycle(command, root, request)
			if err != nil {
				t.Fatal(err)
			}
			plan := preview.(lifecycle.InstallPreview)
			summary, err := runtime.Summarize(root, request, plan)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(summary.MCPServers, memory.ServerKey) {
				t.Fatalf("installation summary omits the memory MCP server: %v", summary.MCPServers)
			}
			managed := 0
			for _, target := range plan.Plans {
				managed += len(target.ManagedPaths)
			}
			out, err := runSetupOutput(t, bin, root, command, body, "--plan-only")
			if err != nil {
				t.Fatalf("setup %s --plan-only: %v\n%s", command, err, out)
			}
			for _, want := range []string{
				"Plan for " + command + " (nothing changes without --yes)",
				fmt.Sprintf("Agents        %d", len(summary.Agents)),
				fmt.Sprintf("Skills        %d", summary.Skills),
				"MCP servers   " + strings.Join(summary.MCPServers, ", "),
				fmt.Sprintf("Files         %d managed", managed),
			} {
				if !strings.Contains(out, want) {
					t.Errorf("plan output lacks %q:\n%s", want, out)
				}
			}
			for _, path := range []string{".takt-manifest.json", ".takt-installed-config.json", "memory", ".config"} {
				if _, err := os.Stat(filepath.Join(root, path)); !os.IsNotExist(err) {
					t.Errorf("plan-only changed %s (stat error: %v)", path, err)
				}
			}
		})
	}
}
