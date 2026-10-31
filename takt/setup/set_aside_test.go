package setup_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		file := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, file string) string {
	t.Helper()
	content, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestSetAsideMovesADirectoryIntoTheBackupsAndRestoresIt(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"tools/bin/run": "old", "tools/lib/a": "lib"})

	entry, err := setup.SetAside(root, "tools")
	if err != nil {
		t.Fatalf("SetAside() error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "tools")); !os.IsNotExist(err) {
		t.Fatalf("tools still present after SetAside: %v", err)
	}
	want := setup.Backup{Path: "tools", Copy: setup.BackupDir + "/tools"}
	if got := entry.Backup(); got != want {
		t.Fatalf("Backup() = %+v, want %+v", got, want)
	}
	if got := readFile(t, filepath.Join(root, setup.BackupDir, "tools", "bin", "run")); got != "old" {
		t.Fatalf("backup content = %q, want %q", got, "old")
	}

	// A partial replacement occupies the path when the restore runs.
	writeTree(t, root, map[string]string{"tools/bin/run": "partial"})
	if err := entry.Restore(); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	if got := readFile(t, filepath.Join(root, "tools", "bin", "run")); got != "old" {
		t.Fatalf("restored content = %q, want %q", got, "old")
	}
	if got := readFile(t, filepath.Join(root, "tools", "lib", "a")); got != "lib" {
		t.Fatalf("restored lib = %q, want %q", got, "lib")
	}
	if _, err := os.Lstat(filepath.Join(root, setup.BackupDir, "tools")); !os.IsNotExist(err) {
		t.Fatalf("backup copy still present after Restore: %v", err)
	}
}

func TestSetAsideKeepsAnEarlierBackup(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"bin/tool":                    "new",
		setup.BackupDir + "/bin/tool": "earlier",
	})

	entry, err := setup.SetAside(root, "bin/tool")
	if err != nil {
		t.Fatalf("SetAside() error = %v", err)
	}
	copyRel := entry.Backup().Copy
	if !strings.HasPrefix(copyRel, setup.BackupDir+"/bin/tool.") {
		t.Fatalf("Copy = %q, want a suffixed path beside the earlier backup", copyRel)
	}
	if got := readFile(t, filepath.Join(root, setup.BackupDir, "bin", "tool")); got != "earlier" {
		t.Fatalf("earlier backup = %q, want it unchanged", got)
	}
	if got := readFile(t, filepath.Join(root, filepath.FromSlash(copyRel))); got != "new" {
		t.Fatalf("new backup = %q, want %q", got, "new")
	}
}

func TestSetAsideRefusesAPathOutsideTheRoot(t *testing.T) {
	root := t.TempDir()
	if _, err := setup.SetAside(root, "../escape"); err == nil {
		t.Fatal("SetAside(../escape) error = nil, want a refusal")
	}
}
