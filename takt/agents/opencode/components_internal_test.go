// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencode

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaktSandboxAdapterArtifact(t *testing.T) {
	artifact := TaktSandboxAdapterArtifact()
	if artifact.Path != ".config/opencode/plugins/takt-sandbox.mjs" {
		t.Fatalf("artifact path = %q", artifact.Path)
	}
	if !strings.Contains(string(artifact.Content), "sandbox-runtime") {
		t.Fatal("sandbox adapter artifact does not reference sandbox-runtime")
	}
}

func TestInstallSandboxDependency(t *testing.T) {
	withCommand := func(t *testing.T, command func(context.Context, string, ...string) *exec.Cmd, run func(string) error) {
		t.Helper()
		previous := installSandboxDependencyCommand
		installSandboxDependencyCommand = command
		t.Cleanup(func() { installSandboxDependencyCommand = previous })
		if err := run(t.TempDir()); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("already installed skips npm", func(t *testing.T) {
		root := t.TempDir()
		packagePath := filepath.Join(sandboxPluginsDir(root), "node_modules", "@anthropic-ai", "sandbox-runtime", "package.json")
		if err := os.MkdirAll(filepath.Dir(packagePath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(packagePath, []byte(`{"version":"0.0.76"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		called := false
		previous := installSandboxDependencyCommand
		installSandboxDependencyCommand = func(context.Context, string, ...string) *exec.Cmd {
			called = true
			return exec.Command("sh", "-c", "exit 1")
		}
		t.Cleanup(func() { installSandboxDependencyCommand = previous })
		if err := InstallSandboxDependency(context.Background(), root); err != nil {
			t.Fatal(err)
		}
		if called {
			t.Fatal("npm ran despite the pinned package being installed")
		}
	})

	t.Run("npm failure preserves command output", func(t *testing.T) {
		withCommand(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", "echo npm-unavailable >&2; exit 7")
		}, func(root string) error {
			err := InstallSandboxDependency(context.Background(), root)
			if err == nil || !strings.Contains(err.Error(), "npm-unavailable") {
				return fmt.Errorf("InstallSandboxDependency() error = %v, want npm output", err)
			}
			return nil
		})
	})

	t.Run("successful npm must install the pinned package", func(t *testing.T) {
		withCommand(t, func(ctx context.Context, name string, args ...string) *exec.Cmd {
			if name != "npm" || len(args) < 3 || args[0] != "install" || args[1] != "--prefix" {
				return exec.CommandContext(ctx, "sh", "-c", "echo unexpected npm arguments >&2; exit 9")
			}
			if args[len(args)-1] != "@anthropic-ai/sandbox-runtime@"+SandboxRuntimeVersion {
				return exec.CommandContext(ctx, "sh", "-c", "echo unexpected sandbox-runtime version >&2; exit 9")
			}
			packagePath := filepath.Join(args[2], "node_modules", "@anthropic-ai", "sandbox-runtime", "package.json")
			if err := os.MkdirAll(filepath.Dir(packagePath), 0o755); err != nil {
				return exec.CommandContext(ctx, "sh", "-c", "echo could not create fake npm package >&2; exit 9")
			}
			if err := os.WriteFile(packagePath, []byte(`{"version":"`+SandboxRuntimeVersion+`"}`), 0o600); err != nil {
				return exec.CommandContext(ctx, "sh", "-c", "echo could not write fake npm package >&2; exit 9")
			}
			return exec.CommandContext(ctx, "sh", "-c", "exit 0")
		}, func(root string) error {
			if err := InstallSandboxDependency(context.Background(), root); err != nil {
				return err
			}
			if !sandboxDependencyInstalled(root) {
				return fmt.Errorf("sandbox dependency is still absent after successful install")
			}
			return nil
		})
	})

	t.Run("successful npm without package is rejected", func(t *testing.T) {
		withCommand(t, func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
			return exec.CommandContext(ctx, "sh", "-c", "exit 0")
		}, func(root string) error {
			err := InstallSandboxDependency(context.Background(), root)
			if err == nil || !strings.Contains(err.Error(), "not usable after npm install") {
				return fmt.Errorf("InstallSandboxDependency() error = %v, want missing-package error", err)
			}
			return nil
		})
	})

	t.Run("unusable plugin directory reports setup error", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(root, []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		previous := installSandboxDependencyCommand
		installSandboxDependencyCommand = func(context.Context, string, ...string) *exec.Cmd {
			t.Error("npm ran although plugin directory creation failed")
			return exec.Command("sh", "-c", "exit 1")
		}
		t.Cleanup(func() { installSandboxDependencyCommand = previous })
		err := InstallSandboxDependency(context.Background(), root)
		if err == nil || !strings.Contains(err.Error(), "prepare sandbox plugin dir") {
			t.Fatalf("InstallSandboxDependency() error = %v, want directory setup error", err)
		}
	})
}
