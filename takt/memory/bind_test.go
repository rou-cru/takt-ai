package memory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

func openStore(t *testing.T) *memory.Store {
	t.Helper()
	dir := t.TempDir()
	if err := memory.Init(dir); err != nil {
		t.Fatal(err)
	}
	s, err := memory.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func bind(t *testing.T, s *memory.Store, host, agent, root string, grants ...memory.RecordArgs) {
	t.Helper()
	if err := s.Bind(context.Background(), memory.BindArgs{HostSession: host, Agent: agent, ClaimedRoot: root, Workspace: "ws", Grants: grants}); err != nil {
		t.Fatal(err)
	}
}

func crew(host string) memory.Identity {
	return memory.Identity{Principal: memory.PrincipalCrew, HostSession: host}
}

func TestBindRootResolution(t *testing.T) {
	s := openStore(t)
	bind(t, s, "root", "takt", "root")
	bind(t, s, "child", "dev", "root")
	bind(t, s, "grand", "dev", "child") // claimed root is itself bound: resolves to its root
	bind(t, s, "child", "dev", "other") // existing binding keeps its root
	grand := mustRecord(t, s, crew("grand"), obs("grand"))
	child := mustRecord(t, s, crew("child"), obs("child"))
	_, err := s.CloseSession(context.Background(), crew("grand"), "o", "s")
	wantRefusal(t, err, "root session")
	if c := closeSession(t, s, crew("root")); c.FirstSequence != grand.ID || c.LastSequence != child.ID {
		t.Fatalf("close from the root = %+v, want it to cover entries #%d to #%d", c, grand.ID, child.ID)
	}
}

func TestBindRefreshesAgent(t *testing.T) {
	s := openStore(t)
	bind(t, s, "h", "dev", "h")
	a := obs("same")
	bind(t, s, "h", "qa", "h")
	r := mustRecord(t, s, crew("h"), a)
	got, err := s.Read(context.Background(), r.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Author != "qa" {
		t.Fatalf("author %q, want the refreshed agent", got.Author)
	}
}

func TestRecordWithoutBinding(t *testing.T) {
	s := openStore(t)
	_, err := s.Record(context.Background(), crew("nobody"), obs("x"))
	var ref *memory.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("got %v", err)
	}
}
