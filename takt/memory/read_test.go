package memory_test

import (
	"context"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

func TestReadUnknownIDAndSection(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	_, err := s.Read(ctx, 99, "")
	wantRefusal(t, err, "#99", "not found")
	id := rec(t, s, external("w1"), memory.ScopeWorkspace, memory.NatureObservation, "doc", "# Intro\nhello\n# Usage\nrun it")
	_, err = s.Read(ctx, id, "Missing")
	wantRefusal(t, err, "Missing", "Intro", "Usage")
}

func TestReadSmallEntryReturnsEverything(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	w := external("w1")
	old := rec(t, s, w, memory.ScopeWorkspace, memory.NatureObservation, "doc", "first body")
	cur := mustRecord(t, s, w, memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace, Title: "doc v2",
		Content: "second body", Evidence: "seen", RelatesTo: rel(old, memory.RelationCorrects)}).ID
	sup := mustRecord(t, s, w, memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace, Title: "extra",
		Content: "supplement v1", Evidence: "seen", RelatesTo: rel(cur, memory.RelationSupplements)}).ID
	sup2 := mustRecord(t, s, w, memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace, Title: "extra v2",
		Content: "supplement v2", Evidence: "seen", RelatesTo: rel(sup, memory.RelationSupersedes)}).ID

	r, err := s.Read(ctx, old, "")
	if err != nil || r.CurrentID != cur || r.Body != "first body" || r.Outline != nil {
		t.Fatalf("old: %+v %v", r, err)
	}
	r, err = s.Read(ctx, cur, "")
	if err != nil || len(r.Supplements) != 1 || r.Supplements[0].ID != sup2 || r.Supplements[0].Body != "supplement v2" {
		t.Fatalf("supplement head: %+v %v", r.Supplements, err)
	}
	if text := r.Text(); !strings.Contains(text, "supplement v2") || !strings.Contains(text, "second body") {
		t.Fatal(text)
	}
}

func TestReadOverBudgetReturnsOutlineAndSection(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	w := external("w1")
	big := strings.Repeat("x", 40*1024)
	body := "# Alpha\n" + big + "\n```\n# not a heading\n```\n# Beta\n" + big + "\n"
	id := rec(t, s, w, memory.ScopeWorkspace, memory.NatureObservation, "big", body)
	tilde := rec(t, s, w, memory.ScopeWorkspace, memory.NatureObservation, "big tilde", "# Alpha\n"+big+"\n~~~\n# not a heading\n~~~\n# Beta\n"+big+"\n")
	if r, err := s.Read(ctx, tilde, ""); err != nil || len(r.Outline) != 2 || r.Outline[1].Heading != "Beta" {
		t.Fatalf("tilde fence: %+v %v", r.Outline, err)
	}
	sup := mustRecord(t, s, w, memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace, Title: "side",
		Content: "side body", Evidence: "seen", RelatesTo: rel(id, memory.RelationSupplements)}).ID

	r, err := s.Read(ctx, id, "")
	if err != nil || len(r.Outline) != 2 || r.Outline[0].Heading != "Alpha" || r.Outline[1].Heading != "Beta" || r.Section != "Alpha" {
		t.Fatalf("first section: %+v %v", r.Outline, err)
	}
	if strings.Contains(r.Body, "# Beta") || !strings.Contains(r.Body, "# not a heading") {
		t.Fatal("wrong section body")
	}
	if len(r.Supplements) != 1 || r.Supplements[0].ID != sup || r.Supplements[0].Body != "" {
		t.Fatalf("supplements: %+v", r.Supplements)
	}
	r, err = s.Read(ctx, id, "Beta")
	if err != nil || r.Section != "Beta" || !strings.HasPrefix(r.Body, "# Beta") {
		t.Fatalf("Beta: %v", err)
	}
}

func TestReadSupplementsBelongToTheWholeChain(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	w := external("w1")
	revise := func(title, content string, id int64, kind string) int64 {
		return mustRecord(t, s, w, memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace, Title: title,
			Content: content, Evidence: "seen", RelatesTo: rel(id, kind)}).ID
	}
	root := rec(t, s, w, memory.ScopeWorkspace, memory.NatureObservation, "root", "root v1")
	column := revise("column", "column v1", root, memory.RelationSupplements)
	newRoot := revise("root v2", "root v2", root, memory.RelationSupersedes)
	columnHead := revise("column v2", "column v2", column, memory.RelationSupersedes)

	r, err := s.Read(ctx, newRoot, "")
	if err != nil || len(r.Supplements) != 1 || r.Supplements[0].ID != columnHead || r.Supplements[0].Body != "column v2" {
		t.Fatalf("new root: %+v %v", r.Supplements, err)
	}
}
