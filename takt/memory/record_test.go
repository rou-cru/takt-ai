package memory_test

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

func obs(title string) memory.RecordArgs {
	return memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace, Title: title, Content: "body of " + title, Evidence: "seen"}
}

func external(workspace string) memory.Identity {
	return memory.Identity{Principal: memory.PrincipalExternal, HostSession: "k1", Workspace: workspace}
}

func mustRecord(t *testing.T, s *memory.Store, id memory.Identity, a memory.RecordArgs) memory.RecordResult {
	t.Helper()
	r, err := s.Record(context.Background(), id, a)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func refusal(t *testing.T, s *memory.Store, id memory.Identity, a memory.RecordArgs, want ...string) {
	t.Helper()
	_, err := s.Record(context.Background(), id, a)
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

func rel(id int64, kind string) *memory.RelatesTo { return &memory.RelatesTo{ID: id, Relation: kind} }

func TestRecordFieldRefusals(t *testing.T) {
	s := openStore(t)
	bind(t, s, "h", "dev", "h")
	id := crew("h")
	bad := func(mut func(*memory.RecordArgs), want string) {
		a := obs("t")
		mut(&a)
		refusal(t, s, id, a, want)
	}
	bad(func(a *memory.RecordArgs) { a.Evidence = "" }, "evidence")
	bad(func(a *memory.RecordArgs) { a.Nature = memory.NatureDecision; a.Evidence = "" }, "evidence")
	bad(func(a *memory.RecordArgs) { a.Content = "" }, "content")
	bad(func(a *memory.RecordArgs) { a.RelatesTo = rel(99, memory.RelationSupplements) }, "does not exist")
	// hypothesis and proposal need no evidence
	a := obs("h")
	a.Nature, a.Evidence = memory.NatureHypothesis, ""
	mustRecord(t, s, id, a)
}

func TestRecordRelationMustShareTier(t *testing.T) {
	s := openStore(t)
	bind(t, s, "h", "dev", "h")
	g := obs("g")
	g.Scope = memory.ScopeGlobal
	gid := mustRecord(t, s, external(""), g)
	a := obs("w")
	a.RelatesTo = rel(gid.ID, memory.RelationSupplements)
	refusal(t, s, crew("h"), a, "same tier")
}

func TestRecordWorkspaceTierNeedsWorkspace(t *testing.T) {
	s := openStore(t)
	refusal(t, s, external(""), obs("x"), "--workspace")
	g := obs("x")
	g.Scope = memory.ScopeGlobal
	mustRecord(t, s, external(""), g)
	mustRecord(t, s, external("ws"), obs("y"))
}

func TestRecordHeadCheckAndCurrentSet(t *testing.T) {
	s := openStore(t)
	bind(t, s, "h", "dev", "h")
	v1 := mustRecord(t, s, crew("h"), obs("v1"))
	rev := obs("v2")
	rev.RelatesTo = rel(v1.ID, memory.RelationSupersedes)
	v2 := mustRecord(t, s, crew("h"), rev)
	// v1 left the current set: it is no longer the head, v2 is.
	rev3 := obs("v3")
	rev3.RelatesTo = rel(v1.ID, memory.RelationCorrects)
	refusal(t, s, crew("h"), rev3, "#"+itoa(v2.ID))
	rev3.RelatesTo = rel(v2.ID, memory.RelationCorrects)
	v3 := mustRecord(t, s, crew("h"), rev3)
	rev4 := obs("v4")
	rev4.RelatesTo = rel(v1.ID, memory.RelationSupersedes)
	refusal(t, s, crew("h"), rev4, "#"+itoa(v3.ID))
	// a supplement may still name a non-head entry
	sup := obs("s")
	sup.RelatesTo = rel(v1.ID, memory.RelationDisputes)
	mustRecord(t, s, crew("h"), sup)
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestCrewGrants(t *testing.T) {
	s := openStore(t)
	g := obs("pref")
	g.Scope = memory.ScopeGlobal
	bind(t, s, "h", "dev", "h")
	refusal(t, s, crew("h"), g, "confirmation")
	altered := g
	altered.Title = "other"
	bind(t, s, "h", "dev", "h", altered)
	refusal(t, s, crew("h"), g, "confirmation")
	bind(t, s, "h", "dev", "h", g)
	mustRecord(t, s, crew("h"), g)
	// the grant is consumed: a different record needs its own grant
	g2 := g
	g2.Title = "pref2"
	refusal(t, s, crew("h"), g2, "confirmation")

	order := obs("order")
	order.UserOrder = true
	refusal(t, s, crew("h"), order, "confirmation")
	bind(t, s, "h", "dev", "h", order)
	mustRecord(t, s, crew("h"), order)
	order.Title = "order again"
	refusal(t, s, crew("h"), order, "confirmation")
}

func TestExternalNeedsNoGrant(t *testing.T) {
	s := openStore(t)
	g := obs("pref")
	g.Scope = memory.ScopeGlobal
	g.UserOrder = true
	r := mustRecord(t, s, external(""), g)
	// external may revise a crew entry of its tier
	bind(t, s, "h", "dev", "h")
	c := mustRecord(t, s, crew("h"), obs("crew"))
	rev := obs("ext")
	rev.RelatesTo = rel(c.ID, memory.RelationCorrects)
	mustRecord(t, s, external("ws"), rev)
	if r.ID == 0 {
		t.Fatal("no id")
	}
}

func TestRecordRefusesBlankTitle(t *testing.T) {
	s := openStore(t)
	bind(t, s, "h", "dev", "h")
	refusal(t, s, crew("h"), obs("  "), "title")
}

func TestRecordRelationStaysInWorkspace(t *testing.T) {
	s := openStore(t)
	other := mustRecord(t, s, external("other-ws"), obs("o"))
	bind(t, s, "h", "dev", "h")
	a := obs("w")
	a.RelatesTo = rel(other.ID, memory.RelationSupplements)
	refusal(t, s, crew("h"), a, "this workspace")
	mine := mustRecord(t, s, crew("h"), obs("m"))
	a.RelatesTo = rel(mine.ID, memory.RelationSupplements)
	mustRecord(t, s, crew("h"), a)
}
