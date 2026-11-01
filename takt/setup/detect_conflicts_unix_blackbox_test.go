//go:build unix

package setup_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestDetectConflictsRefusesNonRegularPathWithoutOpeningIt(t *testing.T) {
	root := t.TempDir()
	fifo := filepath.Join(root, filepath.FromSlash(otherArtifactPath))
	if err := os.MkdirAll(filepath.Dir(fifo), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := setup.DetectConflicts(root, gatePlans("{}\n"))
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("DetectConflicts() error = %v, want a non-regular-file refusal", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DetectConflicts() blocked on a FIFO")
	}
}
