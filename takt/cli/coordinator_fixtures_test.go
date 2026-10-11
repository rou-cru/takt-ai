package main

import (
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/vfs"
	"testing"
)

func loadCoordinator(t *testing.T, root, state string) (*gc.Coordinator, error) {
	t.Helper()
	fs, err := vfs.Open(root, state)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := fs.Close(); err != nil {
			t.Error(err)
		}
	}()
	return gc.LoadCoordinator(fs)
}

func saveCoordinator(t *testing.T, root, state string, c *gc.Coordinator) {
	t.Helper()
	fs, err := vfs.Open(root, state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := fs.Close(); err != nil {
			t.Error(err)
		}
	}()
	if err = gc.SaveCoordinator(fs, c); err != nil {
		t.Fatal(err)
	}
}
