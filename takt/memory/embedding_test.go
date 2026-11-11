package memory_test

import (
	"context"
	"net/http"
	"os"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/memory/embed"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// loadTestModel acquires and loads the real model from TAKT_E2E_MODEL, skipping when it is not set.
func loadTestModel(t *testing.T) *embed.Model {
	t.Helper()
	if testing.Short() {
		t.Skip("real model fixture")
	}
	dir := os.Getenv("TAKT_E2E_MODEL")
	if dir == "" {
		t.Skip("real model fixture requires a model directory")
	}
	if err := embed.Pinned().Acquire(context.Background(), http.DefaultClient, dir); err != nil {
		t.Fatalf("acquire model: %v", err)
	}
	m, err := embed.Load(dir)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	return m
}

func TestCoverageWithoutModel(t *testing.T) {
	s := openStore(t)
	covered, total, err := s.Coverage(context.Background())
	if err != nil || covered != 0 || total != 0 {
		t.Fatalf("empty: %d/%d %v", covered, total, err)
	}
	rec(t, s, external("w1"), memory.ScopeWorkspace, memory.NatureObservation, "a", "text")
	if covered, total, _ = s.Coverage(context.Background()); covered != 0 || total != 1 {
		t.Fatalf("one entry: %d/%d", covered, total)
	}
	if err := s.EmbedPending(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if covered, _, _ = s.Coverage(context.Background()); covered != 0 {
		t.Fatal("nil model embedded something")
	}
}

func TestEmbedPendingCoversEveryEntry(t *testing.T) {
	model := loadTestModel(t)
	s := openStore(t)
	id := external("w1")
	rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "a", "# One\ntext\n# Two\nmore")
	rec(t, s, id, memory.ScopeGlobal, memory.NatureObservation, "b", "plain")
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if err := s.EmbedPending(ctx, model); err != nil {
			t.Fatal(err)
		}
		if covered, total, err := s.Coverage(ctx); err != nil || covered != 2 || total != 2 {
			t.Fatalf("round %d: %d/%d %v", i, covered, total, err)
		}
	}
}

// tinyModel loads the smallest model the service loads, so embedding runs without the network.
func tinyModel(t *testing.T) *embed.Model {
	t.Helper()
	dir := t.TempDir()
	testutil.WriteTinyModel(t, dir)
	m, err := embed.Load(dir)
	if err != nil {
		t.Fatalf("load model: %v", err)
	}
	return m
}

func TestEmbedPendingCoversEveryEntryAndSemanticSearchCountsTheRest(t *testing.T) {
	model := tinyModel(t)
	s := openStore(t)
	id := external("w1")
	rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "a", "# One\ntext\n# Two\nmore")
	rec(t, s, id, memory.ScopeWorkspace, memory.NatureObservation, "b", "plain")
	ctx := context.Background()
	semantic := memory.SearchArgs{Query: "text", Mode: memory.ModeSemantic, Scope: memory.ScopeWorkspace}

	before, err := s.Search(ctx, id, semantic, model)
	if err != nil || !before.Semantic || before.Uncovered != 2 {
		t.Fatalf("before embedding: %+v, %v", before, err)
	}
	if err := s.EmbedPending(ctx, model); err != nil {
		t.Fatal(err)
	}
	if covered, total, err := s.Coverage(ctx); err != nil || covered != 2 || total != 2 {
		t.Fatalf("coverage after embedding: %d/%d %v", covered, total, err)
	}
	after, err := s.Search(ctx, id, semantic, model)
	if err != nil || after.Uncovered != 0 {
		t.Fatalf("after embedding: %+v, %v", after, err)
	}
	semantic.IncludeHistory = true
	if _, err := s.Search(ctx, id, semantic, model); err != nil {
		t.Fatalf("semantic search over history: %v", err)
	}
}
