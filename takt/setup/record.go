package setup

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// saveRecord atomically replaces one setup record, syncing its contents before
// rename and its directory afterward. A failed rename leaves the prior record.
func saveRecord(rootDir, filename, label string, record any) error {
	if strings.TrimSpace(rootDir) == "" {
		return fmt.Errorf("deployment root is required")
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", label, err)
	}
	destination := filepath.Join(rootDir, filename)
	directory := filepath.Dir(destination)
	if err := os.MkdirAll(directory, ManagedDirectoryMode); err != nil {
		return fmt.Errorf("prepare %s directory: %w", label, err)
	}
	staged, err := stageFile(directory, append(content, '\n'), ManagedFileMode)
	if err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	if err := renameFile(staged, destination); err != nil {
		_ = os.Remove(staged)
		return fmt.Errorf("write %s: %w", label, err)
	}
	if err := syncDir(directory); err != nil {
		return fmt.Errorf("write %s: %w", label, err)
	}
	return nil
}
