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
	"path/filepath"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
)

// OwnershipManifestVersion is the schema this build writes; other versions are never misread.
const OwnershipManifestVersion = 1

// OwnershipManifestFilename records every file Takt manages with hashes, takeover state, and owners.
const OwnershipManifestFilename = ".takt-manifest.json"

// OwnershipTarget names a deployment target that owns managed files.
type OwnershipTarget string

// Supported ownership targets: the native agent targets plus the skills
// target, which owns deployed skill files under .opencode/skills/.
const (
	// TargetOpenCode is the OpenCode harness target.
	TargetOpenCode OwnershipTarget = "opencode"
	// TargetSkills owns deployed skill files shared across harnesses.
	TargetSkills OwnershipTarget = "skills"
)

// ownershipTargets is the set of identifiers Uninstall and ownership accept.
var ownershipTargets = map[OwnershipTarget]bool{
	TargetOpenCode: true,
	TargetSkills:   true,
}

// OwnershipTargetFor converts an agent or ownership target identifier to an ownership target.
// It returns an error for unsupported identifiers.
func OwnershipTargetFor(id string) (OwnershipTarget, error) {
	if target := OwnershipTarget(id); ownershipTargets[target] {
		return target, nil
	}
	return "", fmt.Errorf("unsupported target %q", id)
}

// OwnershipEntry records one managed file: its current digest, takeover
// history, mode, and owners.
type OwnershipEntry struct {
	// Path is the slash-relative managed path under the deployment root.
	Path string `json:"path"`
	// SHA256 is the hex digest of the managed content Takt wrote.
	SHA256 string `json:"sha256"`
	// Mode is the permission bits recorded at deploy time.
	Mode uint32 `json:"mode"`
	// PreExisting means the file predates Takt ownership and is never deleted on uninstall.
	PreExisting bool `json:"preExisting,omitempty"`
	// PriorSHA256 is the digest of content Takt overwrote; empty when nothing was ever overwritten.
	PriorSHA256 string `json:"priorSha256,omitempty"`
	// BackupPath is the root-relative backup holding overwritten content for restore.
	BackupPath string `json:"backupPath,omitempty"`
	// Targets are the owning targets in sorted order.
	Targets []OwnershipTarget `json:"targets"`
}

// OwnershipManifest is the single source of truth for which files Takt owns beneath a deployment root.
// Sync and uninstall consume it to decide what to preserve, restore, or remove.
type OwnershipManifest struct {
	// Version is the schema version; only OwnershipManifestVersion loads.
	Version int `json:"version"`
	// Entries maps slash-relative paths to their ownership records.
	Entries map[string]OwnershipEntry `json:"entries"`
}

// NewOwnershipEntry creates a validated ownership entry and digests its managed content.
// Targets must be supported and unique; pre-existing files must carry their prior hash.
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
	entry := OwnershipEntry{
		Path:        clean,
		SHA256:      hashOf(content),
		Mode:        uint32(mode.Perm()),
		PreExisting: preExisting,
		BackupPath:  backupPath,
	}
	if err := setPriorHash(&entry, clean, preExisting, priorSHA256); err != nil {
		return OwnershipEntry{}, err
	}
	if err := setOwnershipTargets(&entry, clean, targets); err != nil {
		return OwnershipEntry{}, err
	}
	return entry, nil
}

func setPriorHash(entry *OwnershipEntry, path string, preExisting bool, priorSHA256 string) error {
	if priorSHA256 == "" {
		if preExisting {
			return fmt.Errorf("ownership entry %q pre-existing requires a valid prior SHA-256", path)
		}
		return nil
	}
	prior, err := hex.DecodeString(priorSHA256)
	if err != nil || len(prior) != sha256.Size {
		return fmt.Errorf("ownership entry %q requires a valid prior SHA-256", path)
	}
	entry.PriorSHA256 = priorSHA256
	return nil
}

func setOwnershipTargets(entry *OwnershipEntry, path string, targets []OwnershipTarget) error {
	seen := make(map[OwnershipTarget]bool, len(targets))
	for _, target := range targets {
		if !ownershipTargets[target] {
			return fmt.Errorf("ownership entry %q has unknown target %q", path, target)
		}
		if seen[target] {
			return fmt.Errorf("ownership entry %q duplicates target %q", path, target)
		}
		seen[target] = true
		entry.Targets = append(entry.Targets, target)
	}
	if len(entry.Targets) == 0 {
		return fmt.Errorf("ownership entry %q requires at least one target", path)
	}
	slices.Sort(entry.Targets)
	return nil
}

// NewOwnershipManifest returns an empty manifest ready for Add calls.
func NewOwnershipManifest() *OwnershipManifest {
	return &OwnershipManifest{
		Version: OwnershipManifestVersion,
		Entries: make(map[string]OwnershipEntry),
	}
}

// Add inserts entries, replacing any entry already recorded for the same path.
// Entries must come from NewOwnershipEntry; only a mismatched manifest version is rejected.
func (m *OwnershipManifest) Add(entries ...OwnershipEntry) error {
	if m.Version != OwnershipManifestVersion {
		return fmt.Errorf("cannot add entries to ownership manifest version %d", m.Version)
	}
	for _, entry := range entries {
		m.Entries[entry.Path] = entry
	}
	return nil
}

// Save writes the manifest beneath rootDir with deterministic bytes, so identical state saves identically.
func (m *OwnershipManifest) Save(rootDir string) error {
	return saveRecord(rootDir, OwnershipManifestFilename, "ownership manifest", m)
}

// LoadOwnershipManifest reads and validates the manifest from rootDir.
// A missing file, malformed JSON, or foreign version is an error callers handle distinctly.
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
	// Reject any manifest entry whose recorded path escapes the deployment
	// root. A crafted or corrupted manifest with keys like "../victim.txt"
	// would otherwise let later phases (sync, uninstall) act on files outside
	// root, since they join the key to root without further checks.
	for entryPath := range manifest.Entries {
		if _, err := SafeJoin(rootDir, entryPath); err != nil {
			return nil, fmt.Errorf("load ownership manifest: entry path %q escapes deployment root: %w", entryPath, err)
		}
	}
	return &manifest, nil
}

// SafeJoin joins root with a slash-relative path, guaranteed to stay inside root.
// It rejects escapes and symlink-ancestor exits, so crafted manifest keys can never address outside files.
func SafeJoin(root, rel string) (string, error) {
	if strings.TrimSpace(rel) == "" {
		return "", fmt.Errorf("safe join: empty relative path")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("safe join: absolute path not allowed: %q", rel)
	}
	cleanRel := filepath.Clean(rel)
	for _, part := range strings.Split(cleanRel, string(filepath.Separator)) {
		if part == ".." {
			return "", fmt.Errorf("safe join: relative path escapes root: %q", rel)
		}
	}
	rootReal, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("safe join: cannot resolve root %q: %w", root, err)
	}
	joined := filepath.Join(rootReal, cleanRel)
	if err := verifyNoSymlinkEscape(joined, rootReal); err != nil {
		return "", err
	}
	return joined, nil
}

// verifyNoSymlinkEscape walks every existing ancestor directory of joined,
// from its parent up to root, and rejects the path if any ancestor, once its
// symlinks are resolved, lands outside rootReal.
func verifyNoSymlinkEscape(joined, rootReal string) error {
	dir := filepath.Dir(joined)
	for {
		if real, err := filepath.EvalSymlinks(dir); err == nil {
			if !isWithin(real, rootReal) {
				return fmt.Errorf("safe join: parent directory %q escapes deployment root via symlink", dir)
			}
		}
		if dir == rootReal {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil
		}
		dir = parent
	}
}

// isWithin reports whether path is the same as or nested beneath root.
func isWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
