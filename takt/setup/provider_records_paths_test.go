package setup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shortActionTimeout keeps the timeout test fast while far exceeding process start-up jitter.
const shortActionTimeout = 200 * time.Millisecond

// longSleepSeconds is far beyond shortActionTimeout so the sleep can only end by cancellation.
const longSleepSeconds = "30"

func TestProviderActionFieldValidation(t *testing.T) {
	lookup := func(string) (string, error) { return "/stub", nil }
	tests := []struct {
		name    string
		action  ProviderAction
		wantErr string
	}{
		{"blank ID", ProviderAction{ID: " ", Program: "p"}, "ID is required"},
		{"padded ID", ProviderAction{ID: " a", Program: "p"}, "is invalid"},
		{"NUL in ID", ProviderAction{ID: "a\x00", Program: "p"}, "is invalid"},
		{"blank program", ProviderAction{ID: "a", Program: ""}, "program is required"},
		{"padded program", ProviderAction{ID: "a", Program: "p "}, "program \"p \" is invalid"},
		{"NUL in argument", ProviderAction{ID: "a", Program: "p", Args: []string{"ok", "b\x00"}}, "invalid argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plans := []TargetPlan{{Actions: []ProviderAction{tt.action}}}
			_, err := preflightProviderActions(plans, ProviderRuntime{LookPath: lookup})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestExecuteProviderActionsReportsProgressAndFailureDetail(t *testing.T) {
	var events []DeploymentProgress
	progress := func(event DeploymentProgress) { events = append(events, event) }
	failure := errors.New("exit status 3")
	runtime := ProviderRuntime{Run: func(_ context.Context, program string, _ ...string) ([]byte, error) {
		if program == "bad" {
			return []byte("  boom detail\n"), failure
		}
		return nil, nil
	}}
	actions := []ProviderAction{{ID: "one", Program: "good"}, {ID: "two", Program: "bad"}, {ID: "three", Program: "good"}}

	result, err := executeProviderActions(DeploymentResult{}, actions, runtime, progress)
	if !errors.Is(err, failure) || !strings.Contains(err.Error(), `"two" failed`) || !strings.HasSuffix(err.Error(), ": boom detail") {
		t.Fatalf("error = %v, want wrapped failure with trimmed output", err)
	}
	if len(result.Actions) != 1 || result.Actions[0] != "one" {
		t.Errorf("Actions = %v, want only the completed action", result.Actions)
	}
	if len(events) != 2 || events[1].Path != "two" || events[1].Completed != 1 || events[1].Total != len(actions) {
		t.Errorf("progress events = %+v", events)
	}
}

func TestExecuteProviderActionsFailureWithoutOutput(t *testing.T) {
	runtime := ProviderRuntime{Run: func(context.Context, string, ...string) ([]byte, error) { return []byte(" \n"), errors.New("nope") }}
	_, err := executeProviderActions(DeploymentResult{}, []ProviderAction{{ID: "a", Program: "p"}}, runtime, nil)
	if err == nil || err.Error() != `provider action "a" failed: nope` {
		t.Fatalf("error = %v", err)
	}
}

func TestRunProviderCommandCapturesOutputAndExitStatus(t *testing.T) {
	out, err := runProviderCommand(context.Background(), "sh", "-c", "echo out; echo err >&2; exit 3")
	if err == nil {
		t.Fatal("non-zero exit reported success")
	}
	if got := string(out); !strings.Contains(got, "out") || !strings.Contains(got, "err") {
		t.Errorf("output = %q, want stdout and stderr", got)
	}
}

func TestRunProviderCommandPrefersContextErrorOnTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), shortActionTimeout)
	defer cancel()
	_, err := runProviderCommand(ctx, "sleep", longSleepSeconds)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestBoundedOutputKeepsOnlyFirstBytes(t *testing.T) {
	const limit = 5
	output := &boundedOutput{remaining: limit}
	for _, chunk := range []string{"abc", "defgh", "ij"} {
		if n, err := output.Write([]byte(chunk)); n != len(chunk) || err != nil {
			t.Fatalf("Write(%q) = %d, %v; want full acceptance", chunk, n, err)
		}
	}
	if string(output.bytes) != "abcde" {
		t.Errorf("kept %q, want abcde", output.bytes)
	}
}

func TestRestoreRejectsBadEntries(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, BackupDir, "b"), "old")
	tests := map[string]struct {
		entry   OwnershipEntry
		wantErr string
	}{
		"invalid managed path": {OwnershipEntry{Path: "../a", BackupPath: BackupDir + "/b"}, "invalid managed path"},
		"invalid backup path":  {OwnershipEntry{Path: "a", BackupPath: "../b"}, "invalid backup path"},
		"backup through link":  {OwnershipEntry{Path: "a", BackupPath: "link/b"}, "resolve backup"},
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			err := restoreEntry(root, tt.entry)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}

	t.Run("destination through link", func(t *testing.T) {
		err := restoreEntry(root, OwnershipEntry{Path: "link/a", BackupPath: BackupDir + "/b"})
		if err == nil || !strings.Contains(err.Error(), "resolve destination") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("destination directory blocked", func(t *testing.T) {
		writeFile(t, filepath.Join(root, "file"), "x")
		err := restoreEntry(root, OwnershipEntry{Path: "file/a", BackupPath: BackupDir + "/b"})
		if err == nil || !strings.Contains(err.Error(), "prepare destination") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("rename failure removes staged file", func(t *testing.T) {
		original := renameFile
		t.Cleanup(func() { renameFile = original })
		renameFile = func(string, string) error { return errors.New("injected") }
		err := restoreEntry(root, OwnershipEntry{Path: "restored.txt", BackupPath: BackupDir + "/b"})
		if err == nil || !strings.Contains(err.Error(), `restore "restored.txt"`) {
			t.Fatalf("error = %v", err)
		}
		assertNoDeploymentTemps(t, root)
	})
	t.Run("defaults the mode when unrecorded", func(t *testing.T) {
		if err := restoreEntry(root, OwnershipEntry{Path: "defaulted.txt", BackupPath: BackupDir + "/b"}); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(filepath.Join(root, "defaulted.txt"))
		if err != nil || info.Mode().Perm() != ManagedFileMode {
			t.Errorf("mode = %v, %v; want %v", info.Mode().Perm(), err, ManagedFileMode)
		}
	})
}

func TestRestorePropagatesEntryFailure(t *testing.T) {
	manifest := NewOwnershipManifest()
	manifest.Entries["a"] = OwnershipEntry{Path: "a", BackupPath: BackupDir + "/missing"}
	if _, err := Restore(t.TempDir(), manifest); err == nil || !strings.Contains(err.Error(), "read backup") {
		t.Fatalf("error = %v", err)
	}
}

func TestSaveRecordFailures(t *testing.T) {
	if err := saveRecord(" ", "f.json", "thing", struct{}{}); err == nil || !strings.Contains(err.Error(), "deployment root is required") {
		t.Errorf("blank root error = %v", err)
	}
	if err := saveRecord(t.TempDir(), "f.json", "thing", make(chan int)); err == nil || !strings.Contains(err.Error(), "marshal thing") {
		t.Errorf("unmarshalable record error = %v", err)
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "blocker"), "file")
	if err := saveRecord(filepath.Join(dir, "blocker", "sub"), "f.json", "thing", struct{}{}); err == nil || !strings.Contains(err.Error(), "prepare thing directory") {
		t.Errorf("blocked directory error = %v", err)
	}
	t.Run("staging in read-only root", func(t *testing.T) {
		skipIfRoot(t)
		root := t.TempDir()
		if err := os.Chmod(root, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
		if err := saveRecord(root, "f.json", "thing", struct{}{}); err == nil || !strings.Contains(err.Error(), "write thing") {
			t.Errorf("error = %v", err)
		}
	})
}

func TestRiskAcceptanceRecordIsUpsertedAndSurvivesCorruption(t *testing.T) {
	root := t.TempDir()
	first := RiskAcceptance{Path: "b", SHA256: "1", Impact: ImpactUncertain}
	if err := RecordRiskAcceptances(root, []RiskAcceptance{first, {Path: "a", SHA256: "2", Impact: ImpactUncertain}}); err != nil {
		t.Fatal(err)
	}
	updated := RiskAcceptance{Path: "b", SHA256: "3", Impact: ImpactUncertain}
	if err := RecordRiskAcceptances(root, []RiskAcceptance{updated}); err != nil {
		t.Fatal(err)
	}
	loaded := loadRiskAcceptances(root)
	if len(loaded) != 2 || loaded["b"] != updated {
		t.Errorf("loaded = %+v, want b replaced and a kept", loaded)
	}
	writeFile(t, filepath.Join(root, RiskAcceptanceFilename), "not json")
	if got := loadRiskAcceptances(root); len(got) != 0 {
		t.Errorf("corrupt record yielded %+v, want nothing accepted", got)
	}
}

func TestRiskAcceptanceWriteFailures(t *testing.T) {
	if err := RecordRiskAcceptances(filepath.Join(t.TempDir(), "absent"), []RiskAcceptance{{Path: "a"}}); err == nil || !strings.Contains(err.Error(), "write accepted risks") {
		t.Errorf("missing root error = %v", err)
	}
	root := t.TempDir()
	original := renameFile
	t.Cleanup(func() { renameFile = original })
	renameFile = func(string, string) error { return errors.New("injected") }
	if err := RecordRiskAcceptances(root, []RiskAcceptance{{Path: "a"}}); err == nil || !strings.Contains(err.Error(), "write accepted risks") {
		t.Errorf("rename failure error = %v", err)
	}
	assertNoDeploymentTemps(t, root)
}

func TestRestoreEntryFailsWhenDestinationDirectoryIsReadOnly(t *testing.T) {
	skipIfRoot(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, BackupDir, "b"), "old")
	locked := filepath.Join(root, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	err := restoreEntry(root, OwnershipEntry{Path: "locked/a.txt", BackupPath: BackupDir + "/b"})
	if err == nil || !strings.Contains(err.Error(), "stage restore") {
		t.Fatalf("error = %v", err)
	}
}
