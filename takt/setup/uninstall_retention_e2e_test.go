package setup

import (
	"context"
	"database/sql"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/engram"
)

// TestE2ERetainedMemoryIsReadableFromRealEngramData is E2E-C: real Engram
// writes the data, a real `engram serve` keeps the database open with committed
// rows still in its WAL, and the delivered copy must open on its own.
// It runs only inside the disposable test container (development/testing/test-containerized.sh):
// it downloads the pinned Engram release and never touches a host data directory.
func TestE2ERetainedMemoryIsReadableFromRealEngramData(t *testing.T) {
	if _, err := os.Stat("/.dockerenv"); err != nil {
		t.Skip("E2E-C runs only inside the test container")
	}
	binary, err := engram.Acquire(context.Background(), t.TempDir())
	if err != nil {
		t.Fatalf("acquire real engram: %v", err)
	}
	dataDir := filepath.Join(t.TempDir(), "engram-data")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("ENGRAM_DATA_DIR", dataDir)
	engramRun := func(dir string, args ...string) string {
		t.Helper()
		command := exec.Command(binary, args...)
		command.Env = append(os.Environ(), "ENGRAM_DATA_DIR="+dir)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("engram %v: %v\n%s", args, err, out)
		}
		return string(out)
	}

	engramRun(dataDir, "save", "Alpha decision", "chose alpha for the parser", "--type", "decision", "--project", "fixture")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	server := exec.Command(binary, "serve", strconv.Itoa(port))
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Process.Kill(); _ = server.Wait() })
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		if conn, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(port)); err == nil {
			_ = conn.Close()
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("engram serve never listened: %v", err)
		}
	}
	engramRun(dataDir, "save", "Beta bug", "the beta bug came from stale cache", "--type", "bugfix", "--project", "fixture")
	if info, err := os.Stat(filepath.Join(dataDir, EngramDatabaseName+"-wal")); err == nil {
		t.Logf("source WAL while the server is up: %d bytes", info.Size())
	}

	dir, err := EngramDataDir()
	if err != nil || dir != dataDir {
		t.Fatalf("EngramDataDir() = %q, %v; want %q", dir, err, dataDir)
	}
	root := t.TempDir()
	result, err := ApplyUninstallRetention(context.Background(), root, RetentionChoices{MemoryFrom: dir}, time.Now())
	if err != nil || len(result.Incomplete) != 0 {
		t.Fatalf("ApplyUninstallRetention() = %+v, %v", result, err)
	}
	if !strings.HasPrefix(result.Memory, filepath.Join(root, RetainedDirName)+string(filepath.Separator)) {
		t.Fatalf("memory %q is not under %s", result.Memory, RetainedDirName)
	}

	db, err := sql.Open("sqlite", "file:"+result.Memory+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT title FROM observations ORDER BY id")
	if err != nil {
		t.Fatalf("read-only query on the delivered copy: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var titles []string
	for rows.Next() {
		var title string
		if err := rows.Scan(&title); err != nil {
			t.Fatal(err)
		}
		titles = append(titles, title)
	}
	if !slices.Equal(titles, []string{"Alpha decision", "Beta bug"}) {
		t.Fatalf("delivered rows = %v, want the two saved memories", titles)
	}
	// The copy is a working Engram database, not just a readable file.
	if out := engramRun(filepath.Dir(result.Memory), "search", "parser", "--project", "fixture"); !strings.Contains(out, "Alpha decision") {
		t.Fatalf("engram search over the delivered copy = %q, want Alpha decision", out)
	}
}
