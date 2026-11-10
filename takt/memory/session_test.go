package memory_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

func closeSession(t *testing.T, s *memory.Store, id memory.Identity) memory.RecordedClose {
	t.Helper()
	c, err := s.CloseSession(context.Background(), id, "objective", "state")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func wantRefusal(t *testing.T, err error, want ...string) {
	t.Helper()
	var ref *memory.RefusalError
	if !errors.As(err, &ref) {
		t.Fatalf("want refusal, got %v", err)
	}
	for _, w := range want {
		if !strings.Contains(err.Error(), w) {
			t.Fatalf("refusal %q lacks %q", err, w)
		}
	}
}

func TestCloseRefusedFromNonRoot(t *testing.T) {
	s := openStore(t)
	bind(t, s, "root", "takt", "root")
	bind(t, s, "child", "dev", "root")
	mustRecord(t, s, crew("child"), obs("a"))
	_, err := s.CloseSession(context.Background(), crew("child"), "o", "s")
	wantRefusal(t, err, "root session")
	closeSession(t, s, crew("root"))
}

func TestCloseNothingNewRefusedAndRecordsNothing(t *testing.T) {
	s := openStore(t)
	bind(t, s, "root", "takt", "root")
	_, err := s.CloseSession(context.Background(), crew("root"), "o", "s")
	wantRefusal(t, err, "nothing new")
	mustRecord(t, s, crew("root"), obs("a"))
	closeSession(t, s, crew("root"))
	_, err = s.CloseSession(context.Background(), crew("root"), "o", "s")
	wantRefusal(t, err, "nothing new")
	list, _ := s.Sessions(context.Background(), crew("root"), "", false, 0)
	if len(list) != 1 || len(list[0].Closes) != 1 {
		t.Fatalf("closes = %+v", list)
	}
}

func TestCloseRangesAcrossReopen(t *testing.T) {
	s := openStore(t)
	bind(t, s, "root", "takt", "root")
	bind(t, s, "child", "dev", "root")
	a := mustRecord(t, s, crew("root"), obs("a"))
	b := mustRecord(t, s, crew("child"), obs("b"))
	c1 := closeSession(t, s, crew("root"))
	if c1.FirstSequence != a.ID || c1.LastSequence != b.ID {
		t.Fatalf("first close = %+v", c1)
	}
	c := mustRecord(t, s, crew("root"), obs("c"))
	c2 := closeSession(t, s, crew("root"))
	if c2.FirstSequence != c.ID || c2.LastSequence != c.ID {
		t.Fatalf("second close = %+v", c2)
	}
}

func TestCloseClearsGrants(t *testing.T) {
	s := openStore(t)
	g := obs("global")
	g.Scope = memory.ScopeGlobal
	bind(t, s, "root", "takt", "root", g)
	mustRecord(t, s, crew("root"), obs("a"))
	closeSession(t, s, crew("root"))
	refusal(t, s, crew("root"), g, "confirmation")
}

func TestContinuityLifecycle(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	bind(t, s, "old", "takt", "old")
	bind(t, s, "old2", "takt", "old2")
	bind(t, s, "root", "takt", "root")
	bind(t, s, "child", "dev", "root")
	mustRecord(t, s, crew("old"), obs("o1"))
	mustRecord(t, s, crew("old2"), obs("o2"))
	closeSession(t, s, crew("old"))
	closeSession(t, s, crew("old2"))

	_, err := s.ContinueSession(ctx, crew("child"), "crew:old")
	wantRefusal(t, err, "root session")
	_, err = s.ContinueSession(ctx, crew("root"), "crew:missing")
	wantRefusal(t, err, "does not exist")
	// an open session is not a valid target
	bind(t, s, "live", "takt", "live")
	mustRecord(t, s, crew("live"), obs("l"))
	_, err = s.ContinueSession(ctx, crew("root"), "crew:live")
	wantRefusal(t, err, "not closed")
	// a close followed by a new entry reopens the session
	mustRecord(t, s, crew("old2"), obs("o3"))
	_, err = s.ContinueSession(ctx, crew("root"), "crew:old2")
	wantRefusal(t, err, "not closed")

	if got, err := s.ContinueSession(ctx, crew("root"), "crew:old"); err != nil || got != "crew:old" {
		t.Fatalf("declare = %q, %v", got, err)
	}
	closeSession(t, s, crew("old2"))
	if got, err := s.ContinueSession(ctx, crew("root"), "crew:old2"); err != nil || got != "crew:old2" {
		t.Fatalf("replace = %q, %v", got, err)
	}
	if got, err := s.ContinueSession(ctx, crew("root"), ""); err != nil || got != "" {
		t.Fatalf("withdraw = %q, %v", got, err)
	}
	if _, err := s.ContinueSession(ctx, crew("root"), "crew:old"); err != nil {
		t.Fatal(err)
	}
	mustRecord(t, s, crew("child"), obs("first")) // the first entry of the session fixes it
	_, err = s.ContinueSession(ctx, crew("root"), "crew:old2")
	wantRefusal(t, err, "fixed")
	_, err = s.ContinueSession(ctx, crew("root"), "")
	wantRefusal(t, err, "fixed")
}

func TestExternalContinuity(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	prev := memory.Identity{Principal: memory.PrincipalExternal, HostSession: "prev", Workspace: "ws"}
	mustRecord(t, s, prev, obs("p"))
	closeSession(t, s, prev)
	bind(t, s, "root", "takt", "root")
	mustRecord(t, s, crew("root"), obs("c"))
	closeSession(t, s, crew("root"))

	now := memory.Identity{Principal: memory.PrincipalExternal, HostSession: "now", Workspace: "ws"}
	_, err := s.ContinueSession(ctx, now, "crew:root")
	wantRefusal(t, err, "crew session", "closed external")
	_, err = s.ContinueSession(ctx, now, "external:nope")
	wantRefusal(t, err, "does not exist")
	if got, err := s.ContinueSession(ctx, now, "external:prev"); err != nil || got != "external:prev" {
		t.Fatalf("declare = %q, %v", got, err)
	}
	mustRecord(t, s, now, obs("n"))
	_, err = s.ContinueSession(ctx, now, "")
	wantRefusal(t, err, "fixed")
	// the crew may continue an external session
	bind(t, s, "root2", "takt", "root2")
	if _, err := s.ContinueSession(ctx, crew("root2"), "external:prev"); err != nil {
		t.Fatal(err)
	}
}

func TestSessionsListing(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	bind(t, s, "a", "takt", "a")
	bind(t, s, "b", "takt", "b")
	first := mustRecord(t, s, crew("a"), obs("alpha finding"))
	second := mustRecord(t, s, crew("a"), memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace,
		Title: "alpha revised", Evidence: "seen", Content: "SECRETBODY", RelatesTo: rel(first.ID, memory.RelationSupersedes)})
	if _, err := s.CloseSession(ctx, crew("a"), "ship the zebra", "done"); err != nil {
		t.Fatal(err)
	}
	mustRecord(t, s, crew("b"), obs("beta work"))
	other := memory.Identity{Principal: memory.PrincipalExternal, HostSession: "x", Workspace: "other"}
	mustRecord(t, s, other, obs("gamma elsewhere"))

	list, err := s.Sessions(ctx, crew("b"), "", false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 || list[0].ID != "crew:b" || list[0].State != memory.SessionCurrent || list[1].State != memory.SessionClosed {
		t.Fatalf("list = %+v", list)
	}
	list, _ = s.Sessions(ctx, crew("a"), "", false, 0)
	if list[0].ID != "crew:b" || list[0].State != memory.SessionOpen {
		t.Fatalf("other open session = %+v", list[0])
	}
	closed := list[1]
	if len(closed.Chronology) != 2 || closed.Chronology[0].ID != first.ID || closed.Chronology[0].Current ||
		closed.Chronology[1].ID != second.ID || !closed.Chronology[1].Current || closed.Chronology[1].Author != "takt" {
		t.Fatalf("chronology = %+v", closed.Chronology)
	}
	text := memory.FormatSessions(list)
	if strings.Contains(text, "SECRETBODY") || !strings.Contains(text, "ship the zebra") || !strings.Contains(text, "superseded") {
		t.Fatalf("text = %q", text)
	}

	list, _ = s.Sessions(ctx, crew("a"), "zebra", false, 0)
	if len(list) != 1 || list[0].ID != "crew:a" {
		t.Fatalf("close match = %+v", list)
	}
	list, _ = s.Sessions(ctx, crew("a"), "BETA", false, 0)
	if len(list) != 1 || list[0].ID != "crew:b" {
		t.Fatalf("title match = %+v", list)
	}
	list, _ = s.Sessions(ctx, crew("a"), `gamma "x`, false, 0)
	if len(list) != 0 {
		t.Fatalf("other workspace listed: %+v", list)
	}
	list, _ = s.Sessions(ctx, crew("a"), "gamma", true, 0)
	if len(list) != 1 || list[0].ID != "external:x" || list[0].Workspace != "other" {
		t.Fatalf("all workspaces = %+v", list)
	}
	list, _ = s.Sessions(ctx, crew("a"), "", true, 2)
	if len(list) != 2 {
		t.Fatalf("limit = %d", len(list))
	}
	list, _ = s.Sessions(ctx, crew("b"), "crew:a", false, 0)
	if len(list) != 1 || list[0].ID != "crew:a" {
		t.Fatalf("id match = %+v", list)
	}
}

func TestSessionsChronologyIsBounded(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	bind(t, s, "a", "takt", "a")
	for i := 0; i < 25; i++ {
		mustRecord(t, s, crew("a"), obs("entry "+itoa(int64(i))))
	}
	list, _ := s.Sessions(ctx, crew("a"), "", false, 0)
	got := list[0]
	if len(got.Chronology) != 20 || got.Omitted != 5 || got.Chronology[0].ID >= got.Chronology[19].ID {
		t.Fatalf("chronology %d omitted %d", len(got.Chronology), got.Omitted)
	}
}

func TestDescribe(t *testing.T) {
	ctx := context.Background()
	s := openStore(t)
	bind(t, s, "a", "takt", "a")
	first := mustRecord(t, s, crew("a"), obs("one"))
	second := mustRecord(t, s, crew("a"), memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace,
		Title: "two", Content: "two body", Evidence: "seen", RelatesTo: rel(first.ID, memory.RelationSupersedes)})
	res, err := s.Describe(ctx, []int64{first.ID, 999, second.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Entries) != 3 {
		t.Fatalf("result = %+v", res)
	}
	one, missing, two := res.Entries[0], res.Entries[1], res.Entries[2]
	if !one.Exists || one.Title != "one" || one.WriterHostSession != "a" || one.CurrentVersion != second.ID || len(one.Outgoing) != 0 {
		t.Fatalf("one = %+v", one)
	}
	if missing.Exists || missing.ID != 999 {
		t.Fatalf("missing = %+v", missing)
	}
	if two.CurrentVersion != second.ID || len(two.Outgoing) != 1 || two.Outgoing[0].Entry != first.ID ||
		two.Outgoing[0].Kind != memory.RelationSupersedes {
		t.Fatalf("two = %+v", two)
	}
}
