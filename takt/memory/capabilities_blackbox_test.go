package memory_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

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
