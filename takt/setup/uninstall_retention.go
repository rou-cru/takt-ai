package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Retention modes keep uninstall evidence owner-only.
const (
	// RetainedDirectoryMode protects directories containing uninstall evidence.
	RetainedDirectoryMode os.FileMode = 0o700
	// RetainedFileMode protects retained uninstall evidence files.
	RetainedFileMode os.FileMode = 0o600
)

// RetainedDirName is the directory under the deployment root that receives content kept during uninstall.
// It is not a harness configuration directory and Takt never records it as managed.
const RetainedDirName = "takt-retained"

// RetentionChoices are the user's explicit decisions for externally modified
// managed files that Uninstall left in place. Paths not listed stay where Uninstall left them.
type RetentionChoices struct {
	// Keep lists preserved files to move into the retained directory.
	Keep []string
	// Remove lists preserved files to delete.
	Remove []string
	// RetainedDir is a retained directory already reserved for this uninstall
	// (ReserveRetentionDir), which kept files share. Empty reserves one on first use.
	RetainedDir string
}

// RetainedMemoryDirName is the directory inside the retained directory that receives the memory backup.
const RetainedMemoryDirName = "memory"

// ReserveRetentionDir reserves the timestamped retained directory for one uninstall, owner-only, and
// returns its path.
func ReserveRetentionDir(rootDir string, now time.Time) (string, error) {
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return "", fmt.Errorf("resolve deployment root: %w", err)
	}
	return newRetentionDirectory(root, now).reserve()
}

func newRetentionDirectory(root string, now time.Time) *retentionDirectory {
	return &retentionDirectory{base: filepath.Join(root, RetainedDirName, now.UTC().Format("20060102T150405Z"))}
}

// RetentionResult reports how RetentionChoices were carried out. Deliberate
// retention (Retained) is kept apart from cleanup that did not complete
// (Incomplete) so neither is relabeled as the other.
type RetentionResult struct {
	// Removed lists deleted paths.
	Removed []string
	// Retained lists paths moved into the retained directory.
	Retained []string
	// RetainedDir is the timestamped directory holding kept content.
	RetainedDir string
	// Incomplete holds "path: reason" for every choice that could not be
	// carried out; the file stays where it was.
	Incomplete []string
	// NotApplied holds the choices a cancellation left untouched in place.
	NotApplied []string
}

// restorablePreExisting reports whether uninstall can restore a pre-existing file from its backup.
// The backup must exist and the file must still hold exactly what Takt recorded.
// A file edited since keeps its current content, Takt contributions included, so user changes survive.
func restorablePreExisting(root string, entry OwnershipEntry) bool {
	if entry.BackupPath == "" {
		return false
	}
	backup, err := SafeJoin(root, filepath.FromSlash(entry.BackupPath))
	if err != nil {
		return false
	}
	if _, err := os.Stat(backup); err != nil {
		return false
	}
	outcome, err := classifyRemoval(root, uninstallPlanEntry{path: entry.Path, entry: entry})
	return err == nil && outcome == removalClean
}

// ApplyUninstallRetention carries out keep/remove choices after Uninstall,
// moving retained files into one timestamped directory.
func ApplyUninstallRetention(ctx context.Context, rootDir string, choices RetentionChoices, now time.Time) (RetentionResult, error) {
	result := RetentionResult{}
	if len(choices.Keep) == 0 && len(choices.Remove) == 0 {
		return result, nil
	}
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return result, fmt.Errorf("resolve deployment root: %w", err)
	}
	retainedDir := newRetentionDirectory(root, now)
	retainedDir.path = choices.RetainedDir
	if err := applyFileChoices(ctx, root, retainedDir, choices, &result); err != nil {
		return result, err
	}
	if len(result.NotApplied) > 0 {
		return result, ctx.Err()
	}
	return result, nil
}

// applyFileChoices moves kept files into retainedDir, deletes removed ones and
// rewrites the manifest.
func applyFileChoices(ctx context.Context, root string, retainedDir *retentionDirectory, choices RetentionChoices, result *RetentionResult) error {
	manifest, err := LoadOwnershipManifest(root)
	if err != nil {
		return err
	}
	pending := append(append([]string(nil), choices.Remove...), choices.Keep...)
	for index, rel := range pending {
		if ctx.Err() != nil {
			result.NotApplied = pending[index:]
			break
		}
		keep := index >= len(choices.Remove)
		if err := retainOrRemove(root, retainedDir, manifest, rel, keep); err != nil {
			result.Incomplete = append(result.Incomplete, rel+": "+err.Error())
			continue
		}
		if !keep {
			result.Removed = append(result.Removed, rel)
			continue
		}
		result.Retained = append(result.Retained, rel)
		result.RetainedDir = retainedDir.path
	}
	_, err = finishUninstall(manifest, root, UninstallResult{})
	return err
}

// retainOrRemove moves rel into retainedDir, or deletes it, and drops its
// manifest entry.
func retainOrRemove(root string, retainedDir *retentionDirectory, manifest *OwnershipManifest, rel string, keep bool) error {
	if _, ok := manifest.Entries[rel]; !ok {
		return fmt.Errorf("no longer recorded as managed")
	}
	source, err := SafeJoin(root, rel)
	if err != nil {
		return err
	}
	if keep {
		if _, err := os.Lstat(source); err != nil {
			return err
		}
		dir, reserveErr := retainedDir.reserve()
		if reserveErr != nil {
			return reserveErr
		}
		err = retainFile(source, filepath.Join(dir, filepath.FromSlash(rel)))
	} else if err = os.Remove(source); errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	if err != nil {
		return err
	}
	delete(manifest.Entries, rel)
	pruneEmptyDirs(root, filepath.Dir(source))
	return nil
}

func retainFile(source, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), ManagedDirectoryMode); err != nil {
		return err
	}
	// Same-root rename is safe; add copy+remove if the retained dir ever moves to another filesystem.
	return renameFile(source, destination)
}

// retentionDirectory lazily reserves one exclusive directory for this operation.
type retentionDirectory struct {
	base string
	path string
}

func (d *retentionDirectory) reserve() (string, error) {
	if d.path != "" {
		return d.path, nil
	}
	parent := filepath.Dir(d.base)
	if err := os.MkdirAll(parent, RetainedDirectoryMode); err != nil {
		return "", err
	}
	// A planted takt-retained symlink would move retained files outside the root.
	if info, err := os.Lstat(parent); err != nil {
		return "", err
	} else if !info.IsDir() {
		return "", fmt.Errorf("retention directory %q is not a real directory", parent)
	}
	for suffix := 0; ; suffix++ {
		candidate := d.base
		if suffix > 0 {
			candidate = fmt.Sprintf("%s-%d", d.base, suffix)
		}
		if err := os.Mkdir(candidate, RetainedDirectoryMode); err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", err
		}
		d.path = candidate
		return candidate, nil
	}
}
