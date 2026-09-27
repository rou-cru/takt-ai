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

// Package setup implements the install, sync, and uninstall lifecycle:
// it builds target plans from a request, deploys rendered artifacts with
// transactional rollback, and tracks ownership through a persistent manifest.
package setup

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

// renameFile swaps staged content into place; tests override it to inject mid-commit failures.
var renameFile = os.Rename

// Deployment modes make deployed artifacts user-readable and traversable.
const (
	// ManagedFileMode is the mode used for deployed user-readable artifacts.
	ManagedFileMode os.FileMode = 0o644
	// ManagedDirectoryMode allows traversal of the deployment tree.
	ManagedDirectoryMode os.FileMode = 0o755
)

// Artifact is renderer output ready for deployment.
type Artifact struct {
	// Path is the slash-relative destination under the deployment root.
	Path string
	// Content is the exact bytes to write.
	Content []byte
}

// DeploymentResult reports the normalized paths changed or left untouched.
type DeploymentResult struct {
	// Changed lists normalized paths written by this deployment.
	Changed []string `json:"changed"`
	// Unchanged lists managed paths already matching deployed content.
	Unchanged []string `json:"unchanged"`
	// Actions lists provider actions that completed after deployment.
	Actions []string `json:"actions,omitempty"`
	// NotApplied lists planned changes a cancellation left unapplied; empty when the deployment completed.
	NotApplied []string `json:"notApplied,omitempty"`
}

// DeployContext is Deploy with cooperative cancellation: it returns partial
// results and ctx.Err() without rolling back what is already installed.
func DeployContext(ctx context.Context, rootDir string, managedPaths []string, artifacts []Artifact) (DeploymentResult, error) {
	root, normalized, err := prepareDeployment(rootDir, managedPaths, artifacts)
	if err != nil {
		return DeploymentResult{}, err
	}
	result, pending, err := inspectDeployments(root, normalized)
	if err != nil || len(pending) == 0 {
		return result, err
	}

	transaction := newDeploymentTransaction(pending)
	if err := transaction.stage(pending); err != nil {
		return DeploymentResult{}, transaction.abort(err)
	}
	if err := transaction.commit(ctx); err != nil {
		if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
			return transaction.stopCancelled(result, err)
		}
		return DeploymentResult{}, transaction.abort(err)
	}
	if err := transaction.cleanupTemps(); err != nil {
		return DeploymentResult{}, fmt.Errorf("deployment committed but temporary cleanup failed: %w", err)
	}
	for _, pendingFile := range pending {
		result.Changed = append(result.Changed, pendingFile.path)
	}
	return result, nil
}

// prepareDeployment validates the deployment inputs once and resolves the
// absolute deployment root.
func prepareDeployment(rootDir string, managedPaths []string, artifacts []Artifact) (string, []Artifact, error) {
	if strings.TrimSpace(rootDir) == "" {
		return "", nil, fmt.Errorf("deployment root is required")
	}
	managed, err := validateManagedPaths(managedPaths)
	if err != nil {
		return "", nil, err
	}
	normalized, err := validateArtifacts(managed, artifacts)
	if err != nil {
		return "", nil, err
	}
	if err := validateArtifactPathConflicts(normalized); err != nil {
		return "", nil, err
	}
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", nil, fmt.Errorf("resolve deployment root: %w", err)
	}
	return root, normalized, nil
}

// inspectDeployments compares every artifact against the filesystem and
// reports unchanged paths plus the artifacts that still need to be written.
func inspectDeployments(root string, normalized []Artifact) (DeploymentResult, []pendingDeployment, error) {
	result := DeploymentResult{
		Changed:   make([]string, 0, len(normalized)),
		Unchanged: make([]string, 0, len(normalized)),
	}
	pending := make([]pendingDeployment, 0, len(normalized))
	for _, artifact := range normalized {
		item, unchanged, err := inspectArtifact(root, artifact)
		if err != nil {
			return DeploymentResult{}, nil, err
		}
		if unchanged {
			result.Unchanged = append(result.Unchanged, artifact.Path)
			continue
		}
		pending = append(pending, item)
	}
	return result, pending, nil
}

// inspectArtifact classifies one managed path as unchanged or as a pending
// write, capturing the current bytes and mode for backup when it exists.
func inspectArtifact(root string, artifact Artifact) (pendingDeployment, bool, error) {
	destination := filepath.Join(root, filepath.FromSlash(artifact.Path))
	missingDirs, err := inspectDeploymentPath(root, artifact.Path)
	if err != nil {
		return pendingDeployment{}, false, err
	}

	info, statErr := os.Stat(destination)
	exists := statErr == nil
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return pendingDeployment{}, false, fmt.Errorf("inspect managed artifact %q: %w", artifact.Path, statErr)
	}
	if exists && !info.Mode().IsRegular() {
		return pendingDeployment{}, false, fmt.Errorf("managed artifact %q is not a regular file", artifact.Path)
	}
	if !exists {
		return pendingDeployment{
			path:        artifact.Path,
			destination: destination,
			mode:        ManagedFileMode,
			content:     artifact.Content,
			missingDirs: missingDirs,
		}, false, nil
	}

	current, readErr := os.ReadFile(destination)
	if readErr != nil {
		return pendingDeployment{}, false, fmt.Errorf("read managed artifact %q: %w", artifact.Path, readErr)
	}
	if bytes.Equal(current, artifact.Content) {
		return pendingDeployment{}, true, nil
	}
	return pendingDeployment{
		path:        artifact.Path,
		destination: destination,
		exists:      true,
		mode:        info.Mode().Perm(),
		original:    current,
		content:     artifact.Content,
		missingDirs: missingDirs,
	}, false, nil
}

// newDeploymentTransaction stages a batch of pending writes for atomic commit or rollback.
func newDeploymentTransaction(pending []pendingDeployment) *deploymentTransaction {
	return &deploymentTransaction{
		files:   make([]*stagedDeployment, 0, len(pending)),
		created: make(map[string]struct{}),
	}
}

// stage writes every pending artifact to temp files without touching final locations.
// Missing directories are created and recorded for commit or rollback.
func (tx *deploymentTransaction) stage(pending []pendingDeployment) error {
	for _, pendingFile := range pending {
		file := &stagedDeployment{
			path:        pendingFile.path,
			destination: pendingFile.destination,
			exists:      pendingFile.exists,
			mode:        pendingFile.mode,
		}
		tx.files = append(tx.files, file)
		if err := tx.stageOne(file, pendingFile); err != nil {
			return err
		}
	}
	return nil
}

// stageOne writes one artifact's content and backup to temp files without touching its destination.
func (tx *deploymentTransaction) stageOne(file *stagedDeployment, pending pendingDeployment) error {
	if err := createMissingDirectories(pending.missingDirs, tx.created, &tx.createdOrder); err != nil {
		return fmt.Errorf("prepare managed artifact %q: %w", pending.path, err)
	}
	staged, err := stageFile(filepath.Dir(pending.destination), pending.content, pending.mode)
	if err != nil {
		return fmt.Errorf("stage managed artifact %q: %w", pending.path, err)
	}
	file.staged = staged
	if !pending.exists {
		return nil
	}
	backup, err := stageFile(filepath.Dir(pending.destination), pending.original, pending.mode)
	if err != nil {
		return fmt.Errorf("backup managed artifact %q: %w", pending.path, err)
	}
	file.backup = backup
	return nil
}

// pendingDeployment is one artifact awaiting install with its current bytes and mode for backup.
type pendingDeployment struct {
	path        string
	destination string
	exists      bool
	mode        os.FileMode
	original    []byte
	content     []byte
	missingDirs []string
}

// stagedDeployment is one artifact written to temp files, awaiting rename into place.
type stagedDeployment struct {
	path        string
	destination string
	staged      string
	backup      string
	exists      bool
	mode        os.FileMode
	installed   bool
}

// deploymentTransaction tracks staged files and created directories for commit, rollback, or cleanup.
type deploymentTransaction struct {
	files        []*stagedDeployment
	created      map[string]struct{}
	createdOrder []string
}

// abort restores installed files and reports the original error with any rollback failure.
func (tx *deploymentTransaction) abort(err error) error {
	if rollbackErr := tx.rollback(); rollbackErr != nil {
		return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
	}
	return err
}

// commit renames staged files into place in order, stopping early when ctx is cancelled.
func (tx *deploymentTransaction) commit(ctx context.Context) error {
	for _, file := range tx.files {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := renameFile(file.staged, file.destination); err != nil {
			return fmt.Errorf("install managed artifact %q: %w", file.path, err)
		}
		file.installed = true
		if err := syncDir(filepath.Dir(file.destination)); err != nil {
			return fmt.Errorf("install managed artifact %q: %w", file.path, err)
		}
	}
	for index := len(tx.createdOrder) - 1; index >= 0; index-- {
		if err := syncDir(tx.createdOrder[index]); err != nil {
			return fmt.Errorf("sync created directory %q: %w", tx.createdOrder[index], err)
		}
	}
	return nil
}

// stopCancelled ends a commit interrupted between artifacts: installed files stay,
// staged temporaries are discarded, and empty created directories are removed best-effort.
func (tx *deploymentTransaction) stopCancelled(result DeploymentResult, cause error) (DeploymentResult, error) {
	for _, file := range tx.files {
		if file.installed {
			result.Changed = append(result.Changed, file.path)
		} else {
			result.NotApplied = append(result.NotApplied, file.path)
		}
	}
	if err := tx.cleanupTemps(); err != nil {
		return result, errors.Join(cause, err)
	}
	for index := len(tx.createdOrder) - 1; index >= 0; index-- {
		_ = os.Remove(tx.createdOrder[index]) // non-empty dirs hold installed files and stay
	}
	return result, cause
}

// rollback restores backups, removes new files, and deletes created directories in reverse order.
func (tx *deploymentTransaction) rollback() error {
	var rollbackErrors []error
	for index := len(tx.files) - 1; index >= 0; index-- {
		if err := tx.files[index].restore(); err != nil {
			rollbackErrors = append(rollbackErrors, err)
		}
	}
	if err := tx.cleanupTemps(); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	for index := len(tx.createdOrder) - 1; index >= 0; index-- {
		directory := tx.createdOrder[index]
		if err := os.Remove(directory); err != nil && !errors.Is(err, os.ErrNotExist) {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove created directory %q: %w", directory, err))
			continue
		}
		if err := syncDir(filepath.Dir(directory)); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("sync parent of removed directory %q: %w", directory, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

// restore undoes one installed artifact: existing files get their backup
// back, brand-new files are removed.
func (file *stagedDeployment) restore() error {
	if !file.installed {
		return nil
	}
	if file.exists {
		return file.restoreExisting()
	}
	return file.removeInstalled()
}

// restoreExisting puts back one overwritten file from backup and syncs its parent.
func (file *stagedDeployment) restoreExisting() error {
	if err := restoreBackup(file); err != nil {
		return fmt.Errorf("restore managed artifact %q: %w", file.path, err)
	}
	if err := syncDir(filepath.Dir(file.destination)); err != nil {
		return fmt.Errorf("sync managed artifact %q parent after restore: %w", file.path, err)
	}
	return nil
}

// removeInstalled deletes one newly installed file and syncs its parent.
func (file *stagedDeployment) removeInstalled() error {
	if err := os.Remove(file.destination); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("remove new managed artifact %q: %w", file.path, err)
	}
	if err := syncDir(filepath.Dir(file.destination)); err != nil {
		return fmt.Errorf("sync managed artifact %q parent after removal: %w", file.path, err)
	}
	return nil
}

// cleanupTemps removes staged temp files left by a commit, abort, or cancellation.
func (tx *deploymentTransaction) cleanupTemps() error {
	var cleanupErrors []error
	for _, file := range tx.files {
		for _, temporary := range []string{file.staged, file.backup} {
			if temporary == "" {
				continue
			}
			if err := os.Remove(temporary); err != nil && !errors.Is(err, os.ErrNotExist) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove temporary file %q: %w", temporary, err))
			}
		}
	}
	return errors.Join(cleanupErrors...)
}

// restoreBackup restores the original file from its backup to the destination.
func restoreBackup(file *stagedDeployment) error {
	if err := os.Rename(file.backup, file.destination); err == nil {
		return nil
	} else {
		renameErr := err
		if removeErr := os.Remove(file.destination); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return fmt.Errorf("rename backup: %v; remove installed file: %w", renameErr, removeErr)
		}
		if err := os.Rename(file.backup, file.destination); err != nil {
			return fmt.Errorf("rename backup: %v; retry restore: %w", renameErr, err)
		}
	}
	return nil
}

// validateArtifactPathConflicts checks whether any artifact path is a parent of another artifact path.
// It returns an error describing the conflicting paths when a conflict is found.
func validateArtifactPathConflicts(artifacts []Artifact) error {
	seen := make(map[string]struct{}, len(artifacts))
	for _, artifact := range artifacts {
		for parent := path.Dir(artifact.Path); parent != "."; parent = path.Dir(parent) {
			if _, exists := seen[parent]; exists {
				return fmt.Errorf("artifact path %q conflicts with child artifact %q", parent, artifact.Path)
			}
		}
		seen[artifact.Path] = struct{}{}
	}
	return nil
}

// inspectDeploymentPath checks a deployment path and returns missing ancestors in creation order.
// Symlinks and non-directory parents are an error.
func inspectDeploymentPath(root, relativePath string) ([]string, error) {
	var missing []string
	current := root
	components := strings.Split(relativePath, "/")
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, fmt.Errorf("inspect deployment path %q: %w", relativePath, err)
			}
			if index < len(components)-1 {
				missing = append(missing, current)
			}
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("deployment path %q contains a symlink", relativePath)
		}
		if index < len(components)-1 && !info.IsDir() {
			return nil, fmt.Errorf("parent directory for managed artifact %q is not a directory", relativePath)
		}
	}
	return missing, nil
}

// createMissingDirectories creates the specified directories in order and
// records each directory for potential rollback.
func createMissingDirectories(directories []string, created map[string]struct{}, createdOrder *[]string) error {
	for _, directory := range directories {
		if _, exists := created[directory]; exists {
			continue
		}
		if err := os.Mkdir(directory, ManagedDirectoryMode); err != nil && !os.IsExist(err) {
			return fmt.Errorf("create parent directory %q: %w", directory, err)
		}
		created[directory] = struct{}{}
		*createdOrder = append(*createdOrder, directory)
	}
	return nil
}

// validateManagedPaths normalizes managed paths and rejects invalid or duplicate entries.
func validateManagedPaths(paths []string) (map[string]struct{}, error) {
	managed := make(map[string]struct{}, len(paths))
	for _, candidate := range paths {
		clean, err := artifacts.NormalizeRelPath(candidate)
		if err != nil {
			return nil, fmt.Errorf("invalid managed path: %w", err)
		}
		if _, exists := managed[clean]; exists {
			return nil, fmt.Errorf("duplicate managed path %q", clean)
		}
		managed[clean] = struct{}{}
	}
	return managed, nil
}

// validateArtifacts normalizes, validates, and sorts artifact paths against the managed paths.
// It returns an error for invalid, duplicate, or unmanaged paths.
func validateArtifacts(managed map[string]struct{}, input []Artifact) ([]Artifact, error) {
	normalized := make([]Artifact, 0, len(input))
	seen := make(map[string]struct{}, len(input))
	for _, artifact := range input {
		clean, err := artifacts.NormalizeRelPath(artifact.Path)
		if err != nil {
			return nil, fmt.Errorf("invalid artifact path: %w", err)
		}
		if _, exists := seen[clean]; exists {
			return nil, fmt.Errorf("duplicate artifact path %q", clean)
		}
		if _, managed := managed[clean]; !managed {
			return nil, fmt.Errorf("artifact path %q is not managed", clean)
		}
		seen[clean] = struct{}{}
		normalized = append(normalized, Artifact{Path: clean, Content: artifact.Content})
	}
	slices.SortFunc(normalized, func(a, b Artifact) int { return cmp.Compare(a.Path, b.Path) })
	return normalized, nil
}

// stageFile creates a temp file with the given mode and content, removing it when any step fails.
func stageFile(directory string, content []byte, mode os.FileMode) (string, error) {
	return filemerge.StageTempFile(directory, ".takt-setup-*", content, mode)
}

// syncDir delegates to filemerge.SyncDir so directory-sync behavior has one implementation.
func syncDir(path string) error {
	return filemerge.SyncDir(path)
}
