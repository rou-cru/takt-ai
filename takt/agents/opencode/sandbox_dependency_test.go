// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencode_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
)

// fakeNpmInstall stands in for `npm install --prefix <dir> … <spec>`: it logs its
// arguments to $NPM_LOG and installs the package at the version spec names.
const fakeNpmInstall = `printf '%s\n' "$*" > "$NPM_LOG"
for spec; do :; done
dir="$3/node_modules/@anthropic-ai/sandbox-runtime"
mkdir -p "$dir" && printf '{"version":"%s"}\n' "${spec##*@}" > "$dir/package.json"
`

// fakeNpm puts an npm running script first on PATH and returns the file it logs to.
func fakeNpm(t *testing.T, script string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "npm"), []byte("#!/bin/sh\n"+script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	log := filepath.Join(dir, "npm.log")
	t.Setenv("NPM_LOG", log)
	return log
}

func pluginsDir(root string) string {
	return filepath.Join(root, filepath.FromSlash(opencode.ConfigDir()), "plugins")
}

func manifestPath(root string) string {
	return filepath.Join(pluginsDir(root), "node_modules", "@anthropic-ai", "sandbox-runtime", "package.json")
}

func installVersion(t *testing.T, root, version string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(manifestPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath(root), []byte(`{"version":"`+version+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func installedVersion(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(manifestPath(root))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		t.Fatal(err)
	}
	return pkg.Version
}

func TestTaktSandboxAdapterArtifact(t *testing.T) {
	artifact := opencode.TaktSandboxAdapterArtifact()
	if artifact.Path != ".config/opencode/plugins/takt-sandbox.mjs" {
		t.Fatalf("artifact path = %q", artifact.Path)
	}
	if !strings.Contains(string(artifact.Content), "sandbox-runtime") {
		t.Fatal("sandbox adapter artifact does not reference sandbox-runtime")
	}
}

func TestInstallSandboxDependency(t *testing.T) {
	t.Run("npm installs the pinned package into the plugins directory", func(t *testing.T) {
		log := fakeNpm(t, fakeNpmInstall)
		root := t.TempDir()
		if err := opencode.InstallSandboxDependency(context.Background(), root); err != nil {
			t.Fatal(err)
		}
		args, err := os.ReadFile(log)
		if err != nil {
			t.Fatal(err)
		}
		want := "install --prefix " + pluginsDir(root)
		if !strings.HasPrefix(string(args), want) || !strings.HasSuffix(strings.TrimSpace(string(args)), "@anthropic-ai/sandbox-runtime@"+opencode.SandboxRuntimeVersion) {
			t.Fatalf("npm ran with %q; want %q … the pinned package", args, want)
		}
		if got := installedVersion(t, root); got != opencode.SandboxRuntimeVersion {
			t.Fatalf("installed version = %q; want %q", got, opencode.SandboxRuntimeVersion)
		}
	})

	t.Run("an older installed version is upgraded to the pinned one", func(t *testing.T) {
		fakeNpm(t, fakeNpmInstall)
		root := t.TempDir()
		installVersion(t, root, "0.0.76")
		if err := opencode.InstallSandboxDependency(context.Background(), root); err != nil {
			t.Fatal(err)
		}
		if got := installedVersion(t, root); got != opencode.SandboxRuntimeVersion {
			t.Fatalf("installed version = %q; want the older one replaced by %q", got, opencode.SandboxRuntimeVersion)
		}
	})

	t.Run("the pinned version installed skips npm", func(t *testing.T) {
		log := fakeNpm(t, fakeNpmInstall)
		root := t.TempDir()
		installVersion(t, root, opencode.SandboxRuntimeVersion)
		if err := opencode.InstallSandboxDependency(context.Background(), root); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("npm ran despite the pinned package being installed: %v", err)
		}
	})

	t.Run("npm failure preserves command output", func(t *testing.T) {
		fakeNpm(t, "echo npm-unavailable >&2; exit 7\n")
		err := opencode.InstallSandboxDependency(context.Background(), t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "npm-unavailable") {
			t.Fatalf("InstallSandboxDependency() error = %v, want npm output", err)
		}
	})

	t.Run("successful npm without package is rejected", func(t *testing.T) {
		fakeNpm(t, "exit 0\n")
		err := opencode.InstallSandboxDependency(context.Background(), t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "not usable after npm install") {
			t.Fatalf("InstallSandboxDependency() error = %v, want missing-package error", err)
		}
	})

	t.Run("unusable plugin directory reports setup error", func(t *testing.T) {
		log := fakeNpm(t, fakeNpmInstall)
		root := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(root, []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		err := opencode.InstallSandboxDependency(context.Background(), root)
		if err == nil || !strings.Contains(err.Error(), "prepare sandbox plugin dir") {
			t.Fatalf("InstallSandboxDependency() error = %v, want directory setup error", err)
		}
		if _, err := os.Stat(log); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("npm ran although plugin directory creation failed")
		}
	})
}
