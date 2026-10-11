package session_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/session"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func TestBrokenTelemetryCannotPreventGovernedWork(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, ".takt-ai"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".takt-ai", "events.db"), []byte("broken database"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := session.NewDurable(root, filepath.Join(t.TempDir(), "private"), "session-1")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := s.FS.Close(); err != nil {
			t.Error(err)
		}
	}()
	defer func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	}()
	key := bind(t, s, "dev", "unit", "dev", "a")
	if _, err := s.FS.Apply(vfs.Operation{Key: key, CallID: "stage", Action: vfs.OpCreate, Path: "a", Content: []byte("candidate")}); err != nil {
		t.Fatal(err)
	}
	if len(s.FS.JournalPage("session-1", "unit", -1, 100)) != 1 {
		t.Fatal("authoritative journal lost")
	}
	if _, err := s.FS.Apply(vfs.Operation{Key: key, CallID: "outside", ExpectedRevision: 1, Action: vfs.OpCreate, Path: "outside", Content: []byte("bypass")}); !errors.Is(err, vfs.ErrScopeDenied) {
		t.Fatalf("telemetry outage widened authority: %v", err)
	}
}
