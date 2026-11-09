package memory_test

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
	_ "modernc.org/sqlite"
)

func storeFile(dir string) string { return filepath.Join(dir, memory.StoreFile) }

func TestInitThenOpen(t *testing.T) {
	dir := t.TempDir()
	if err := memory.Init(dir); err != nil {
		t.Fatal(err)
	}
	s, err := memory.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInitRefusesExistingStore(t *testing.T) {
	dir := t.TempDir()
	if err := memory.Init(dir); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(storeFile(dir))
	if err := memory.Init(dir); !errors.Is(err, memory.ErrStoreExists) {
		t.Fatalf("got %v", err)
	}
	after, _ := os.ReadFile(storeFile(dir))
	if !bytes.Equal(before, after) {
		t.Fatal("existing store changed")
	}
}

func TestOpenWithoutStore(t *testing.T) {
	dir := t.TempDir()
	if _, err := memory.Open(dir); !errors.Is(err, memory.ErrNoStore) {
		t.Fatalf("got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("Open created files: %v", entries)
	}
}

func TestOpenRefusesNewerStore(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", storeFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE t (x); PRAGMA user_version = 999"); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	before, _ := os.ReadFile(storeFile(dir))
	if _, err := memory.Open(dir); !errors.Is(err, memory.ErrNewerStore) {
		t.Fatalf("got %v", err)
	}
	after, _ := os.ReadFile(storeFile(dir))
	if !bytes.Equal(before, after) {
		t.Fatal("newer store changed")
	}
}

func TestOpenReportsCorruptStore(t *testing.T) {
	dir := t.TempDir()
	garbage := bytes.Repeat([]byte("not a sqlite database "), 100)
	if err := os.WriteFile(storeFile(dir), garbage, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.Open(dir); !errors.Is(err, memory.ErrCorrupt) {
		t.Fatalf("got %v", err)
	}
	after, _ := os.ReadFile(storeFile(dir))
	if !bytes.Equal(garbage, after) {
		t.Fatal("corrupt store changed")
	}
}

func TestBackupIsOpenable(t *testing.T) {
	dir := t.TempDir()
	if err := memory.Init(dir); err != nil {
		t.Fatal(err)
	}
	s, err := memory.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.Close() }()
	dest := filepath.Join(t.TempDir(), "memory.db")
	path, size, err := s.Backup(t.Context(), dest)
	if err != nil || path != dest || size == 0 {
		t.Fatalf("got %q %d %v", path, size, err)
	}
	b, err := memory.Open(filepath.Dir(dest))
	if err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
}
