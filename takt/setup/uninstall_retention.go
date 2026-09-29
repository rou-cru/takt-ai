package setup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver used to snapshot Engram's database
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
	// MemoryFrom is the Engram data directory whose database is handed off
	// into the retained directory. Empty means no memory choice was made.
	MemoryFrom string
}

// EngramDataDirName is the default Engram data directory under the user's
// home; ENGRAM_DATA_DIR overrides it (`engram help`, Environment).
const EngramDataDirName = ".engram"

// EngramDatabaseName is the SQLite file Engram keeps inside its data directory.
const EngramDatabaseName = "engram.db"

// retainedMemoryDir holds the delivered Engram database inside the retained directory.
const retainedMemoryDir = "engram"

// EngramDataDir resolves where Engram keeps its data the way Engram does:
// ENGRAM_DATA_DIR when set, otherwise ~/.engram.
func EngramDataDir() (string, error) {
	if dir := os.Getenv("ENGRAM_DATA_DIR"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve Engram data directory: %w", err)
	}
	return filepath.Join(home, EngramDataDirName), nil
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
	// Memory is the delivered Engram database inside RetainedDir; empty when
	// no memory was handed off (no choice, or Engram holds no database).
	Memory string
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
// moving retained files and the Engram database into one timestamped
// directory.
func ApplyUninstallRetention(ctx context.Context, rootDir string, choices RetentionChoices, now time.Time) (RetentionResult, error) {
	result := RetentionResult{}
	if len(choices.Keep) == 0 && len(choices.Remove) == 0 && choices.MemoryFrom == "" {
		return result, nil
	}
	root, err := filepath.Abs(rootDir)
	if err != nil {
		return result, fmt.Errorf("resolve deployment root: %w", err)
	}
	retainedDir := &retentionDirectory{base: filepath.Join(root, RetainedDirName, now.UTC().Format("20060102T150405Z"))}
	if len(choices.Keep) > 0 || len(choices.Remove) > 0 {
		if err := applyFileChoices(ctx, root, retainedDir, choices, &result); err != nil {
			return result, err
		}
	}
	if choices.MemoryFrom != "" {
		retainMemory(ctx, choices.MemoryFrom, retainedDir, &result)
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

// memoryNotApplied names the Engram memory choice a cancellation left untouched.
const memoryNotApplied = "Engram memory"

// retainMemory hands the Engram database in dataDir off to retainedDir as one
// consistent file. The source is only read: it stays where it was, and a
// failure is reported as Incomplete with no partial copy left behind. A data
// directory without a database has nothing to retain.
func retainMemory(ctx context.Context, dataDir string, retainedDir *retentionDirectory, result *RetentionResult) {
	if ctx.Err() != nil {
		result.NotApplied = append(result.NotApplied, memoryNotApplied)
		return
	}
	source := filepath.Join(dataDir, EngramDatabaseName)
	if _, err := os.Stat(source); errors.Is(err, os.ErrNotExist) {
		return
	}
	dir, err := retainedDir.reserve()
	if err != nil {
		result.Incomplete = append(result.Incomplete, memoryNotApplied+": "+err.Error())
		return
	}
	destination := filepath.Join(dir, retainedMemoryDir, EngramDatabaseName)
	if err := snapshotDatabase(ctx, source, destination); err != nil {
		result.Incomplete = append(result.Incomplete, memoryNotApplied+": "+err.Error())
		return
	}
	result.Memory, result.RetainedDir = destination, dir
}

// snapshotDatabase writes a consistent single-file copy of a SQLite database
// with VACUUM INTO. Copying engram.db beside its -wal would miss committed
// pages still in the log and tear while Engram servers are running; VACUUM
// INTO reads through SQLite, so the snapshot is transactionally consistent
// even then. The source opens read-only and is never modified.
func snapshotDatabase(ctx context.Context, source, destination string) (err error) {
	if err := os.MkdirAll(filepath.Dir(destination), RetainedDirectoryMode); err != nil {
		return err
	}
	// Only this call's exclusively reserved file may be cleaned up on failure.
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, RetainedFileMode)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(destination)
		}
	}()
	if err = file.Close(); err != nil {
		return err
	}
	uri := url.URL{Scheme: "file", Path: source, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(ctx, "VACUUM INTO ?", destination); err != nil {
		return fmt.Errorf("snapshot %s: %w", source, err)
	}
	return nil
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
	if err := os.MkdirAll(filepath.Dir(d.base), RetainedDirectoryMode); err != nil {
		return "", err
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
