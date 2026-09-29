// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package verify

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestInstalledServerRejectsLegacyOpenCodeMCPShapes(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "flat server entry",
			config:  `{"mcp":{"engram":{"command":["engram","mcp"],"type":"local","disabled":false}}}`,
			wantErr: "flat MCP entries",
		},
		{
			name:    "missing type",
			config:  `{"mcp":{"servers":{"engram":{"command":["engram","mcp"],"disabled":false}}}}`,
			wantErr: "missing required type",
		},
		{
			name:    "local command string",
			config:  `{"mcp":{"servers":{"engram":{"type":"local","command":"engram mcp","disabled":false}}}}`,
			wantErr: "command must be an array",
		},
		{
			name:    "legacy enabled switch",
			config:  `{"mcp":{"servers":{"engram":{"type":"local","command":["engram","mcp"],"enabled":true}}}}`,
			wantErr: "legacy enabled",
		},
		{
			name:    "separate args",
			config:  `{"mcp":{"servers":{"engram":{"type":"local","command":["engram"],"args":["mcp"],"disabled":false}}}}`,
			wantErr: "legacy args",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeOpenCodeConfig(t, tc.config)
			_, present, err := installedServer(root, setup.TargetOpenCode, string(model.ComponentEngram))
			if err == nil {
				t.Fatalf("installedServer() error = nil, want error containing %q", tc.wantErr)
			}
			if present {
				t.Fatal("installedServer() reported a legacy shape as present")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want substring %q", err, tc.wantErr)
			}
		})
	}
}

func TestInstalledServerAcceptsCanonicalOpenCodeV2MCPShape(t *testing.T) {
	root := writeOpenCodeConfig(t, `{"mcp":{"servers":{"engram":{"type":"local","command":["engram","mcp","--tools=agent"],"disabled":false}}}}`)
	config, present, err := installedServer(root, setup.TargetOpenCode, string(model.ComponentEngram))
	if err != nil {
		t.Fatalf("installedServer() error = %v", err)
	}
	if !present {
		t.Fatal("installedServer() did not find canonical V2 server")
	}
	if config.Type != "local" || len(config.Command) == 0 {
		t.Fatalf("config = %+v, want local command array", config)
	}
}

func writeOpenCodeConfig(t *testing.T, contents string) string {
	t.Helper()
	root := t.TempDir()
	path := model.OpenCodeConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}
