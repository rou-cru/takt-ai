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

func TestReadPageFiltersAndBoundsTheRecordedOrder(t *testing.T) {
	state := t.TempDir()
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, session := range []string{"root-a", "root-b", "root-a"} {
		if err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched, SessionID: session, WorkUnitID: "child", AttemptID: history.FirstAttempt, Cause: history.CauseNone, Agent: "architect", Artifact: "artifacts/report.md"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	all, err := history.ReadPage(state, "", "", -1, 10)
	if err != nil || len(all) != 3 {
		t.Fatalf("ReadPage = %+v, %v; want 3 entries", all, err)
	}
	if got, _ := history.ReadPage(state, "root-a", "", -1, 10); len(got) != 2 {
		t.Fatalf("session filter = %+v", got)
	}
	if got, _ := history.ReadPage(state, "", "", all[0].Seq, 1); len(got) != 1 || got[0].Seq != all[1].Seq {
		t.Fatalf("cursor and limit = %+v", got)
	}
	if got, err := history.ReadPage(filepath.Join(t.TempDir(), "absent"), "", "", -1, 10); err != nil || len(got) != 0 {
		t.Fatalf("absent history = %+v, %v", got, err)
	}
}
