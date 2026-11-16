// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main_test

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/setup"
)

// planRequest is the default setup request and its JSON input.
func planRequest(t *testing.T) (setup.PlanRequest, []byte) {
	t.Helper()
	request, err := setup.DefaultPlanRequest()
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	return request, body
}

func runSetupOutput(t *testing.T, bin, root, command string, body []byte, flags ...string) (string, error) {
	t.Helper()
	args := append([]string{"setup", command, "--root", root, "--input", "-"}, flags...)
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(string(body))
	cmd.Env = []string{"HOME=" + root, "PATH=" + os.Getenv("PATH"), memory.EnvDir + "=" + filepath.Join(root, "memory")}
	if dir := os.Getenv("GOCOVERDIR"); dir != "" {
		cmd.Env = append(cmd.Env, "GOCOVERDIR="+dir)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// The ownership manifest is the public input to uninstall, so its observable
// plan must distinguish deletion, local edits, and restoration without acting.
func TestSetupUninstallPlanReportsOwnershipOutcomes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the CLI in an isolated HOME")
	}
	bin := buildTaktAI(t)
	_, body := planRequest(t)
	for _, tc := range []struct {
		name, current, prior, want string
	}{
		{"managed", "managed", "", "[remove] plugin.ts"},
		{"locally edited", "edited", "", "[preserve] plugin.ts"},
		{"prior content", "managed", "before Takt", "[restore] plugin.ts"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "plugin.ts")
			if err := os.WriteFile(path, []byte(tc.current), 0o600); err != nil {
				t.Fatal(err)
			}
			var hash, backup string
			if tc.prior != "" {
				hash = fmt.Sprintf("%x", sha256.Sum256([]byte(tc.prior)))
				backup = "prior.ts"
				if err := os.WriteFile(filepath.Join(root, backup), []byte(tc.prior), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			entry, err := setup.NewOwnershipEntry("plugin.ts", []byte("managed"), 0o600, tc.prior != "", hash, backup, setup.TargetOpenCode)
			if err != nil {
				t.Fatal(err)
			}
			manifest := setup.NewOwnershipManifest()
			if err := manifest.Add(entry); err != nil {
				t.Fatal(err)
			}
			if err := manifest.Save(root); err != nil {
				t.Fatal(err)
			}
			out, err := runSetupOutput(t, bin, root, "uninstall", body, "--plan-only")
			if err != nil || !strings.Contains(out, "Plan for uninstall:") || !strings.Contains(out, tc.want) {
				t.Fatalf("uninstall plan = %q, %v; want %q", out, err, tc.want)
			}
			if tc.current == "edited" {
				out, err = runSetupOutput(t, bin, root, "uninstall", body, "--yes")
				if err == nil || !strings.Contains(out, "1 modified file(s)") || !strings.Contains(out, "[modified] plugin.ts") {
					t.Fatalf("uninstall with edits = %q, %v; want explicit decision refusal", out, err)
				}
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != tc.current {
				t.Fatalf("preview or refusal changed the file: %q, %v", got, err)
			}
		})
	}
}

func TestSetupOutputRefusesInvalidRequestAndMissingAuthorization(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the CLI in an isolated HOME")
	}
	bin := buildTaktAI(t)
	_, body := planRequest(t)
	for _, tc := range []struct {
		name string
		body []byte
		want string
	}{
		{"invalid request", []byte(`{"unknown":true}`), "invalid setup request"},
		{"no authorization", body, "refusing to change the environment without --yes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			out, err := runSetupOutput(t, bin, root, "install", tc.body)
			if err == nil || !strings.Contains(out, tc.want) {
				t.Fatalf("setup refusal = %q, %v; want %q", out, err, tc.want)
			}
			files, err := os.ReadDir(root)
			if err != nil || len(files) != 0 {
				t.Fatalf("refused install changed root: %v, %v", files, err)
			}
		})
	}
}
