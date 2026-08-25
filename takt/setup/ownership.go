// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
	"github.com/rou-cru/takt-ai/takt/model"
)

// OwnershipManifestVersion is the schema version written by this build.
// LoadOwnershipManifest rejects files carrying any other version so future
// formats are never misread by old binaries.
const OwnershipManifestVersion = 1

// OwnershipManifestFilename is the manifest file name at the deployment root.
// It records every file Takt manages across all targets, with content hashes,
// prior-state capture, permissions, backup metadata, and owning targets.
const OwnershipManifestFilename = ".takt-manifest.json"

// OwnershipTarget names a deployment target that owns managed files.
type OwnershipTarget string

const (
	TargetClaude   OwnershipTarget = "claude"
	TargetCodex    OwnershipTarget = "codex"
	TargetOpenCode OwnershipTarget = "opencode"
)

var ownershipTargets = map[OwnershipTarget]bool{
	TargetClaude:   true,
	TargetCodex:    true,
	TargetOpenCode: true,
}

// OwnershipTargetFor maps a plan's target onto its ownership target id.
// Plans carry either model agent ids ("claude-code") or ownership target ids
// ("claude"); agent ids are translated, ownership ids pass through, anything
// OwnershipTargetFor converts an agent or ownership target identifier to an ownership target.
// It returns an error for unsupported identifiers.
func OwnershipTargetFor(id string) (OwnershipTarget, error) {
	switch id {
	case string(model.AgentClaudeCode):
		return TargetClaude, nil
	case string(model.AgentCodex):
		return TargetCodex, nil
	case string(model.AgentOpenCode):
		return TargetOpenCode, nil
	}
	target := OwnershipTarget(id)
	if !ownershipTargets[target] {
		return "", fmt.Errorf("unsupported target %q", id)
	}
	return target, nil
}

// OwnershipEntry records one managed file. Path is slash-relative to the
// deployment root. SHA256 is the hex digest of the managed content Takt wrote;
// PriorSHA256 captures the pre-existing user content before takeover (empty
// when the file did not pre-exist); BackupPath points at a preserved copy of
// that prior content when one exists.
type OwnershipEntry struct {
	Path        string            `json:"path"`
	SHA256      string            `json:"sha256"`
	Mode        uint32            `json:"mode"`
	PreExisting bool              `json:"preExisting,omitempty"`
	PriorSHA256 string            `json:"priorSha256,omitempty"`
	BackupPath  string            `json:"backupPath,omitempty"`
	Targets     []OwnershipTarget `json:"targets"`
}

// OwnershipManifest is the cross-target superset of the per-target manifests:
// the single source of truth for which files Takt owns beneath a deployment
// root. Later lifecycle phases consume it to drive sync and uninstall.
type OwnershipManifest struct {
	Version int                       `json:"version"`
	Entries map[string]OwnershipEntry `json:"entries"`
}

// NewOwnershipEntry validates its inputs and computes the SHA-256 digest of
// the managed content, so callers never hand-maintain hashes. Targets are
// NewOwnershipEntry creates a validated ownership entry and computes the SHA-256
// hash of its managed content. Targets must be supported and unique; they are
// stored in sorted order. Prior content metadata is required for pre-existing
// files and rejected otherwise. It returns an error for invalid paths, empty
// content, invalid modes, prior hashes, or targets.
func NewOwnershipEntry(managedPath string, content []byte, mode os.FileMode, preExisting bool, priorSHA256, backupPath string, targets ...OwnershipTarget) (OwnershipEntry, error) {
	clean, err := artifacts.NormalizeRelPath(managedPath)
	if err != nil {
		return OwnershipEntry{}, fmt.Errorf("invalid ownership entry path %q: %w", managedPath, err)
	}
	if len(content) == 0 {
		return OwnershipEntry{}, fmt.Errorf("ownership entry %q requires managed content", clean)
	}
	if mode == 0 {
		return OwnershipEntry{}, fmt.Errorf("ownership entry %q requires a non-zero file mode", clean)
	}
	digest := sha256.Sum256(content)
	entry := OwnershipEntry{
		Path:        clean,
		SHA256:      hex.EncodeToString(digest[:]),
		Mode:        uint32(mode.Perm()),
		PreExisting: preExisting,
		BackupPath:  backupPath,
	}
	if preExisting {
		if prior, err := hex.DecodeString(priorSHA256); err != nil || len(prior) != sha256.Size {
			return OwnershipEntry{}, fmt.Errorf("ownership entry %q pre-existing requires a valid prior SHA-256", clean)
		}
		entry.PriorSHA256 = priorSHA256
	} else if priorSHA256 != "" {
		return OwnershipEntry{}, fmt.Errorf("ownership entry %q has prior SHA-256 but is not marked pre-existing", clean)
	}
	seen := make(map[OwnershipTarget]bool, len(targets))
	for _, target := range targets {
		if !ownershipTargets[target] {
			return OwnershipEntry{}, fmt.Errorf("ownership entry %q has unknown target %q", clean, target)
		}
		if seen[target] {
			return OwnershipEntry{}, fmt.Errorf("ownership entry %q duplicates target %q", clean, target)
		}
		seen[target] = true
		entry.Targets = append(entry.Targets, target)
	}
	if len(entry.Targets) == 0 {
		return OwnershipEntry{}, fmt.Errorf("ownership entry %q requires at least one target", clean)
	}
	sort.Slice(entry.Targets, func(i, j int) bool { return entry.Targets[i] < entry.Targets[j] })
	return entry, nil
}

// NewOwnershipManifest returns an empty manifest ready for Add calls.
func NewOwnershipManifest() *OwnershipManifest {
	return &OwnershipManifest{
		Version: OwnershipManifestVersion,
		Entries: make(map[string]OwnershipEntry),
	}
}

// Add inserts entries, replacing any entry already recorded for the same
// path. Entries must come from NewOwnershipEntry, which owns validation; Add
// only rejects a mismatched manifest version so a manifest loaded by a future
// reader cannot be mutated with stale rules.
func (m *OwnershipManifest) Add(entries ...OwnershipEntry) error {
	if m.Version != OwnershipManifestVersion {
		return fmt.Errorf("cannot add entries to ownership manifest version %d", m.Version)
	}
	for _, entry := range entries {
		m.Entries[entry.Path] = entry
	}
	return nil
}

// Save writes the manifest to OwnershipManifestFilename beneath rootDir,
// creating parent directories as needed. Serialization is deterministic, so
// saving identical state twice produces identical bytes (idempotent).
func (m *OwnershipManifest) Save(rootDir string) error {
	if strings.TrimSpace(rootDir) == "" {
		return fmt.Errorf("deployment root is required")
	}
	content, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ownership manifest: %w", err)
	}
	content = append(content, '\n')
	destination := filepath.Join(rootDir, OwnershipManifestFilename)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return fmt.Errorf("prepare ownership manifest directory: %w", err)
	}
	staged, err := stageFile(filepath.Dir(destination), content, 0o644)
	if err != nil {
		return fmt.Errorf("write ownership manifest: %w", err)
	}
	if err := renameFile(staged, destination); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("write ownership manifest: %w", err)
	}
	if err := syncDir(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("write ownership manifest: %w", err)
	}
	return nil
}

// LoadOwnershipManifest reads the manifest from the deployment root. A missing
// file reports an error wrapping os.ErrNotExist; an unsupported future version
// LoadOwnershipManifest reads and validates the ownership manifest from rootDir.
// It returns an error if the file is missing, malformed, or uses an unsupported
// manifest version.
func LoadOwnershipManifest(rootDir string) (*OwnershipManifest, error) {
	raw, err := os.ReadFile(filepath.Join(rootDir, OwnershipManifestFilename))
	if err != nil {
		return nil, fmt.Errorf("load ownership manifest: %w", err)
	}
	var manifest OwnershipManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("parse ownership manifest: %w", err)
	}
	if manifest.Version != OwnershipManifestVersion {
		return nil, fmt.Errorf("unsupported ownership manifest version %d (this build supports version %d)", manifest.Version, OwnershipManifestVersion)
	}
	if manifest.Entries == nil {
		manifest.Entries = make(map[string]OwnershipEntry)
	}
	return &manifest, nil
}

// IsManaged reports whether the given slash-relative path is owned by Takt.
func (m *OwnershipManifest) IsManaged(managedPath string) bool {
	_, exists := m.Entries[path.Clean(managedPath)]
	return exists
}

// EntriesForTarget returns the entries owned by the given target, sorted by
// path. Unknown targets yield an empty slice.
func (m *OwnershipManifest) EntriesForTarget(target OwnershipTarget) []OwnershipEntry {
	paths := make([]string, 0)
	for entryPath, entry := range m.Entries {
		for _, owner := range entry.Targets {
			if owner == target {
				paths = append(paths, entryPath)
				break
			}
		}
	}
	sort.Strings(paths)
	entries := make([]OwnershipEntry, 0, len(paths))
	for _, entryPath := range paths {
		entries = append(entries, m.Entries[entryPath])
	}
	return entries
}
