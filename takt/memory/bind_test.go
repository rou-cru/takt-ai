package memory_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
	_ "modernc.org/sqlite"
)

func openStore(t *testing.T) *memory.Store {
	t.Helper()
	s, _ := openStoreIn(t)
	return s
}

// openStoreIn opens a new store and also returns its directory.
func openStoreIn(t *testing.T) (*memory.Store, string) {
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
	return s, dir
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

func TestBindRefusesAnEmptyArgument(t *testing.T) {
	s := openStore(t)
	whole := memory.BindArgs{HostSession: "h", Agent: "dev", ClaimedRoot: "h", Workspace: "ws"}
	cases := map[string]func(a *memory.BindArgs){
		"host session": func(a *memory.BindArgs) { a.HostSession = "" },
		"agent":        func(a *memory.BindArgs) { a.Agent = "" },
		"claimed root": func(a *memory.BindArgs) { a.ClaimedRoot = "" },
		"workspace":    func(a *memory.BindArgs) { a.Workspace = "" },
	}
	for name, empty := range cases {
		t.Run(name, func(t *testing.T) {
			a := whole
			empty(&a)
			wantRefusal(t, s.Bind(context.Background(), a), "bind needs")
		})
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

// TestOperationsReportAMissingTable removes one table of a populated store and expects the operation
// that reads or writes it to fail with an error instead of returning a partial answer.
func TestOperationsReportAMissingTable(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name, table string
		run         func(*memory.Store) error
	}{
		{"sessions without closes", "closes", func(s *memory.Store) error {
			_, err := s.Sessions(ctx, crew("root"), "", false, 0)
			return err
		}},
		{"sessions without entries", "entries", func(s *memory.Store) error {
			_, err := s.Sessions(ctx, crew("root"), "", false, 0)
			return err
		}},
		{"sessions query without closes", "closes_fts", func(s *memory.Store) error {
			_, err := s.Sessions(ctx, crew("root"), "obs", false, 0)
			return err
		}},
		{"close without closes", "closes", func(s *memory.Store) error {
			mustRecord(t, s, crew("root"), obs("later"))
			_, err := s.CloseSession(ctx, crew("root"), "o", "s")
			return err
		}},
		{"close without the close index", "closes_fts", func(s *memory.Store) error {
			mustRecord(t, s, crew("root"), obs("later"))
			_, err := s.CloseSession(ctx, crew("root"), "o", "s")
			return err
		}},
		{"record without the entry index", "fts_current_1", func(s *memory.Store) error {
			_, err := s.Record(ctx, crew("root"), obs("x"))
			return err
		}},
		{"record without sessions", "sessions", func(s *memory.Store) error {
			_, err := s.Record(ctx, crew("root"), obs("x"))
			return err
		}},
		{"close without bindings", "bindings", func(s *memory.Store) error {
			_, err := s.CloseSession(ctx, crew("root"), "o", "s")
			return err
		}},
		{"continue without sessions", "sessions", func(s *memory.Store) error {
			_, err := s.ContinueSession(ctx, crew("root"), "other")
			return err
		}},
		{"record without entries", "entries", func(s *memory.Store) error {
			_, err := s.Record(ctx, crew("root"), obs("x"))
			return err
		}},
		{"record without partitions", "partitions", func(s *memory.Store) error {
			_, err := s.Record(ctx, crew("root"), obs("x"))
			return err
		}},
		{"record without current", "current", func(s *memory.Store) error {
			_, err := s.Record(ctx, crew("root"), obs("x"))
			return err
		}},
		{"record without relations", "relations", func(s *memory.Store) error {
			a := obs("x")
			a.RelatesTo = &memory.RelatesTo{ID: 1, Relation: memory.RelationSupplements}
			_, err := s.Record(ctx, crew("root"), a)
			return err
		}},
		{"record without bindings", "bindings", func(s *memory.Store) error {
			_, err := s.Record(ctx, crew("root"), obs("x"))
			return err
		}},
		{"read without entries", "entries", func(s *memory.Store) error {
			_, err := s.Read(ctx, 1, "")
			return err
		}},
		{"read without relations", "relations", func(s *memory.Store) error {
			_, err := s.Read(ctx, 1, "")
			return err
		}},
		{"describe without entries", "entries", func(s *memory.Store) error {
			_, err := s.Describe(ctx, []int64{1})
			return err
		}},
		{"search without partitions", "partitions", func(s *memory.Store) error {
			_, err := s.Search(ctx, crew("root"), memory.SearchArgs{Query: "obs", Scope: memory.ScopeWorkspace}, nil)
			return err
		}},
		{"bind without bindings", "bindings", func(s *memory.Store) error {
			return s.Bind(ctx, memory.BindArgs{HostSession: "n", Agent: "dev", ClaimedRoot: "root", Workspace: "ws"})
		}},
		{"coverage without chunks", "chunks", func(s *memory.Store) error {
			_, _, err := s.Coverage(ctx)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, dir := openStoreIn(t)
			bind(t, s, "root", "takt", "root")
			mustRecord(t, s, crew("root"), obs("obs one"))
			db, err := sql.Open("sqlite", storeFile(dir))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.Close() })
			if _, err := db.ExecContext(ctx, "DROP TABLE "+c.table); err != nil {
				t.Fatal(err)
			}
			if err := c.run(s); err == nil {
				t.Fatalf("no error without table %s", c.table)
			}
		})
	}
}
