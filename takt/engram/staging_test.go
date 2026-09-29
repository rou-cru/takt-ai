package engram

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicExecutableAndCleanup(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "engram")
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(destination, []byte("new")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "new" {
		t.Fatalf("content=%q err=%v", data, err)
	}
	info, err := os.Stat(destination)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode=%v", info.Mode())
	}
	blocked := filepath.Join(dir, "directory")
	if err := os.Mkdir(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomic(blocked, []byte("new")); err == nil {
		t.Fatal("rename to directory succeeded")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("abandoned temp: %v, %v", entries, err)
	}
}
