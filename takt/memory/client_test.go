package memory_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

func TestAdminClientOverTheSocket(t *testing.T) {
	dir := shortDir(t)
	start(t, dir)
	ctx := context.Background()
	admin, err := memory.DialAdmin(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()

	if st, err := admin.Status(ctx); err != nil || st.State != memory.StateNoStore {
		t.Fatalf("status before init: %+v, %v", st, err)
	}
	if st, err := admin.Init(ctx); err != nil || st.State != memory.StateReady {
		t.Fatalf("init: %+v, %v", st, err)
	}
	if _, err := admin.Init(ctx); err == nil {
		t.Fatal("a second init was accepted")
	}
	if err := admin.Bind(ctx, memory.BindArgs{HostSession: "s1", Agent: "takt", ClaimedRoot: "s1", Workspace: "ws"}); err != nil {
		t.Fatalf("bind: %v", err)
	}
	if res, err := admin.Describe(ctx, memory.DescribeInput{IDs: []int64{99}}); err != nil || len(res.Entries) != 1 || res.Entries[0].Exists {
		t.Fatalf("describe: %+v, %v", res, err)
	}
	dest := filepath.Join(dir, "copy.db")
	if out, err := admin.Backup(ctx, memory.BackupInput{Path: dest}); err != nil || out.Path != dest || out.Size == 0 {
		t.Fatalf("backup: %+v, %v", out, err)
	}
}

func TestProbeDistinguishesUnreachableFromMalformed(t *testing.T) {
	dir := shortDir(t)
	if _, err := memory.Probe(context.Background(), dir); !errors.Is(err, memory.ErrUnreachable) {
		t.Fatalf("no owner: got %v, want ErrUnreachable", err)
	}

	ln, err := net.Listen("unix", filepath.Join(dir, memory.SocketFile))
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("garbage")) })}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	if _, err := memory.Probe(context.Background(), dir); !errors.Is(err, memory.ErrMalformed) {
		t.Fatalf("garbage answer: got %v, want ErrMalformed", err)
	}
}

func TestProbeNeverStartsTheOwner(t *testing.T) {
	dir := shortDir(t)
	_, _ = memory.Probe(context.Background(), dir)
	if _, err := os.Stat(filepath.Join(dir, memory.LockFile)); err == nil {
		t.Fatal("probe started an owner")
	}
}
