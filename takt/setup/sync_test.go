package setup_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestSyncContextRedeploysChangedContent(t *testing.T) {
	root := t.TempDir()
	if _, err := setup.ApplyContext(context.Background(), root, simplePlan("hello"), setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}

	result, err := setup.SyncContext(context.Background(), root, simplePlan("updated content"), setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("SyncContext() error = %v", err)
	}
	if len(result.Changed) != 1 || result.Changed[0] != "a.txt" {
		t.Fatalf("SyncContext() Changed = %v, want [a.txt]", result.Changed)
	}
	content, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(content) != "updated content" {
		t.Errorf("a.txt content = %q, %v, want the synced content", content, err)
	}
}

func TestSyncContextPreservesLocallyEditedFile(t *testing.T) {
	root := t.TempDir()
	if _, err := setup.ApplyContext(context.Background(), root, simplePlan("hello"), setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("user edited this locally"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	result, err := setup.SyncContext(context.Background(), root, simplePlan("updated content"), setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("SyncContext() error = %v", err)
	}
	if len(result.Changed) != 0 {
		t.Errorf("SyncContext() Changed = %v, want none: the file was edited locally", result.Changed)
	}
	content, err := os.ReadFile(filepath.Join(root, "a.txt"))
	if err != nil || string(content) != "user edited this locally" {
		t.Errorf("a.txt content = %q, %v, want the local edit preserved", content, err)
	}
}

func TestSyncContextRedeploysDeletedFile(t *testing.T) {
	root := t.TempDir()
	if _, err := setup.ApplyContext(context.Background(), root, simplePlan("hello"), setup.ProviderRuntime{}); err != nil {
		t.Fatalf("ApplyContext() error = %v", err)
	}
	if err := os.Remove(filepath.Join(root, "a.txt")); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	result, err := setup.SyncContext(context.Background(), root, simplePlan("hello"), setup.ProviderRuntime{})
	if err != nil {
		t.Fatalf("SyncContext() error = %v", err)
	}
	if len(result.Changed) != 1 || result.Changed[0] != "a.txt" {
		t.Errorf("SyncContext() Changed = %v, want [a.txt]: a deleted managed file is redeployed", result.Changed)
	}
}

func TestSyncContextRequiresRootDir(t *testing.T) {
	if _, err := setup.SyncContext(context.Background(), "", simplePlan("hello"), setup.ProviderRuntime{}); err == nil {
		t.Fatal("SyncContext() with an empty root error = nil, want an error")
	}
}
