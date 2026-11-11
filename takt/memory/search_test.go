package memory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

func rec(t *testing.T, s *memory.Store, id memory.Identity, scope, nature, title, content string) int64 {
	t.Helper()
	return mustRecord(t, s, id, memory.RecordArgs{Nature: nature, Scope: scope, Title: title, Content: content, Evidence: "seen"}).ID
}

func search(t *testing.T, s *memory.Store, id memory.Identity, a memory.SearchArgs) memory.SearchResult {
	t.Helper()
	r, err := s.Search(context.Background(), id, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func ids(r memory.SearchResult) []int64 {
	var out []int64
	for _, h := range r.Hits {
		out = append(out, h.ID)
	}
	return out
}

func sameIDs(got []int64, want ...int64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestSearchHistoryFilterAndMarkers(t *testing.T) {
	s := openStore(t)
	id := external("w1")
	old := rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "cache", "the cache is warm")
	cur := mustRecord(t, s, id, memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace, Title: "cache v2",
		Content: "the cache is cold", Evidence: "seen", RelatesTo: rel(old, memory.RelationSupersedes)}).ID
	other := rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "cache note", "cache disputed here")
	mustRecord(t, s, id, memory.RecordArgs{Nature: memory.NatureHypothesis, Scope: memory.ScopeWorkspace, Title: "cache doubt",
		Content: "cache is wrong", RelatesTo: rel(other, memory.RelationDisputes)})
	exc := mustRecord(t, s, id, memory.RecordArgs{Nature: memory.NatureHypothesis, Scope: memory.ScopeWorkspace, Title: "cache edge",
		Content: "cache edge case", RelatesTo: rel(cur, memory.RelationExceptionTo)}).ID

	r := search(t, s, id, memory.SearchArgs{Query: "cache", Scope: memory.ScopeWorkspace})
	for _, h := range r.Hits {
		if h.ID == old {
			t.Fatal("superseded entry present without history")
		}
	}
	r = search(t, s, id, memory.SearchArgs{Query: "warm", Scope: memory.ScopeWorkspace, IncludeHistory: true})
	if !sameIDs(ids(r), old) || r.Hits[0].SupersededBy != cur {
		t.Fatalf("history hit: %+v", r.Hits)
	}
	text := r.Text()
	if !strings.Contains(text, "[superseded by #"+itoa(cur)+"]") {
		t.Fatal(text)
	}
	text = search(t, s, id, memory.SearchArgs{Query: "cache", Scope: memory.ScopeWorkspace}).Text()
	if !strings.Contains(text, "[disputed]") || !strings.Contains(text, "[exception]") {
		t.Fatal(text)
	}
	_ = exc
}

func TestSearchWorkspaceIsolation(t *testing.T) {
	s := openStore(t)
	a := rec(t, s, external("w1"), memory.ScopeWorkspace, memory.NatureObservation, "alpha", "shared term")
	b := rec(t, s, external("w2"), memory.ScopeWorkspace, memory.NatureObservation, "beta", "shared term")
	g := rec(t, s, external("w1"), memory.ScopeGlobal, memory.NatureObservation, "gamma", "shared term")
	if got := ids(search(t, s, external("w1"), memory.SearchArgs{Query: "shared", Scope: memory.ScopeWorkspace})); !sameIDs(got, a) {
		t.Fatalf("w1: %v", got)
	}
	if got := ids(search(t, s, external("w1"), memory.SearchArgs{Query: "shared", Scope: memory.ScopeWorkspace, AllWorkspaces: true})); !sameIDs(got, b, a) {
		t.Fatalf("all: %v", got)
	}
	if got := ids(search(t, s, external("w2"), memory.SearchArgs{Query: "shared", Scope: memory.ScopeGlobal})); !sameIDs(got, g) {
		t.Fatalf("global: %v", got)
	}
}

func TestSearchOrderAndTieRule(t *testing.T) {
	s := openStore(t)
	id := external("w1")
	first := rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "t", "tie word")
	second := mustRecord(t, s, id, memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace, Title: "t", Content: "tie word ", Evidence: "seen"}).ID
	strong := rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "needle needle", "needle needle needle")
	rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "filler", "tie word")
	got := ids(search(t, s, id, memory.SearchArgs{Query: "needle", Scope: memory.ScopeWorkspace}))
	if !sameIDs(got, strong) {
		t.Fatalf("got %v", got)
	}
	got = ids(search(t, s, id, memory.SearchArgs{Query: "tie", Scope: memory.ScopeWorkspace, Limit: 2}))
	if len(got) != 2 || got[0] < got[1] {
		t.Fatalf("tie order: %v (first %d second %d)", got, first, second)
	}
}

func TestSearchRefusals(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	_, err := s.Search(ctx, external("w1"), memory.SearchArgs{Query: "x", Scope: memory.ScopeWorkspace, Mode: memory.ModeSemantic}, nil)
	wantRefusal(t, err, "lexical")
	_, err = s.Search(ctx, external(""), memory.SearchArgs{Query: "x", Scope: memory.ScopeWorkspace}, nil)
	wantRefusal(t, err, "--workspace")
	_, err = s.Search(ctx, memory.Identity{Principal: memory.PrincipalCrew, HostSession: "nobody"}, memory.SearchArgs{Query: "x", Scope: memory.ScopeGlobal}, nil)
	wantRefusal(t, err, "binding")
	if r := search(t, s, external("w1"), memory.SearchArgs{Query: "x", Scope: memory.ScopeGlobal}); len(r.Hits) != 0 {
		t.Fatal("empty store returned hits")
	}
}

func TestSearchSpecialCharactersAreTerms(t *testing.T) {
	s := openStore(t)
	id := external("w1")
	a := rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "build", "uses c++ AND NOT go-lang")
	if got := ids(search(t, s, id, memory.SearchArgs{Query: `"c++" AND (go-lang`, Scope: memory.ScopeWorkspace})); !sameIDs(got, a) {
		t.Fatalf("got %v", got)
	}
}

func TestSearchCrewUsesBindingWorkspace(t *testing.T) {
	s := openStore(t)
	bind(t, s, "h1", "qa", "h1")
	n := rec(t, s, crew("h1"), memory.ScopeWorkspace, memory.NatureObservation, "mine", "crew entry")
	rec(t, s, external("other"), memory.ScopeWorkspace, memory.NatureObservation, "theirs", "crew entry")
	if got := ids(search(t, s, crew("h1"), memory.SearchArgs{Query: "crew", Scope: memory.ScopeWorkspace})); !sameIDs(got, n) {
		t.Fatalf("got %v", got)
	}
}

func TestSearchSemanticWithModel(t *testing.T) {
	model := loadTestModel(t)
	s := openStore(t)
	id := external("w1")
	a := rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "database", "the database stores records on disk")
	rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "fruit", "apples and oranges are fruit")
	ctx := context.Background()
	r, err := s.Search(ctx, id, memory.SearchArgs{Query: "persistent storage", Scope: memory.ScopeWorkspace, Mode: memory.ModeSemantic}, model)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Hits) != 0 || r.Uncovered != 2 {
		t.Fatalf("before embedding: hits %v uncovered %d", ids(r), r.Uncovered)
	}
	if err := s.EmbedPending(ctx, model); err != nil {
		t.Fatal(err)
	}
	r, err = s.Search(ctx, id, memory.SearchArgs{Query: "persistent storage", Scope: memory.ScopeWorkspace, Mode: memory.ModeSemantic}, model)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Hits) != 2 || r.Hits[0].ID != a || r.Uncovered != 0 || !strings.Contains(r.Text(), "without a semantic representation: 0") {
		t.Fatalf("after embedding: %+v", r)
	}
	late := rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "late", "written after the worker ran")
	r, _ = s.Search(ctx, id, memory.SearchArgs{Query: "persistent storage", Scope: memory.ScopeWorkspace, Mode: memory.ModeSemantic}, model)
	if r.Uncovered != 1 {
		t.Fatalf("uncovered %d for #%d", r.Uncovered, late)
	}
}

func TestSearchRefusesInvalidArguments(t *testing.T) {
	s := openStore(t)
	id := external("w1")
	for name, c := range map[string]struct {
		args memory.SearchArgs
		want string
	}{
		"blank query":                    {memory.SearchArgs{Query: "  "}, "query is required"},
		"query without searchable terms": {memory.SearchArgs{Query: "!!!"}, "no searchable terms"},
		"negative limit":                 {memory.SearchArgs{Query: "alpha", Limit: -1}, "limit -1"},
		"all workspaces on the global":   {memory.SearchArgs{Query: "alpha", Scope: memory.ScopeGlobal, AllWorkspaces: true}, "all_workspaces"},
		"semantic without a model":       {memory.SearchArgs{Query: "alpha", Mode: memory.ModeSemantic}, "mode lexical"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := s.Search(context.Background(), id, c.args, nil)
			wantRefusal(t, err, c.want)
		})
	}
}
