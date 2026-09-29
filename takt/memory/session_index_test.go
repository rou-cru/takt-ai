package memory

import (
	"os"
	"testing"
)

func TestSessionIndexStagingPermissionsAndRenameFailure(t *testing.T) {
	root := t.TempDir()
	lock, err := lockSession(root, "session")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := lock.Close(); err != nil {
			t.Errorf("close session index lock: %v", err)
		}
	}()
	l := &sessionIndex{Session: "session", Entries: []sessionIndexEntry{}}
	if err := saveSessionIndex(root, l); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{sessionsDir(root): 0o700, sessionIndexPath(root, l.Session): 0o600} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != mode {
			t.Fatalf("%s mode=%v", path, info.Mode())
		}
	}
	l.Session = "directory"
	if err := os.Mkdir(sessionIndexPath(root, l.Session), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := saveSessionIndex(root, l); err == nil {
		t.Fatal("rename to directory succeeded")
	}
	entries, err := os.ReadDir(sessionsDir(root))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("abandoned temp: %v", entries)
	}
}
