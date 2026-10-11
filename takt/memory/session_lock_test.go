package memory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMemoryLockCancellationReleasesFailedAcquisition(t *testing.T) {
	root := t.TempDir()
	held, err := lockSession(context.Background(), root, "s")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := lockSession(ctx, root, "s"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("lock wait: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancellation ignored")
	}
	if err := held.Close(); err != nil {
		t.Fatal(err)
	}
	acquired, err := lockSession(context.Background(), root, "s")
	if err != nil {
		t.Fatal(err)
	}
	if err := acquired.Close(); err != nil {
		t.Fatal(err)
	}
}
