package setup

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestApplyUninstallRetentionWithoutChoicesIsNoOp(t *testing.T) {
	root := t.TempDir()
	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{}, time.Now())
	if err != nil || result.RetainedDir != "" {
		t.Fatalf("result = %+v, err = %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, RetainedDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retained dir created without choices: %v", err)
	}
}

// engramFixture builds an Engram-shaped data directory: a WAL-mode database
// whose latest rows still sit in the -wal file because the writer stays open.
// The returned data directory lives under the test's temp dir only.
func engramFixture(t *testing.T) string {
	t.Helper()
	dataDir := filepath.Join(t.TempDir(), "engram-data")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dataDir, EngramDatabaseName))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA wal_autocheckpoint=0",
		"CREATE TABLE observations (id INTEGER PRIMARY KEY, title TEXT)",
		"INSERT INTO observations (title) VALUES ('alpha decision'), ('beta bug')",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	if info, err := os.Stat(filepath.Join(dataDir, EngramDatabaseName+"-wal")); err != nil || info.Size() == 0 {
		t.Fatalf("fixture WAL = %v, %v; want committed rows still in the log", info, err)
	}
	return dataDir
}

func readTitles(t *testing.T, path string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT title FROM observations ORDER BY id")
	if err != nil {
		t.Fatalf("query delivered memory: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		titles = append(titles, title)
	}
	return titles
}

func TestApplyUninstallRetentionDeliversMemoryToOneDirectory(t *testing.T) {
	root := t.TempDir()
	dataDir := engramFixture(t)
	now := time.Date(2026, 9, 12, 10, 30, 0, 0, time.UTC)

	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{MemoryFrom: dataDir}, now)
	if err != nil {
		t.Fatalf("ApplyUninstallRetention() error = %v", err)
	}

	wantDir := filepath.Join(root, RetainedDirName, "20260912T103000Z")
	wantFile := filepath.Join(wantDir, "engram", EngramDatabaseName)
	if result.RetainedDir != wantDir || result.Memory != wantFile || len(result.Incomplete) != 0 {
		t.Fatalf("result = %+v, want memory at %s with no incomplete work", result, wantFile)
	}
	if got := readTitles(t, wantFile); !slices.Equal(got, []string{"alpha decision", "beta bug"}) {
		t.Errorf("delivered rows = %v, want the fixture rows including those still in the source WAL", got)
	}
	entries, _ := os.ReadDir(filepath.Join(wantDir, "engram"))
	if len(entries) != 1 {
		t.Errorf("delivered directory holds %d entries, want the single database file", len(entries))
	}
	if got := readTitles(t, filepath.Join(dataDir, EngramDatabaseName)); len(got) != 2 {
		t.Errorf("source rows = %v, want the live database untouched", got)
	}
	if topLevel, _ := os.ReadDir(root); len(topLevel) != 1 || topLevel[0].Name() != RetainedDirName {
		t.Errorf("deployment root = %v, want only %s: the handoff stays outside every managed path", topLevel, RetainedDirName)
	}
}

func TestApplyUninstallRetentionWithoutMemoryChoiceLeavesNothing(t *testing.T) {
	root := t.TempDir()
	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{}, time.Now())
	if err != nil || result.Memory != "" || result.RetainedDir != "" || len(result.Incomplete) != 0 {
		t.Fatalf("result = %+v, %v; want no work", result, err)
	}
	if entries, _ := os.ReadDir(root); len(entries) != 0 {
		t.Errorf("deployment root = %v, want it untouched", entries)
	}
}

func TestApplyUninstallRetentionMemoryWithoutDatabaseHasNothingToRetain(t *testing.T) {
	root := t.TempDir()
	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{MemoryFrom: t.TempDir()}, time.Now())
	if err != nil || result.Memory != "" || len(result.Incomplete) != 0 {
		t.Fatalf("result = %+v, %v; want a quiet no-op", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, RetainedDirName)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("retained directory created for nothing: %v", err)
	}
}

func TestApplyUninstallRetentionUnreadableMemoryIsIncompleteWithoutPartialCopy(t *testing.T) {
	root := t.TempDir()
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, EngramDatabaseName), []byte("not a database, just text padding padding padding padding padding padding"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{MemoryFrom: dataDir}, time.Now())
	if err != nil {
		t.Fatalf("error = %v; a failed handoff is incomplete work, not a failure", err)
	}
	if result.Memory != "" || len(result.Incomplete) != 1 || !strings.HasPrefix(result.Incomplete[0], "Engram memory:") {
		t.Fatalf("result = %+v, want one Engram memory incomplete entry", result)
	}
	if got, _ := os.ReadFile(filepath.Join(dataDir, EngramDatabaseName)); !strings.HasPrefix(string(got), "not a database") {
		t.Error("source was modified")
	}
	if matches, _ := filepath.Glob(filepath.Join(root, RetainedDirName, "*", "engram", "*")); len(matches) != 0 {
		t.Errorf("partial copy left behind: %v", matches)
	}
}

func TestApplyUninstallRetentionCancelledMemoryIsNotApplied(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	root := t.TempDir()
	result, err := ApplyUninstallRetention(ctx, root, RetentionChoices{MemoryFrom: engramFixture(t)}, time.Now())
	if err == nil || result.Memory != "" || !slices.Equal(result.NotApplied, []string{"Engram memory"}) {
		t.Fatalf("result = %+v, %v; want the memory choice reported as not applied", result, err)
	}
}

func TestEngramDataDirFollowsEngramResolution(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ENGRAM_DATA_DIR", "")
	if got, err := EngramDataDir(); err != nil || got != filepath.Join(home, ".engram") {
		t.Errorf("default = %q, %v; want ~/.engram", got, err)
	}
	override := t.TempDir()
	t.Setenv("ENGRAM_DATA_DIR", override)
	if got, err := EngramDataDir(); err != nil || got != override {
		t.Errorf("override = %q, %v; want %q", got, err, override)
	}
}

func TestApplyUninstallRetentionTimestampCollisionPreservesSnapshots(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(fmt.Sprintf("corrupt-second-%v", corrupt), func(t *testing.T) {
			root, source := t.TempDir(), engramFixture(t)
			now := time.Date(2026, 9, 12, 10, 30, 0, 0, time.UTC)
			first, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{MemoryFrom: source}, now)
			if err != nil || first.Memory == "" {
				t.Fatalf("first = %+v, %v", first, err)
			}
			original, err := os.ReadFile(first.Memory)
			if err != nil {
				t.Fatal(err)
			}
			if corrupt {
				source = t.TempDir()
				if err := os.WriteFile(filepath.Join(source, EngramDatabaseName), []byte(strings.Repeat("corrupt", 100)), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			second, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{MemoryFrom: source}, now)
			if err != nil {
				t.Fatal(err)
			}
			if corrupt {
				if len(second.Incomplete) != 1 || second.Memory != "" {
					t.Fatalf("second = %+v", second)
				}
				if _, err := os.Stat(filepath.Join(first.RetainedDir+"-1", retainedMemoryDir, EngramDatabaseName)); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("partial snapshot remains: %v", err)
				}
			} else {
				if second.RetainedDir != first.RetainedDir+"-1" || second.Memory == first.Memory || len(second.Incomplete) != 0 {
					t.Fatalf("first = %+v, second = %+v", first, second)
				}
				if got := readTitles(t, second.Memory); !slices.Equal(got, []string{"alpha decision", "beta bug"}) {
					t.Fatalf("second rows = %v", got)
				}
			}
			current, err := os.ReadFile(first.Memory)
			if err != nil || !bytes.Equal(current, original) {
				t.Fatalf("first snapshot changed: %v", err)
			}
			if got := readTitles(t, first.Memory); !slices.Equal(got, []string{"alpha decision", "beta bug"}) {
				t.Fatalf("first rows = %v", got)
			}
		})
	}
}

func TestSnapshotDatabasePreservesExistingDestination(t *testing.T) {
	for _, content := range []string{"", "previous retained content"} {
		t.Run(fmt.Sprintf("size-%d", len(content)), func(t *testing.T) {
			source := filepath.Join(engramFixture(t), EngramDatabaseName)
			dir := t.TempDir()
			destination := filepath.Join(dir, retainedMemoryDir, EngramDatabaseName)
			if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(destination, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := snapshotDatabase(context.Background(), source, destination); !os.IsExist(err) {
				t.Fatalf("error = %v, want exclusive-create conflict", err)
			}
			result := RetentionResult{}
			retainMemory(context.Background(), filepath.Dir(source), &retentionDirectory{path: dir}, &result)
			if len(result.Incomplete) != 1 || result.Memory != "" {
				t.Fatalf("retention conflict = %+v", result)
			}
			got, err := os.ReadFile(destination)
			if err != nil || string(got) != content {
				t.Fatalf("existing destination changed: %q, %v", got, err)
			}
		})
	}
}

func TestSnapshotDatabaseCancelledRemovesOnlyReservedFile(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	destination := filepath.Join(t.TempDir(), EngramDatabaseName)
	if err := snapshotDatabase(ctx, filepath.Join(engramFixture(t), EngramDatabaseName), destination); err == nil {
		t.Fatal("cancelled snapshot succeeded")
	}
	if _, err := os.Stat(destination); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reserved file remains: %v", err)
	}
}

func TestRetentionDirectorySkipsEveryExistingCandidate(t *testing.T) {
	base := filepath.Join(t.TempDir(), "timestamp")
	if err := os.Mkdir(base, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+"-1", []byte("existing file"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := retentionDirectory{base: base}
	for range 2 {
		got, err := dir.reserve()
		if err != nil || got != base+"-2" {
			t.Fatalf("reserve = %q, %v", got, err)
		}
	}
	if got, err := os.ReadFile(base + "-1"); err != nil || string(got) != "existing file" {
		t.Fatalf("collision changed: %q, %v", got, err)
	}
}
