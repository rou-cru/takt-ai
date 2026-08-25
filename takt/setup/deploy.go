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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
)

// ponytail: package-level function var instead of interface injection; enough
// for fault-injection tests, swap for a rename interface only if callers need it.
var renameFile = os.Rename

// Artifact is renderer output ready for deployment. Path is relative to the
// deployment root; Content is never retained by the deployer.
type Artifact struct {
	Path    string
	Content []byte
}

// DeploymentResult reports the normalized paths changed or left untouched.
type DeploymentResult struct {
	Changed   []string `json:"changed"`
	Unchanged []string `json:"unchanged"`
}

// Deploy writes only artifacts listed in managedPaths beneath rootDir.
// Existing bytes are compared before writing, and paths absent from artifacts
// are never deleted or modified. Changed artifacts are staged before a batch
// commit; a failed commit triggers best-effort rollback.
func Deploy(rootDir string, managedPaths []string, artifacts []Artifact) (DeploymentResult, error) {
	if strings.TrimSpace(rootDir) == "" {
		return DeploymentResult{}, fmt.Errorf("deployment root is required")
	}
	managed, err := validateManagedPaths(managedPaths)
	if err != nil {
		return DeploymentResult{}, err
	}
	normalized, err := validateArtifacts(managed, artifacts)
	if err != nil {
		return DeploymentResult{}, err
	}
	if err := validateArtifactPathConflicts(normalized); err != nil {
		return DeploymentResult{}, err
	}
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return DeploymentResult{}, fmt.Errorf("resolve deployment root: %w", err)
	}

	result := DeploymentResult{
		Changed:   make([]string, 0, len(normalized)),
		Unchanged: make([]string, 0, len(normalized)),
	}
	pending := make([]pendingDeployment, 0, len(normalized))
	for _, artifact := range normalized {
		destination := filepath.Join(root, filepath.FromSlash(artifact.Path))
		missingDirs, err := inspectDeploymentPath(root, artifact.Path)
		if err != nil {
			return DeploymentResult{}, err
		}

		info, statErr := os.Stat(destination)
		exists := statErr == nil
		if statErr != nil && !os.IsNotExist(statErr) {
			return DeploymentResult{}, fmt.Errorf("inspect managed artifact %q: %w", artifact.Path, statErr)
		}
		if exists && !info.Mode().IsRegular() {
			return DeploymentResult{}, fmt.Errorf("managed artifact %q is not a regular file", artifact.Path)
		}
		if exists {
			current, readErr := os.ReadFile(destination)
			if readErr != nil {
				return DeploymentResult{}, fmt.Errorf("read managed artifact %q: %w", artifact.Path, readErr)
			}
			if bytes.Equal(current, artifact.Content) {
				result.Unchanged = append(result.Unchanged, artifact.Path)
				continue
			}
			pending = append(pending, pendingDeployment{
				path:        artifact.Path,
				destination: destination,
				exists:      true,
				mode:        info.Mode().Perm(),
				original:    current,
				content:     artifact.Content,
				missingDirs: missingDirs,
			})
			continue
		}

		pending = append(pending, pendingDeployment{
			path:        artifact.Path,
			destination: destination,
			mode:        0o644,
			content:     artifact.Content,
			missingDirs: missingDirs,
		})
	}

	if len(pending) == 0 {
		return result, nil
	}

	transaction := deploymentTransaction{
		files:   make([]*stagedDeployment, 0, len(pending)),
		created: make(map[string]struct{}),
	}
	for _, pendingFile := range pending {
		file := &stagedDeployment{
			path:        pendingFile.path,
			destination: pendingFile.destination,
			exists:      pendingFile.exists,
			mode:        pendingFile.mode,
		}
		transaction.files = append(transaction.files, file)

		if err := createMissingDirectories(pendingFile.missingDirs, transaction.created, &transaction.createdOrder); err != nil {
			return DeploymentResult{}, transaction.abort(fmt.Errorf("prepare managed artifact %q: %w", pendingFile.path, err))
		}
		staged, err := stageFile(filepath.Dir(pendingFile.destination), pendingFile.content, pendingFile.mode)
		if err != nil {
			return DeploymentResult{}, transaction.abort(fmt.Errorf("stage managed artifact %q: %w", pendingFile.path, err))
		}
		file.staged = staged
		if pendingFile.exists {
			backup, backupErr := stageFile(filepath.Dir(pendingFile.destination), pendingFile.original, pendingFile.mode)
			if backupErr != nil {
				return DeploymentResult{}, transaction.abort(fmt.Errorf("backup managed artifact %q: %w", pendingFile.path, backupErr))
			}
			file.backup = backup
		}
	}

	if err := transaction.commit(); err != nil {
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

type pendingDeployment struct {
	path        string
	destination string
	exists      bool
	mode        os.FileMode
	original    []byte
	content     []byte
	missingDirs []string
}

type stagedDeployment struct {
	path        string
	destination string
	staged      string
	backup      string
	exists      bool
	mode        os.FileMode
	installed   bool
}

type deploymentTransaction struct {
	files        []*stagedDeployment
	created      map[string]struct{}
	createdOrder []string
}

func (tx *deploymentTransaction) abort(err error) error {
	if rollbackErr := tx.rollback(); rollbackErr != nil {
		return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
	}
	return err
}

func (tx *deploymentTransaction) commit() error {
	for _, file := range tx.files {
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

func (tx *deploymentTransaction) rollback() error {
	var rollbackErrors []error
	for index := len(tx.files) - 1; index >= 0; index-- {
		file := tx.files[index]
		if !file.installed {
			continue
		}
		if file.exists {
			if err := restoreBackup(file); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("restore managed artifact %q: %w", file.path, err))
			} else if err := syncDir(filepath.Dir(file.destination)); err != nil {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("sync managed artifact %q parent after restore: %w", file.path, err))
			}
			continue
		}
		if err := os.Remove(file.destination); err != nil {
			if !os.IsNotExist(err) {
				rollbackErrors = append(rollbackErrors, fmt.Errorf("remove new managed artifact %q: %w", file.path, err))
			}
			continue
		}
		if err := syncDir(filepath.Dir(file.destination)); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("sync managed artifact %q parent after removal: %w", file.path, err))
		}
	}
	if err := tx.cleanupTemps(); err != nil {
		rollbackErrors = append(rollbackErrors, err)
	}
	for index := len(tx.createdOrder) - 1; index >= 0; index-- {
		directory := tx.createdOrder[index]
		if err := os.Remove(directory); err != nil && !os.IsNotExist(err) {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("remove created directory %q: %w", directory, err))
			continue
		}
		if err := syncDir(filepath.Dir(directory)); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("sync parent of removed directory %q: %w", directory, err))
		}
	}
	return errors.Join(rollbackErrors...)
}

func (tx *deploymentTransaction) cleanupTemps() error {
	var cleanupErrors []error
	for _, file := range tx.files {
		for _, temporary := range []string{file.staged, file.backup} {
			if temporary == "" {
				continue
			}
			if err := os.Remove(temporary); err != nil && !os.IsNotExist(err) {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("remove temporary file %q: %w", temporary, err))
			}
		}
	}
	return errors.Join(cleanupErrors...)
}

func restoreBackup(file *stagedDeployment) error {
	if err := os.Rename(file.backup, file.destination); err == nil {
		return nil
	} else {
		renameErr := err
		if removeErr := os.Remove(file.destination); removeErr != nil && !os.IsNotExist(removeErr) {
			return fmt.Errorf("rename backup: %v; remove installed file: %w", renameErr, removeErr)
		}
		if err := os.Rename(file.backup, file.destination); err != nil {
			return fmt.Errorf("rename backup: %v; retry restore: %w", renameErr, err)
		}
	}
	return nil
}

func validateArtifactPathConflicts(artifacts []Artifact) error {
	for index := 0; index+1 < len(artifacts); index++ {
		parent := artifacts[index].Path + "/"
		if strings.HasPrefix(artifacts[index+1].Path, parent) {
			return fmt.Errorf("artifact path %q conflicts with child artifact %q", artifacts[index].Path, artifacts[index+1].Path)
		}
	}
	return nil
}

// inspectDeploymentPath walks every component of relativePath beneath root,
// rejecting symlinks anywhere on the path and non-directory ancestors. It
// returns the missing ancestor directories in parent-first creation order.
func inspectDeploymentPath(root, relativePath string) ([]string, error) {
	var missing []string
	current := root
	components := strings.Split(relativePath, "/")
	for index, component := range components {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			if !os.IsNotExist(err) {
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

// createMissingDirectories creates the directories recorded during inspection,
// parents first, and records newly created ones for rollback.
func createMissingDirectories(directories []string, created map[string]struct{}, createdOrder *[]string) error {
	for _, directory := range directories {
		if _, exists := created[directory]; exists {
			continue
		}
		if err := os.Mkdir(directory, 0o755); err != nil && !os.IsExist(err) {
			return fmt.Errorf("create parent directory %q: %w", directory, err)
		}
		created[directory] = struct{}{}
		*createdOrder = append(*createdOrder, directory)
	}
	return nil
}

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
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].Path < normalized[j].Path })
	return normalized, nil
}

func stageFile(directory string, content []byte, mode os.FileMode) (name string, err error) {
	temporary, err := os.CreateTemp(directory, ".takt-setup-*")
	if err != nil {
		return "", err
	}
	name = temporary.Name()
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(name)
		}
	}()
	if err = temporary.Chmod(mode); err != nil {
		return "", err
	}
	if _, err = temporary.Write(content); err != nil {
		return "", err
	}
	if err = temporary.Sync(); err != nil {
		return "", err
	}
	if err = temporary.Close(); err != nil {
		return "", err
	}
	return name, nil
}

// syncDir flushes a directory entry so renames and removals survive a crash.
// EINVAL is tolerated for filesystems that do not support directory fsync.
func syncDir(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil && !errors.Is(err, syscall.EINVAL) {
		return err
	}
	return nil
}
