package memory_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
)

func TestCapabilitiesWithoutUsableWorkspaceKeepRecording(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, directory := range map[string]string{
		"missing directory": filepath.Join(t.TempDir(), "absent"),
		"regular file":      file,
	} {
		got := memory.Capabilities(memory.CapabilitiesRequest{Author: "architect", Session: "root", Directory: directory})
		if !slices.Equal(got, []string{"memory_record"}) {
			t.Fatalf("%s: got %v, want only memory_record", name, got)
		}
	}
}

// TestCapabilitiesFollowTheHoldingSession: lifecycle tools belong to the root
// session and to the exact session the history records as the interlocutor
// holder, not to another session of the holder's agent.
func TestCapabilitiesFollowTheHoldingSession(t *testing.T) {
	dir := t.TempDir()
	h, err := history.Open(history.StateDir(dir))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Append(history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched,
		SessionID: "root", WorkUnitID: "holder", AttemptID: history.FirstAttempt,
		Cause: history.CauseNone, Agent: "architect", Artifact: "artifacts/report.md",
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	lifecycle := []string{"memory_record", "memory_continue_session", "memory_close_session"}
	for caller, want := range map[string][]string{"holder": lifecycle, "root": lifecycle, "sibling": {"memory_record"}} {
		got := memory.Capabilities(memory.CapabilitiesRequest{Author: "architect", Session: "root", Directory: dir, CallerSession: caller})
		if !slices.Equal(got, want) {
			t.Errorf("caller %s: got %v, want %v", caller, got, want)
		}
	}
}
