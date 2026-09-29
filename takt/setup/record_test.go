package setup

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRecordReplacementFailurePreservesPreviousState(t *testing.T) {
	for _, tc := range []struct {
		name     string
		filename string
		save     func(string) error
	}{
		{"installed config", InstalledConfigFilename, func(root string) error {
			return RecordInstallation(root, PlanRequest{})
		}},
		{"ownership manifest", OwnershipManifestFilename, NewOwnershipManifest().Save},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			destination := filepath.Join(root, tc.filename)
			prior := []byte("previous complete record\n")
			if err := os.WriteFile(destination, prior, 0o644); err != nil {
				t.Fatal(err)
			}
			original := renameFile
			t.Cleanup(func() { renameFile = original })
			failure := errors.New("injected rename failure")
			renameFile = func(string, string) error { return failure }
			if err := tc.save(root); !errors.Is(err, failure) {
				t.Fatalf("save error = %v, want rename failure", err)
			}
			got, err := os.ReadFile(destination)
			if err != nil || string(got) != string(prior) {
				t.Fatalf("prior record changed: %q, %v", got, err)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 1 || entries[0].Name() != tc.filename {
				t.Fatalf("staging files leaked after failed replacement: %v, %v", entries, err)
			}
		})
	}
}
