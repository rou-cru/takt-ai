package history_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

func TestStateDirIsThePrivateVFSDirectory(t *testing.T) {
	if got, want := history.StateDir("ws"), filepath.Join("ws", ".takt-ai", "vfs"); got != want {
		t.Fatalf("StateDir = %q, want %q", got, want)
	}
}

func TestReadProjectionOfAbsentHistoryCreatesNothing(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state")
	got, err := history.ReadProjection(state)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, history.Project(nil)) {
		t.Fatalf("absent history projected %v, want empty", got)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("ReadProjection created the state directory: %v", err)
	}
}

func TestReadProjectionMatchesOpenedHistory(t *testing.T) {
	state := t.TempDir()
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched, SessionID: "root", WorkUnitID: "child", AttemptID: history.FirstAttempt, Cause: history.CauseNone, Agent: "architect", Artifact: "artifacts/report.md"}); err != nil {
		t.Fatal(err)
	}
	want := h.Project()
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := history.ReadProjection(state)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReadProjection = %v, want %v", got, want)
	}
}

func TestReadProjectionFailsOnUnreadableHistory(t *testing.T) {
	foreign := t.TempDir()
	body := strings.Repeat(foreignFileBody, foreignFileReps)
	if err := os.WriteFile(filepath.Join(foreign, storeFile), []byte(body), privateFileMode); err != nil {
		t.Fatal(err)
	}
	notDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(notDir, "file"), []byte("x"), privateFileMode); err != nil {
		t.Fatal(err)
	}
	for name, state := range map[string]string{
		"foreign store":    foreign,
		"state under file": filepath.Join(notDir, "file", "state"),
	} {
		if _, err := history.ReadProjection(state); err == nil {
			t.Fatalf("%s: ReadProjection succeeded", name)
		}
	}
}
