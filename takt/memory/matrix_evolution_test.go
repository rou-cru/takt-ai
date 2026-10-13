package memory_test

import (
	"context"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

func TestMatrixEvolutionChecksBothTitlesAndColumnOwner(t *testing.T) {
	ctx := context.Background()
	f, cfg := newFake(t)
	record := func(author, title string, rel *memory.RelatesTo) (memory.RecordResult, error) {
		req := base()
		req.Author, req.Title, req.RelatesTo = author, title, rel
		return memory.Record(ctx, cfg, req)
	}
	matrix, err := record("qa", "Traceability Matrix", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := record("dev", "Unrelated observation", nil)
	if err != nil {
		t.Fatal(err)
	}
	columns := []struct{ owner, title string }{{"qa", "Traceability Matrix: Requirements"}, {"tpm", "Traceability Matrix: Owning node"}, {"verify", "Traceability Matrix: Evidence"}}
	for _, column := range columns {
		original, err := record(column.owner, column.title, &memory.RelatesTo{ID: matrix.ID, Relation: "supplements"})
		if err != nil {
			t.Fatal(err)
		}
		for _, relation := range []string{"supplements", "corrects", "supersedes", "exception-to"} {
			t.Run(column.owner+"/"+relation, func(t *testing.T) {
				evolved, err := record(column.owner, column.title, &memory.RelatesTo{ID: original.ID, Relation: relation})
				if err != nil {
					t.Fatal(err)
				}
				if evolved.ID == 0 || !slices.Contains(mustIDs(t, cfg, column.owner), evolved.ID) {
					t.Fatal("authorized evolution not persisted")
				}
				for _, wrong := range columns {
					if wrong.owner == column.owner {
						continue
					}
					before, _ := memory.EntryIDsForSession(cfg.Root, "ses_1")
					// Wrong owner uses its own valid column title to try switching the target.
					if _, err := record(wrong.owner, wrong.title, &memory.RelatesTo{ID: original.ID, Relation: relation}); err == nil {
						t.Fatal("evolution changed column via title")
					}
					// Reusing the target's title also cannot evade ownership.
					if _, err := record(wrong.owner, column.title, &memory.RelatesTo{ID: original.ID, Relation: relation}); err == nil {
						t.Fatal("wrong author evolved column")
					}
					if _, err := record("dev", "Corrected evidence", &memory.RelatesTo{ID: original.ID, Relation: relation}); err == nil {
						t.Fatal("generic title bypassed target ownership")
					}
					after, _ := memory.EntryIDsForSession(cfg.Root, "ses_1")
					if !slices.Equal(before, after) {
						t.Fatal("refused correction left writes")
					}
				}
			})
		}
		// Requested canonical titles cannot bypass validation through an unrelated target.
		if _, err := record("dev", column.title, &memory.RelatesTo{ID: other.ID, Relation: "supplements"}); err == nil {
			t.Fatal("requested column escaped validation")
		}
		if _, err := record(column.owner, column.title, nil); err == nil {
			t.Fatal("unlinked column accepted")
		}
		// Artifact recognition is independent of the attribution prefix Engram stored.
		f.mu.Lock()
		for i := range f.obs {
			if f.obs[i].ID == original.ID {
				f.obs[i].Title = "[old-specialty] [older-specialty] " + column.title
			}
		}
		f.mu.Unlock()
		if _, err := record("dev", "Evidence correction", &memory.RelatesTo{ID: original.ID, Relation: "corrects"}); err == nil {
			t.Fatal("foreign stored prefix bypassed validation")
		}
		if _, err := record(column.owner, column.title, &memory.RelatesTo{ID: original.ID, Relation: "supplements"}); err != nil {
			t.Fatal(err)
		}
		for _, author := range []string{"qa", "tpm", "verify", "dev", "test", "quality", "mutation", "explorer", "spec"} {
			if _, err := record(author, column.title, &memory.RelatesTo{ID: original.ID, Relation: "disputes"}); err != nil {
				t.Fatalf("%s dispute: %v", author, err)
			}
		}
	}
	if _, err := record("dev", "Traceability Matrix tooling notes", &memory.RelatesTo{ID: other.ID, Relation: "supplements"}); err != nil {
		t.Fatalf("unrelated entry altered: %v", err)
	}
}

func mustIDs(t *testing.T, cfg memory.Config, author string) []int64 {
	t.Helper()
	ids, err := memory.EntryIDsByAuthor(cfg.Root, "ses_1", author)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestMatrixTitlesCanonicalizeOnlyRecognizedArtifacts(t *testing.T) {
	for _, prefix := range []string{"", "[qa] ", "[qa] [tpm] "} {
		t.Run(prefix, func(t *testing.T) {
			f, cfg := newFake(t)
			req := base()
			req.Author, req.Title = "qa", prefix+"Traceability Matrix"
			result, err := memory.Record(context.Background(), cfg, req)
			if err != nil {
				t.Fatal(err)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			for _, o := range f.obs {
				if o.ID == result.ID && o.Title != "[qa] Traceability Matrix" {
					t.Fatalf("title = %q", o.Title)
				}
			}
		})
	}
	f, cfg := newFake(t)
	req := base()
	req.Title = "[custom] Ordinary notes"
	result, err := memory.Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.obs {
		if o.ID == result.ID && o.Title != "["+req.Author+"] "+req.Title {
			t.Fatalf("unrelated title = %q", o.Title)
		}
	}
}
