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
	"fmt"
	"os"
	"path/filepath"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
)

// RestoreResult reports which managed paths had their pre-existing content restored from backup.
type RestoreResult struct {
	// Restored lists managed paths whose pre-existing content came back from backup.
	Restored []string `json:"restored"`
}

// Restore writes each entry's backed-up pre-existing content back, undoing Takt's takeover.
// Entries without backups are skipped; crafted paths can never write outside rootDir.
func Restore(rootDir string, manifest *OwnershipManifest) (RestoreResult, error) {
	if manifest == nil {
		return RestoreResult{}, fmt.Errorf("ownership manifest is required")
	}
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolve deployment root: %w", err)
	}
	result := RestoreResult{}
	for _, entryPath := range sortedEntryPaths(manifest) {
		entry := manifest.Entries[entryPath]
		if entry.BackupPath == "" {
			continue
		}
		if err := restoreEntry(root, entry); err != nil {
			return RestoreResult{}, err
		}
		result.Restored = append(result.Restored, entry.Path)
	}
	return result, nil
}

// restoreEntry restores a single entry's backed-up content to its managed
// path, both resolved through SafeJoin so neither can escape root.
func restoreEntry(root string, entry OwnershipEntry) error {
	cleanPath, cleanBackup, err := normalizeRestorePaths(entry)
	if err != nil {
		return err
	}
	backupSrc, err := SafeJoin(root, filepath.FromSlash(cleanBackup))
	if err != nil {
		return fmt.Errorf("resolve backup for %q: %w", cleanPath, err)
	}
	content, err := os.ReadFile(backupSrc)
	if err != nil {
		return fmt.Errorf("read backup for %q: %w", cleanPath, err)
	}
	destination, err := SafeJoin(root, filepath.FromSlash(cleanPath))
	if err != nil {
		return fmt.Errorf("resolve destination for %q: %w", cleanPath, err)
	}
	mode := os.FileMode(entry.Mode)
	if mode == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(destination), ManagedDirectoryMode); err != nil {
		return fmt.Errorf("prepare destination for %q: %w", cleanPath, err)
	}
	staged, err := stageFile(filepath.Dir(destination), content, mode)
	if err != nil {
		return fmt.Errorf("stage restore for %q: %w", cleanPath, err)
	}
	if err := renameFile(staged, destination); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("restore %q: %w", cleanPath, err)
	}
	if err := syncDir(filepath.Dir(destination)); err != nil {
		return fmt.Errorf("restore %q: %w", cleanPath, err)
	}
	return nil
}

func normalizeRestorePaths(entry OwnershipEntry) (string, string, error) {
	cleanPath, err := artifacts.NormalizeRelPath(entry.Path)
	if err != nil {
		return "", "", fmt.Errorf("invalid managed path %q: %w", entry.Path, err)
	}
	cleanBackup, err := artifacts.NormalizeRelPath(entry.BackupPath)
	if err != nil {
		return "", "", fmt.Errorf("invalid backup path %q: %w", entry.BackupPath, err)
	}
	return cleanPath, cleanBackup, nil
}
