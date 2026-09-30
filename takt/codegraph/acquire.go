// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

// Package codegraph keeps the codebase-exploration MCP wiring in one place so every agent target shares the same setup and checks.
package codegraph

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
)

// CodegraphVersion is the minimum codegraph release Takt configures and the one it installs.
const CodegraphVersion = "1.6.1"

const (
	// codegraphDirectoryMode keeps the installed tool directory traversable by its owner.
	codegraphDirectoryMode os.FileMode = 0o755
)

// npmPackage is the npm distribution; its per-platform optionalDependency carries the native bundle.
const npmPackage = "@colbymchenry/codegraph"

// managedMarkerKey is a regular file inside the managed install; the ownership manifest tracks it
// because it holds files, not directories, and the rest of the tree is removed with it.
const managedMarkerKey = ".takt-ai/codegraph/node_modules/@colbymchenry/codegraph/npm-shim.js"

var (
	// lookPath finds binaries so tests can swap in a fake path.
	lookPath = exec.LookPath
	// execCommand runs binaries so tests can stub command execution.
	execCommand = exec.CommandContext
	// userHomeDir resolves the home directory; tests isolate the home guard.
	userHomeDir = os.UserHomeDir
)

// runCommand executes one CodeGraph CLI command from the target workspace.
// Tests replace it to verify startup ordering without launching CodeGraph.
var runCommand = func(ctx context.Context, binary, workspace string, args ...string) ([]byte, error) {
	cmd := execCommand(ctx, binary, args...)
	cmd.Dir = workspace
	return cmd.CombinedOutput()
}

// ManagedMarkerKey returns the ownership-manifest key for the managed install, so tests assert the same slash-path lifecycle records.
func ManagedMarkerKey() string { return managedMarkerKey }

// ManagedPrefix is where Takt installs its own copy: <root>/.takt-ai/codegraph
func ManagedPrefix(root string) string {
	return filepath.Join(root, ".takt-ai", "codegraph")
}

// ManagedBinaryPath is the launcher npm links inside the managed prefix.
func ManagedBinaryPath(root string) string {
	return filepath.Join(ManagedPrefix(root), "node_modules", ".bin", "codegraph")
}

// Resolve is read-only. Order: (1) `codegraph` on PATH whose `--version` >= CodegraphVersion,
// (2) ManagedBinaryPath(root) if it exists and reports >= CodegraphVersion. Returns absolute path.
func Resolve(root string) (string, bool) {
	candidates := []string{}
	if onPath, err := lookPath("codegraph"); err == nil {
		candidates = append(candidates, onPath)
	}
	candidates = append(candidates, ManagedBinaryPath(root))
	return shared.ResolveBinary(candidates, func(absolute string) bool {
		version, err := VerifyVersion(absolute)
		return err == nil && compatibleVersion(version)
	})
}

// Acquire returns Resolve's path when found; otherwise installs the pinned npm package into
// ManagedPrefix(root) so the MCP entry can point at an absolute path instead of relying on PATH.
func Acquire(ctx context.Context, root string) (string, error) {
	if path, found := Resolve(root); found {
		return path, nil
	}
	prefix := ManagedPrefix(root)
	if err := os.MkdirAll(prefix, codegraphDirectoryMode); err != nil {
		return "", fmt.Errorf("prepare codegraph install dir: %w", err)
	}
	output, err := execCommand(ctx, "npm", "install", "--prefix", prefix, "--no-fund", "--no-audit", "--no-package-lock", "--loglevel=error", npmPackage+"@"+CodegraphVersion).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("install codegraph %s with npm: %w: %s", CodegraphVersion, err, strings.TrimSpace(string(output)))
	}
	path, found := Resolve(root)
	if !found {
		return "", fmt.Errorf("codegraph %s not usable after npm install at %s", CodegraphVersion, ManagedBinaryPath(root))
	}
	return path, nil
}

// EnsureIndexed initializes CodeGraph once for a workspace before its MCP
// server is started. Existing indexes are validated and left to CodeGraph's
// serve-time reconciliation; a broken existing index is reported, not reset.
func EnsureIndexed(ctx context.Context, binary, workspace string) error {
	if strings.TrimSpace(binary) == "" {
		return fmt.Errorf("codegraph binary is required")
	}
	if strings.TrimSpace(workspace) == "" {
		return fmt.Errorf("workspace directory is required")
	}
	home, err := userHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	same, err := sameDirectory(workspace, home)
	if err != nil {
		return fmt.Errorf("compare CodeGraph workspace with home directory: %w", err)
	}
	if same {
		return fmt.Errorf("refusing to initialize CodeGraph in home directory %s; open a specific project directory", home)
	}
	output, err := runCommand(ctx, binary, workspace, "status", workspace)
	if err != nil {
		return fmt.Errorf("check CodeGraph index for %s: %w: %s", workspace, err, strings.TrimSpace(string(output)))
	}
	needsInit := strings.Contains(strings.ToLower(string(output)), "not initialized")
	if !needsInit {
		return nil
	}
	output, err = runCommand(ctx, binary, workspace, "init", "--yes", workspace)
	if err != nil {
		return fmt.Errorf("initialize CodeGraph index for %s: %w: %s", workspace, err, strings.TrimSpace(string(output)))
	}
	if output, err := runCommand(ctx, binary, workspace, "status", workspace); err != nil {
		return fmt.Errorf("verify CodeGraph index for %s: %w: %s", workspace, err, strings.TrimSpace(string(output)))
	} else if strings.Contains(strings.ToLower(string(output)), "not initialized") {
		return fmt.Errorf("verify CodeGraph index for %s: CodeGraph still reports the workspace as not initialized", workspace)
	}
	return nil
}

func sameDirectory(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		return false, err
	}
	return os.SameFile(leftInfo, rightInfo), nil
}

// VerifyVersion runs `<binary> --version` so broken installs fail fast with a clear error.
func VerifyVersion(binary string) (string, error) {
	out, err := execCommand(context.Background(), binary, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("codegraph version command failed: %w", err)
	}
	version := strings.TrimSpace(string(out))
	if version == "" {
		return "", fmt.Errorf("codegraph version returned empty output")
	}
	return version, nil
}

// compatibleVersion reads the bare `X.Y.Z` codegraph prints; anything unparseable is incompatible.
func compatibleVersion(output string) bool {
	return shared.VersionAtLeast(strings.TrimSpace(output), CodegraphVersion)
}
