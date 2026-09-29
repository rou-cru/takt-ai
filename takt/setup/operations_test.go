package setup

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

// #12693: an invalid ownership target must be rejected before any artifact is
// written, so no files or manifest are left on disk.
func TestApplyRejectsInvalidTargetBeforeDeploy(t *testing.T) {
	root := t.TempDir()
	plans := []TargetPlan{{
		Target:       "cursor",
		ManagedPaths: []string{"evil.txt"},
		Artifacts:    []Artifact{{Path: "evil.txt", Content: []byte("must not write")}},
	}}
	_, err := ApplyContext(context.Background(), root, plans, ProviderRuntime{})
	if err == nil || !strings.Contains(err.Error(), "unsupported target") {
		t.Fatalf("Apply() error = %v, want unsupported target", err)
	}
	entries, readErr := os.ReadDir(root)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("rejected deployment wrote files: %v", entries)
	}
	_, manifestErr := LoadOwnershipManifest(root)
	if !errors.Is(manifestErr, os.ErrNotExist) {
		t.Errorf("manifest load error = %v, want absent after rejected deploy", manifestErr)
	}
}
