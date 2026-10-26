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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
)

// FileConflict reasons, one per destination state restore refuses to overwrite.
const (
	// ConflictModifiedSinceInstall marks destination bytes that differ from
	// both the deployed content and the backup: an edit made after the
	// backup was taken.
	ConflictModifiedSinceInstall = "modified_since_install"
	// ConflictUnknownInstalledHash marks an entry whose recorded installed
	// digest cannot establish that the destination is unchanged; an
	// unknown hash is never evidence of equality.
	ConflictUnknownInstalledHash = "unknown_installed_hash"
	// ConflictDestinationType marks a destination that is not a regular
	// file, such as a directory or symlink.
	ConflictDestinationType = "destination_type"
)

// FileConflict is one managed path restore left untouched because its
// destination state is not safe to overwrite.
type FileConflict struct {
	// Path is the managed path.
	Path string `json:"path"`
	// CurrentHash is the digest of the current destination bytes; empty when the destination is not a regular file.
	CurrentHash string `json:"currentHash"`
	// ReplacementHash is the digest of the backup content restore would put in place.
	ReplacementHash string `json:"replacementHash"`
	// Reason is the FileConflict constant naming why restore left the path alone.
	Reason string `json:"reason"`
}

// RestoreResult reports the per-file disposition of a restore.
type RestoreResult struct {
	// Restored lists managed paths whose backed-up pre-existing content was put back.
	Restored []string `json:"restored"`
	// Preserved lists managed paths whose current bytes stayed in place: files that already held the backup bytes.
	Preserved []string `json:"preserved"`
	// Conflicts lists managed paths that restore refused to overwrite; their bytes are untouched.
	Conflicts []FileConflict `json:"conflicts"`
	// Incomplete lists managed paths whose restore could not be carried out; the returned error names each failure.
	Incomplete []string `json:"incomplete"`
}

// restoreDisposition classifies one backed-up entry before any write.
type restoreDisposition int

const (
	// restoreEligible: the destination is missing or unchanged since install, so restoring is safe.
	restoreEligible restoreDisposition = iota
	// restoreNoop: the destination already holds the backup bytes, so restoring changes nothing.
	restoreNoop
	// restoreConflicted: the destination state is left untouched and reported as a conflict.
	restoreConflicted
)

// restorePlan is one classified backed-up entry.
type restorePlan struct {
	// path is the normalized managed path, slash-relative.
	path string
	// destination is the safe-joined destination inside the deployment root.
	destination string
	// content is the backup content restore writes.
	content []byte
	// replacementHash is hashOf(content).
	replacementHash string
	// currentHash is the digest of the current destination bytes; empty when the destination is not a regular file.
	currentHash string
	// mode is the permission bits the restored file gets.
	mode os.FileMode
	// disposition is the classification before any write.
	disposition restoreDisposition
	// conflict carries the per-file conflict when disposition is restoreConflicted.
	conflict FileConflict
}

// Restore restores each entry's backed-up pre-existing content, undoing
// Takt's takeover. Entries without backups are skipped; crafted paths can
// never write outside rootDir. A destination edited after the backup was
// taken is never overwritten: the edit survives and the file is reported in
// RestoreResult.Conflicts.
func Restore(rootDir string, manifest *OwnershipManifest) (RestoreResult, error) {
	if manifest == nil {
		return RestoreResult{}, fmt.Errorf("ownership manifest is required")
	}
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("resolve deployment root: %w", err)
	}
	result := RestoreResult{}
	var failures []error
	for _, entryPath := range sortedEntryPaths(manifest) {
		entry := manifest.Entries[entryPath]
		if entry.BackupPath == "" {
			// Entries without a backup hold no pre-existing content to restore.
			continue
		}
		plan, err := classifyRestoreEntry(root, entry)
		if err != nil {
			result.Incomplete = append(result.Incomplete, entry.Path)
			failures = append(failures, err)
			continue
		}
		switch plan.disposition {
		case restoreNoop:
			// The destination already holds the restored bytes.
			result.Preserved = append(result.Preserved, plan.path)
		case restoreEligible:
			if err := applyRestorePlan(plan); err != nil {
				result.Incomplete = append(result.Incomplete, plan.path)
				failures = append(failures, err)
				break
			}
			result.Restored = append(result.Restored, plan.path)
		case restoreConflicted:
			result.Conflicts = append(result.Conflicts, plan.conflict)
		}
	}
	return result, errors.Join(failures...)
}

// loadRestorePlan resolves an entry's paths through SafeJoin, so neither can
// escape root, and reads its backup content.
func loadRestorePlan(root string, entry OwnershipEntry) (restorePlan, error) {
	cleanPath, cleanBackup, err := normalizeRestorePaths(entry)
	if err != nil {
		return restorePlan{}, err
	}
	backupSrc, err := SafeJoin(root, filepath.FromSlash(cleanBackup))
	if err != nil {
		return restorePlan{}, fmt.Errorf("resolve backup for %q: %w", cleanPath, err)
	}
	content, err := os.ReadFile(backupSrc)
	if err != nil {
		return restorePlan{}, fmt.Errorf("read backup for %q: %w", cleanPath, err)
	}
	destination, err := SafeJoin(root, filepath.FromSlash(cleanPath))
	if err != nil {
		return restorePlan{}, fmt.Errorf("resolve destination for %q: %w", cleanPath, err)
	}
	return restorePlan{
		path:            cleanPath,
		destination:     destination,
		content:         content,
		replacementHash: hashOf(content),
		mode:            restoreMode(entry.Mode),
	}, nil
}

// restoreEntry restores a single entry's backed-up content to its managed
// path. The uninstall flow calls it for entries its own plan already verified.
func restoreEntry(root string, entry OwnershipEntry) error {
	plan, err := loadRestorePlan(root, entry)
	if err != nil {
		return err
	}
	return applyRestorePlan(plan)
}

// classifyRestoreEntry reads one entry's backup and destination and decides
// its disposition before any write. Nothing is mutated here.
func classifyRestoreEntry(root string, entry OwnershipEntry) (restorePlan, error) {
	plan, err := loadRestorePlan(root, entry)
	if err != nil {
		return restorePlan{}, err
	}
	cleanPath, destination := plan.path, plan.destination
	info, err := os.Lstat(destination)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// A missing destination cannot lose user content, so it is restorable.
			plan.disposition = restoreEligible
			return plan, nil
		}
		return restorePlan{}, fmt.Errorf("inspect destination for %q: %w", cleanPath, err)
	}
	if !info.Mode().IsRegular() {
		// A directory, symlink, or other non-file destination is a type
		// mismatch; restore never deletes it to put a file in place.
		plan.disposition = restoreConflicted
		plan.conflict = FileConflict{
			Path:            cleanPath,
			ReplacementHash: plan.replacementHash,
			Reason:          ConflictDestinationType,
		}
		return plan, nil
	}
	current, err := os.ReadFile(destination)
	if err != nil {
		return restorePlan{}, fmt.Errorf("read destination for %q: %w", cleanPath, err)
	}
	plan.currentHash = hashOf(current)
	if plan.currentHash == plan.replacementHash {
		// The destination already holds the backup bytes; restoring is a no-op.
		plan.disposition = restoreNoop
		return plan, nil
	}
	installed, ok := canonicalHash(entry.SHA256)
	if !ok {
		// Without the deployed digest, unchanged content cannot be
		// established; an unknown hash is never evidence of equality.
		plan.disposition = restoreConflicted
		plan.conflict = FileConflict{
			Path:            cleanPath,
			CurrentHash:     plan.currentHash,
			ReplacementHash: plan.replacementHash,
			Reason:          ConflictUnknownInstalledHash,
		}
		return plan, nil
	}
	if plan.currentHash == installed {
		// The destination still holds the content Takt deployed.
		plan.disposition = restoreEligible
		return plan, nil
	}
	// The destination bytes changed after the backup was taken.
	plan.disposition = restoreConflicted
	plan.conflict = FileConflict{
		Path:            cleanPath,
		CurrentHash:     plan.currentHash,
		ReplacementHash: plan.replacementHash,
		Reason:          ConflictModifiedSinceInstall,
	}
	return plan, nil
}

// applyRestorePlan writes one plan's backup content to its destination.
// classifyRestoreEntry read the destination immediately before this call,
// which is the scoped content recheck before replacing.
func applyRestorePlan(plan restorePlan) error {
	directory := filepath.Dir(plan.destination)
	if err := os.MkdirAll(directory, ManagedDirectoryMode); err != nil {
		return fmt.Errorf("prepare destination for %q: %w", plan.path, err)
	}
	staged, err := stageFile(directory, plan.content, plan.mode)
	if err != nil {
		return fmt.Errorf("stage restore for %q: %w", plan.path, err)
	}
	if err := renameFile(staged, plan.destination); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("restore %q: %w", plan.path, err)
	}
	if err := syncDir(directory); err != nil {
		return fmt.Errorf("restore %q: %w", plan.path, err)
	}
	return nil
}

// canonicalHash returns the lowercase digest when raw is a valid SHA-256
// hex digest, and ok=false when the recorded digest is unknown.
func canonicalHash(raw string) (digest string, ok bool) {
	if len(raw) != sha256.Size*2 {
		return "", false
	}
	decoded, err := hex.DecodeString(raw)
	if err != nil || len(decoded) != sha256.Size {
		return "", false
	}
	return strings.ToLower(raw), true
}

// restoreMode returns the permission bits a restored file gets, defaulting
// to 0o644 when the entry recorded none.
func restoreMode(mode uint32) os.FileMode {
	if mode == 0 {
		return 0o644
	}
	return os.FileMode(mode)
}

// normalizeRestorePaths validates an entry's managed and backup paths as
// root-relative locations.
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
