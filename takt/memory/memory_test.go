// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package memory_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/history"
)

// observation is the HTTP wire payload, asserted independently of internals.
type observation struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	ToolName  string `json:"tool_name"`
	Project   string `json:"project"`
	Scope     string `json:"scope"`
}

type fakeObs struct {
	observation
	ID int64
}

type fakeEdge struct {
	A, B      int64
	Relation  string
	Reasoning string
	Model     string
}

type fakeEngram struct {
	mu       sync.Mutex
	nextID   int64
	sessions []map[string]string
	obs      []fakeObs
	edges    []fakeEdge
	projects map[int64]string // extra observations seeded for relates_to lookups
}

// newFake serves an in-memory Engram for the package tests and the memory.Config that reaches it.
func newFake(t *testing.T) (*fakeEngram, memory.Config) {
	t.Helper()
	f := &fakeEngram{nextID: 100, projects: map[int64]string{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"status":"ok"}`)); err != nil {
			t.Errorf("write health response: %v", err)
		}
	})
	mux.HandleFunc("GET /project/current", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(map[string]string{"project": "demo", "cwd": r.URL.Query().Get("cwd")}); err != nil {
			t.Errorf("encode project response: %v", err)
		}
	})
	mux.HandleFunc("POST /sessions", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode session request: %v", err)
			return
		}
		f.mu.Lock()
		f.sessions = append(f.sessions, body)
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(map[string]string{"id": body["id"], "status": "created"}); err != nil {
			t.Errorf("encode session response: %v", err)
		}
	})
	mux.HandleFunc("POST /observations", func(w http.ResponseWriter, r *http.Request) {
		var o observation
		if err := json.NewDecoder(r.Body).Decode(&o); err != nil {
			t.Errorf("decode observation request: %v", err)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		for _, existing := range f.obs {
			if existing.observation == o {
				w.WriteHeader(http.StatusCreated)
				if err := json.NewEncoder(w).Encode(map[string]any{"id": existing.ID}); err != nil {
					t.Errorf("encode existing observation response: %v", err)
				}
				return
			}
		}
		f.nextID++
		f.obs = append(f.obs, fakeObs{o, f.nextID})
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(map[string]any{"id": f.nextID}); err != nil {
			t.Errorf("encode observation response: %v", err)
		}
	})
	mux.HandleFunc("GET /observations/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		f.mu.Lock()
		defer f.mu.Unlock()
		if p, ok := f.projects[id]; ok {
			if err := json.NewEncoder(w).Encode(map[string]any{"id": id, "project": p}); err != nil {
				t.Errorf("encode project observation response: %v", err)
			}
			return
		}
		for _, o := range f.obs {
			if o.ID == id {
				if err := json.NewEncoder(w).Encode(map[string]any{"id": id, "project": o.Project, "type": o.Type, "title": o.Title}); err != nil {
					t.Errorf("encode observation lookup response: %v", err)
				}
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		if _, err := w.Write([]byte(`{"error":"observation not found"}`)); err != nil {
			t.Errorf("write missing observation response: %v", err)
		}
	})
	mux.HandleFunc("POST /conflicts/compare", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			A          int64    `json:"memory_id_a"`
			B          int64    `json:"memory_id_b"`
			Relation   string   `json:"relation"`
			Confidence *float64 `json:"confidence"`
			Reasoning  string   `json:"reasoning"`
			Model      string   `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode conflict request: %v", err)
			return
		}
		if body.Confidence == nil || *body.Confidence != 1 {
			http.Error(w, `{"error":"confidence is required"}`, http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.edges = append(f.edges, fakeEdge{body.A, body.B, body.Relation, body.Reasoning, body.Model})
		f.mu.Unlock()
		if _, err := w.Write([]byte(`{"sync_id":"rel-x"}`)); err != nil {
			t.Errorf("write conflict response: %v", err)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	clock := time.Date(2026, 9, 13, 15, 0, 0, 0, time.UTC)
	return f, memory.Config{
		Root:      t.TempDir(),
		EngramURL: srv.URL,
		HTTP:      srv.Client(),
		Now:       func() time.Time { return clock },
	}
}

func (f *fakeEngram) byID(id int64) fakeObs {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.obs {
		if o.ID == id {
			return o
		}
	}
	return fakeObs{}
}

func (f *fakeEngram) edgesFrom(a int64) []fakeEdge {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []fakeEdge
	for _, e := range f.edges {
		if e.A == a {
			out = append(out, e)
		}
	}
	return out
}

func base() memory.RecordRequest {
	return memory.RecordRequest{
		Author: "dev", Session: "ses_1", Directory: "/work/demo",
		Nature: "proposal", Scope: "project", Title: "Split the parser", Content: "The parser mixes IO and parsing.",
	}
}

func TestValidateResultIDs(t *testing.T) {
	f, cfg := newFake(t)
	f.projects[123] = "demo"
	f.projects[456] = "demo"
	for _, test := range []struct {
		name string
		ids  []int64
		ok   bool
	}{
		{"each result exists", []int64{123, 456}, true},
		{"missing ID", nil, false},
		{"nonexistent ID", []int64{999}, false},
		{"duplicate ID", []int64{123, 123}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := memory.ValidateResultIDs(context.Background(), cfg, test.ids)
			if (err == nil) != test.ok {
				t.Fatalf("memory.ValidateResultIDs(%v) = %v; want success %v", test.ids, err, test.ok)
			}
		})
	}
}

func wantValidation(t *testing.T, err error) {
	t.Helper()
	var ve *memory.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *memory.ValidationError, got %v", err)
	}
}

func TestRecordValidation(t *testing.T) {
	_, cfg := newFake(t)
	cases := map[string]func(*memory.RecordRequest){
		"foreign author":           func(r *memory.RecordRequest) { r.Author = "gpt-helper" },
		"decision w/o evidence":    func(r *memory.RecordRequest) { r.Nature = "decision" },
		"observation w/o evidence": func(r *memory.RecordRequest) { r.Nature = "observation" },
		"personal w/o confirm":     func(r *memory.RecordRequest) { r.Scope = "personal" },
		"bad relation":             func(r *memory.RecordRequest) { r.RelatesTo = &memory.RelatesTo{ID: 1, Relation: "likes"} },
		"path session":             func(r *memory.RecordRequest) { r.Session = "../etc" },
		"content over safe limit":  func(r *memory.RecordRequest) { r.Content = strings.Repeat("x", 45001) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := base()
			mutate(&req)
			_, err := memory.Record(context.Background(), cfg, req)
			wantValidation(t, err)
		})
	}
}

func TestRecordDecisionFromInterlocutor(t *testing.T) {
	f, cfg := newFake(t)
	req := base()
	req.Author, req.Nature, req.Evidence = "architect", "decision", "user said so"
	res, err := memory.Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.byID(res.ID).Content; !strings.Contains(got, "**Authority**: user, as quoted by architect — user said so\n") {
		t.Fatalf("authority not the quoted user approval:\n%s", got)
	}
}

func TestRecordDecisionFromExecution(t *testing.T) {
	f, cfg := newFake(t)
	req := base()
	req.Nature, req.Evidence = "decision", "user: go with option B"
	res, err := memory.Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	got := f.byID(res.ID).Content
	if !strings.Contains(got, "**Author**: dev\n") || !strings.Contains(got, "**Authority**: user, as quoted by dev — user: go with option B\n") {
		t.Fatalf("attribution wrong:\n%s", got)
	}
}

func TestRecordUserOrderAttribution(t *testing.T) {
	f, cfg := newFake(t)
	req := base()
	req.Content = "The user ordered: next steps will cover the CLI."
	req.UserOrder = true
	res, err := memory.Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.byID(res.ID).Content; !strings.Contains(got, "**Authority**: user\n") {
		t.Fatalf("authority not user:\n%s", got)
	}
}

func TestRecordUserOrderedDecisionAttribution(t *testing.T) {
	f, cfg := newFake(t)
	req := base()
	req.Nature, req.Evidence, req.UserOrder = "decision", "user: ship it", true
	res, err := memory.Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.byID(res.ID).Content; !strings.Contains(got, "**Authority**: user\n") {
		t.Fatalf("a confirmed user order stamped as a quote:\n%s", got)
	}
}

func TestRecordRelatesToMissing(t *testing.T) {
	f, cfg := newFake(t)
	req := base()
	req.RelatesTo = &memory.RelatesTo{ID: 9999, Relation: "corrects"}
	_, err := memory.Record(context.Background(), cfg, req)
	wantValidation(t, err)
	if len(f.sessions) != 0 {
		t.Fatalf("rejected record created a session: %v", f.sessions)
	}

	f.projects[50] = "other"
	req.RelatesTo = &memory.RelatesTo{ID: 50, Relation: "corrects"}
	_, err = memory.Record(context.Background(), cfg, req)
	wantValidation(t, err)
}

func TestRecordContentAndLazyAnchor(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	req := base()
	req.Nature, req.Evidence = "observation", "go test output"
	first, err := memory.Record(ctx, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	second := base()
	second.Title = "Parser split landed"
	if _, err := memory.Record(ctx, cfg, second); err != nil {
		t.Fatal(err)
	}
	if len(f.sessions) != 1 || f.sessions[0]["id"] != "ses_1" || f.sessions[0]["project"] != "demo" || f.sessions[0]["directory"] != "/work/demo" {
		t.Fatalf("sessions = %v", f.sessions)
	}
	anchors := 0
	for _, o := range f.obs {
		if o.Type == "session_anchor" {
			anchors++
			wantAnchor := observation{SessionID: "ses_1", Type: "session_anchor", Title: "[takt] Session ses_1 started",
				Content:  "**Session**: ses_1\n**Directory**: /work/demo\n**Started**: 2026-09-13T15:00:00Z",
				ToolName: "takt-harness", Project: "demo", Scope: "project"}
			if o.observation != wantAnchor {
				t.Fatalf("anchor = %+v", o.observation)
			}
		}
	}
	if anchors != 1 {
		t.Fatalf("anchors = %d", anchors)
	}
	got := f.byID(first.ID).observation
	want := observation{SessionID: "ses_1", Type: "observation", Title: "[dev] Split the parser",
		Content:  "**Author**: dev\n**Nature**: observation\n**Tier**: workspace\n**Authority**: evidence: go test output\n**Session**: ses_1\n**Relation**: none\n\nThe parser mixes IO and parsing.",
		ToolName: "dev", Project: "demo", Scope: "project"}
	if got != want {
		t.Fatalf("entry =\n%+v\nwant\n%+v", got, want)
	}
}

func TestRecordRelationEdges(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	target, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	l, _ := readIndex(cfg.Root, "ses_1")
	verbs := map[string]struct{ verb, effect string }{
		"corrects":     {"supersedes", " (#%d is superseded)"},
		"supersedes":   {"supersedes", " (#%d is superseded)"},
		"supplements":  {"compatible", ""},
		"exception-to": {"scoped", ""},
		"disputes":     {"conflicts_with", " (#%d is disputed)"},
	}
	for rel, want := range verbs {
		verb := want.verb
		req := base()
		req.Title = "Relation " + rel
		req.RelatesTo = &memory.RelatesTo{ID: target.ID, Relation: rel}
		res, err := memory.Record(ctx, cfg, req)
		if err != nil {
			t.Fatal(err)
		}
		header := fmt.Sprintf("**Relation**: %s #%d", rel, target.ID)
		if want.effect != "" {
			header += fmt.Sprintf(want.effect, target.ID)
		}
		if c := f.byID(res.ID).Content; !strings.Contains(c, header+"\n") {
			t.Fatalf("relation header %q missing:\n%s", header, c)
		}
		want := []fakeEdge{
			{res.ID, l.StartAnchor, "related", "takt:in-session", "dev"},
			{res.ID, target.ID, verb, "takt:" + rel, "dev"},
		}
		if got := f.edgesFrom(res.ID); fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("%s edges = %v want %v", rel, got, want)
		}
	}
}

// MEM-SCP-2/4: workspace artifacts cannot land in the global tier, and a global
// entry is stored unbound from the project so the tiers stay partitioned.
func TestGlobalTierRejectsWorkspaceArtifacts(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	global := func() memory.RecordRequest {
		r := base()
		r.Scope, r.Confirmed = "personal", true
		r.Title, r.Content = "The user works in short review cycles", "The user asked for small diffs reviewed one at a time."
		return r
	}
	for name, mutate := range map[string]func(*memory.RecordRequest){
		"code excerpt":        func(r *memory.RecordRequest) { r.Content += " ```go\nfunc main() {}\n```" },
		"workspace directory": func(r *memory.RecordRequest) { r.Content += " Under /work/demo." },
	} {
		t.Run(name, func(t *testing.T) {
			req := global()
			mutate(&req)
			_, err := memory.Record(ctx, cfg, req)
			wantValidation(t, err)
		})
	}

	// MEM-SCP-2 wants universal environment quirks in the global tier, and those
	// name paths, so a path outside the workspace is not a workspace artifact.
	quirk := global()
	quirk.Title = "The user keeps their editor config outside the project"
	quirk.Content = "Their editor config lives in ~/.config/nvim/init.lua."
	if _, err := memory.Record(ctx, cfg, quirk); err != nil {
		t.Fatalf("global tier rejected an environment quirk: %v", err)
	}

	res, err := memory.Record(ctx, cfg, global())
	if err != nil {
		t.Fatal(err)
	}
	got := f.byID(res.ID).observation
	if got.Project != "" || got.Scope != "personal" {
		t.Fatalf("global entry is bound to a project: %+v", got)
	}
	if !strings.Contains(got.Content, "**Tier**: global\n") {
		t.Fatalf("tier missing:\n%s", got.Content)
	}

	// A workspace entry and a global entry sit in different tiers, so neither relates to the other.
	workspace, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	cross := global()
	cross.Title, cross.RelatesTo = "The user restated the review habit", &memory.RelatesTo{ID: workspace.ID, Relation: "supersedes"}
	_, err = memory.Record(ctx, cfg, cross)
	wantValidation(t, err)

	inward := base()
	inward.Title, inward.RelatesTo = "Parser split contradicts the habit", &memory.RelatesTo{ID: res.ID, Relation: "disputes"}
	_, err = memory.Record(ctx, cfg, inward)
	wantValidation(t, err)
}

// MEM-RET-4/5 and MEM-TYP-6: applicability is derived from the recorded links,
// so a superseded entry stops being presented as a directive while its nature
// and content stay untouched.
func TestSupersededEntryStopsGoverning(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	old, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	fresh := base()
	fresh.Nature, fresh.Title, fresh.Evidence = "decision", "Parser stays as one unit", "user approved keeping it"
	fresh.RelatesTo = &memory.RelatesTo{ID: old.ID, Relation: "supersedes"}
	newer, err := memory.Record(ctx, cfg, fresh)
	if err != nil {
		t.Fatal(err)
	}
	open := base()
	open.Title, open.Content = "The split is still the better option", "The tradeoff was not settled."
	open.RelatesTo = &memory.RelatesTo{ID: newer.ID, Relation: "disputes"}
	disputer, err := memory.Record(ctx, cfg, open)
	if err != nil {
		t.Fatal(err)
	}

	res, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_1", Directory: "/work/demo", Objective: "Decide the parser shape", State: "Disagreement open"})
	if err != nil {
		t.Fatal(err)
	}
	narrative := f.byID(res.EndAnchorID).Content
	for _, want := range []string{
		fmt.Sprintf("proposal #%d — Split the parser (superseded: history, not a current directive)", old.ID),
		fmt.Sprintf("decision #%d — Parser stays as one unit (disputed: open, not settled by recency)", newer.ID),
		fmt.Sprintf("proposal #%d — The split is still the better option\n", disputer.ID),
	} {
		if !strings.Contains(narrative, want) {
			t.Fatalf("missing %q in narrative:\n%s", want, narrative)
		}
	}
	// Recording never promotes a nature: the superseded entry is still a proposal.
	if o := f.byID(old.ID); o.Type != "proposal" || !strings.Contains(o.Content, "**Nature**: proposal\n") {
		t.Fatalf("nature changed: %+v", o.observation)
	}
}

func TestRecordDedup(t *testing.T) {
	_, cfg := newFake(t)
	ctx := context.Background()
	a, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	b, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || a.Deduplicated || !b.Deduplicated {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	l, _ := readIndex(cfg.Root, "ses_1")
	if len(l.Entries) != 1 {
		t.Fatalf("entries = %d", len(l.Entries))
	}
}

func TestRecordDedupIndexesEveryCaller(t *testing.T) {
	_, cfg := newFake(t)
	ctx := context.Background()
	first, second := base(), base()
	first.CallerSession, second.CallerSession = "ses_pm_a", "ses_pm_b"
	a, err := memory.Record(ctx, cfg, first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := memory.Record(ctx, cfg, second)
	if err != nil || !b.Deduplicated || b.ID != a.ID {
		t.Fatalf("second write = %+v, %v", b, err)
	}
	for _, caller := range []string{"ses_pm_a", "ses_pm_b"} {
		ids, err := memory.EntryIDsByCaller(cfg.Root, "ses_1", caller)
		if err != nil || !slices.Equal(ids, []int64{a.ID}) {
			t.Fatalf("EntryIDsByCaller(%s) = %v, %v", caller, ids, err)
		}
	}
	if l, _ := readIndex(cfg.Root, "ses_1"); len(l.Entries) != 1 {
		t.Fatalf("entries = %d", len(l.Entries))
	}
}

func TestCloseChronology(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	first, err := memory.Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	req := base()
	req.Author, req.Nature, req.Title, req.Evidence = "architect", "decision", "Froze the plugin boundary", "user approved the freeze"
	req.RelatesTo = &memory.RelatesTo{ID: first.ID, Relation: "supplements"}
	second, err := memory.Record(ctx, cfg, req)
	if err != nil {
		t.Fatal(err)
	}

	_, err = memory.Close(ctx, cfg, memory.CloseRequest{Author: "dev", Session: "ses_1", Directory: "/work/demo"})
	wantValidation(t, err)

	res, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_1", Directory: "/work/demo", Objective: "Split parser", State: "Parser split; CLI unwired"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Entries != 2 || res.EndAnchorID == 0 {
		t.Fatalf("res = %+v", res)
	}
	end := f.byID(res.EndAnchorID)
	wantContent := fmt.Sprintf(`**Session**: ses_1
**Closed**: 2026-09-13T15:00:00Z
**Continues**: none

## Objective
Split parser

## What happened
1. 2026-09-13T15:00:00Z [dev] proposal #%d — Split the parser
2. 2026-09-13T15:00:00Z [architect] decision #%d — Froze the plugin boundary

## How the entries relate
- #%d supplements #%d

## State at close
Parser split; CLI unwired`, first.ID, second.ID, second.ID, first.ID)
	if end.Content != wantContent || end.Title != "[takt] Session ses_1 closed" || end.ToolName != "takt-harness" || end.Type != "session_anchor" {
		t.Fatalf("end anchor =\n%+v\nwant content\n%s", end.observation, wantContent)
	}
	if strings.Contains(strings.ToLower(end.Content), "next steps") {
		t.Fatal("end anchor mentions next steps")
	}
	l, _ := readIndex(cfg.Root, "ses_1")
	if got := f.edgesFrom(res.EndAnchorID); fmt.Sprint(got) != fmt.Sprint([]fakeEdge{{res.EndAnchorID, l.StartAnchor, "related", "takt:session-end", "takt-harness"}}) {
		t.Fatalf("end edges = %v", got)
	}
	for _, id := range []int64{first.ID, second.ID} {
		edges := f.edgesFrom(id)
		last := edges[len(edges)-1]
		if last != (fakeEdge{id, res.EndAnchorID, "related", "takt:closed-in", "takt-harness"}) {
			t.Fatalf("closed-in edge for %d = %v", id, last)
		}
	}
	if l.EndAnchor != res.EndAnchorID {
		t.Fatalf("session index end anchor = %d", l.EndAnchor)
	}

	_, err = memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_1", Directory: "/work/demo"})
	wantValidation(t, err)

	// A new result resumes the same host conversation after a close.
	late := base()
	late.Title = "Renamed the parser"
	_, err = memory.Record(ctx, cfg, late)
	if err != nil {
		t.Fatal(err)
	}

	// A3: anchors are not memory entries, so nothing relates to them.
	for _, anchor := range []int64{l.StartAnchor, res.EndAnchorID} {
		req := base()
		req.Session, req.RelatesTo = "ses_2", &memory.RelatesTo{ID: anchor, Relation: "supplements"}
		_, err = memory.Record(ctx, cfg, req)
		wantValidation(t, err)
	}
	if len(f.sessions) != 1 {
		t.Fatalf("rejected records created sessions: %v", f.sessions)
	}
}

func TestCloseHolder(t *testing.T) {
	_, cfg := newFake(t)
	ctx := context.Background()
	dir := t.TempDir() // a role that HoldsInterface opens history at this state
	if _, err := memory.Record(ctx, cfg, base()); err != nil {
		t.Fatal(err)
	}
	_, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "dev", Session: "ses_1", Directory: dir})
	wantValidation(t, err)
	if _, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "architect", Session: "ses_1", Directory: dir}); err != nil {
		t.Fatal(err)
	}
	// A fallback close bypasses the holder check.
	if _, err := memory.Record(ctx, cfg, func() memory.RecordRequest { r := base(); r.Session = "ses_2"; return r }()); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "dev", Session: "ses_2", Directory: dir, Fallback: true}); err != nil {
		t.Fatal(err)
	}
}

func TestCloseFallbackWithoutSessionIndex(t *testing.T) {
	f, cfg := newFake(t)
	cfg.EngramURL = "http://127.0.0.1:1" // must not be contacted
	res, err := memory.Close(context.Background(), cfg, memory.CloseRequest{Session: "ses_none", Directory: "/work/demo", Fallback: true})
	if err != nil || res != (memory.CloseResult{}) {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if len(f.obs) != 0 {
		t.Fatal("fallback wrote to engram")
	}
}

func TestContinue(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	dir := t.TempDir() // a role that HoldsInterface opens history at this state
	cont := memory.ContinueRequest{Author: "takt", Session: "ses_2", Directory: dir, PreviousSession: "ses_1"}
	wantValidation(t, memory.Continue(ctx, cfg, cont))

	if _, err := memory.Record(ctx, cfg, base()); err != nil {
		t.Fatal(err)
	}
	wantValidation(t, memory.Continue(ctx, cfg, cont)) // previous not closed yet
	prev, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_1", Directory: dir})
	if err != nil {
		t.Fatal(err)
	}

	bad := cont
	bad.Author = "dev"
	wantValidation(t, memory.Continue(ctx, cfg, bad))

	cont.Author = "architect"
	if err := memory.Continue(ctx, cfg, cont); err != nil {
		t.Fatal(err)
	}
	l, _ := readIndex(cfg.Root, "ses_2")
	if l.PendingContinues != "ses_1" || l.Continues != "" || l.StartAnchor != 0 {
		t.Fatalf("declaration = %+v, want pending ses_1 and no anchor", l)
	}

	next := base()
	next.Session = "ses_2"
	next.Directory = dir
	next.Title = "Second session entry"
	if _, err := memory.Record(ctx, cfg, next); err != nil {
		t.Fatal(err)
	}
	l, _ = readIndex(cfg.Root, "ses_2")
	if l.Continues != "ses_1" || l.PendingContinues != "" {
		t.Fatalf("after first entry = %+v, want continues ses_1", l)
	}
	want := []fakeEdge{{l.StartAnchor, prev.EndAnchorID, "related", "takt:continues", "takt-harness"}}
	if got := f.edgesFrom(l.StartAnchor); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("edges = %v want %v", got, want)
	}
	wantValidation(t, memory.Continue(ctx, cfg, cont))

	res, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_2", Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	if c := f.byID(res.EndAnchorID).Content; !strings.Contains(c, "**Continues**: ses_1\n") {
		t.Fatalf("close content:\n%s", c)
	}
}

func TestContinueDeclarationIsRevocableUntilFirstEntry(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	dir := t.TempDir()
	for _, prev := range []string{"ses_a", "ses_b"} {
		writeIndex(t, cfg.Root, prev, `{"session":"`+prev+`","project":"elsewhere","start_anchor":7,"end_anchor":8,"entries":[]}`)
	}
	cont := memory.ContinueRequest{Author: "takt", Session: "ses_2", Directory: dir, PreviousSession: "ses_a"}
	for _, previous := range []string{"ses_a", "ses_b", "", "ses_b"} {
		cont.PreviousSession = previous
		if err := memory.Continue(ctx, cfg, cont); err != nil {
			t.Fatalf("declare %q: %v", previous, err)
		}
	}
	if len(f.edges) != 0 || len(f.obs) != 0 {
		t.Fatalf("a declaration wrote to engram: edges=%v obs=%d", f.edges, len(f.obs))
	}

	res, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_2", Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	l, _ := readIndex(cfg.Root, "ses_2")
	if l.Continues != "ses_b" {
		t.Fatalf("continues = %q, want the last declaration", l.Continues)
	}
	if got := f.edgesFrom(l.StartAnchor); len(got) != 1 || got[0].B != 8 {
		t.Fatalf("edges = %v, want one link to the other project's end anchor", got)
	}
	if c := f.byID(res.EndAnchorID).Content; !strings.Contains(c, "**Continues**: ses_b\n") {
		t.Fatalf("close content:\n%s", c)
	}
}

func TestContinueWithdrawnLeavesNoLink(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	dir := t.TempDir()
	writeIndex(t, cfg.Root, "ses_1", `{"session":"ses_1","project":"demo","start_anchor":7,"end_anchor":8,"entries":[]}`)
	cont := memory.ContinueRequest{Author: "takt", Session: "ses_2", Directory: dir}
	if err := memory.Continue(ctx, cfg, cont); err != nil { // withdrawing nothing is a no-op
		t.Fatal(err)
	}
	cont.PreviousSession = "ses_1"
	if err := memory.Continue(ctx, cfg, cont); err != nil {
		t.Fatal(err)
	}
	cont.PreviousSession = ""
	if err := memory.Continue(ctx, cfg, cont); err != nil {
		t.Fatal(err)
	}
	if res, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_2", Directory: dir, Fallback: true}); err != nil || res != (memory.CloseResult{}) {
		t.Fatalf("fallback close of a declaration-only session = %+v, %v", res, err)
	}
	if _, err := memory.Close(ctx, cfg, memory.CloseRequest{Author: "takt", Session: "ses_2", Directory: dir}); err != nil {
		t.Fatal(err)
	}
	l, _ := readIndex(cfg.Root, "ses_2")
	if l.Continues != "" || len(f.edgesFrom(l.StartAnchor)) != 0 {
		t.Fatalf("withdrawn declaration linked: %+v", l)
	}
}

func TestConcurrentRecords(t *testing.T) {
	f, cfg := newFake(t)
	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := base()
			req.Title = fmt.Sprintf("Finding %d", i)
			_, err := memory.Record(context.Background(), cfg, req)
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	l, err := readIndex(cfg.Root, "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != n || len(f.sessions) != 1 {
		t.Fatalf("entries = %d sessions = %d", len(l.Entries), len(f.sessions))
	}
}

func TestEnsureServeWithoutBinary(t *testing.T) {
	cfg := memory.Config{Root: t.TempDir(), EngramURL: "http://127.0.0.1:1"}
	_, err := memory.Record(context.Background(), cfg, base())
	var ve *memory.ValidationError
	if err == nil || errors.As(err, &ve) {
		t.Fatalf("want internal error, got %v", err)
	}
}

func TestDetectProjectBasename(t *testing.T) {
	dir := t.TempDir() + "/My-Proj"
	if got := memory.ResolveProject(context.Background(), memory.Config{EngramURL: "http://127.0.0.1:1"}, dir); got != "my-proj" {
		t.Fatalf("project = %q", got)
	}
}

func TestMemoryAcceptsPreviouslyBlockedWords(t *testing.T) {
	for _, text := range []string{"se verificó todo", "The log quoted: \"will\"", "Next steps", "pending", "going to", "to do"} {
		t.Run(text, func(t *testing.T) {
			f, cfg := newFake(t)
			req := base()
			req.Title, req.Content = text, text
			result, err := memory.Record(context.Background(), cfg, req)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(f.byID(result.ID).Content, "**Authority**: author\n") {
				t.Fatal("changed attribution")
			}
			if _, err := memory.Close(context.Background(), cfg, memory.CloseRequest{Author: "takt", Session: req.Session, Directory: req.Directory, Objective: text, State: text}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// MEM-AUT-6: the harness reports a session's own entries; the specialist never declares them.
func TestEntryIDsForSession(t *testing.T) {
	root := t.TempDir()
	ids, err := memory.EntryIDsForSession(root, "ses_missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("ids = %v; want empty", ids)
	}

	_, cfg := newFake(t)
	first, err := memory.Record(context.Background(), cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	req := base()
	req.Title = "Second finding"
	second, err := memory.Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := memory.EntryIDsForSession(cfg.Root, "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{first.ID, second.ID}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ids = %v; want %v", got, want)
	}
}

// IR-14..17: holdsSession admits the orchestrator always, the base holder
// when no switch was ever recorded, and only the current temporary holder
// once one has been.
func TestHoldsSessionInterlocutorHolder(t *testing.T) {
	dir := t.TempDir()
	h, err := history.Open(filepath.Join(dir, ".takt-ai", "vfs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Append(history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched,
		SessionID: "root", WorkUnitID: "child", AttemptID: history.FirstAttempt,
		Cause: history.CauseNone, Agent: "architect", Artifact: "artifacts/report.md",
	}); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}

	if got := memory.Capabilities(memory.CapabilitiesRequest{Author: "takt", Session: "root", Directory: dir}); !slices.Contains(got, "memory_continue_session") {
		t.Fatalf("orchestrator should always pass: %v", got)
	}
	if got := memory.Capabilities(memory.CapabilitiesRequest{Author: "architect", Session: "root", Directory: dir}); !slices.Contains(got, "memory_continue_session") {
		t.Fatalf("the current holder should pass: %v", got)
	}
	if slices.Contains(memory.Capabilities(memory.CapabilitiesRequest{Author: "pm", Session: "root", Directory: dir}), "memory_continue_session") {
		t.Fatal("a direct_interlocutor that is not the current holder should fail")
	}
}

func TestHoldsSessionBaseHolderWithoutSwitch(t *testing.T) {
	dir := t.TempDir()
	if got := memory.Capabilities(memory.CapabilitiesRequest{Author: "takt", Session: "root", Directory: dir}); !slices.Contains(got, "memory_continue_session") {
		t.Fatalf("orchestrator should always pass: %v", got)
	}
	if got := memory.Capabilities(memory.CapabilitiesRequest{Author: "architect", Session: "root", Directory: dir}); !slices.Contains(got, "memory_continue_session") {
		t.Fatalf("the base holder should pass when no switch was ever recorded: %v", got)
	}
	if slices.Contains(memory.Capabilities(memory.CapabilitiesRequest{Author: "dev", Session: "root", Directory: dir}), "memory_continue_session") {
		t.Fatal("a role that never holds the interface should fail")
	}
}

func TestCapabilitiesProjectsEveryCatalogInstance(t *testing.T) {
	cat, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, def := range cat.Agents {
		for _, id := range def.Instances {
			t.Run(id, func(t *testing.T) {
				tools := memory.Capabilities(memory.CapabilitiesRequest{Author: id, Session: "root", Directory: dir})
				want := []string{"memory_record"}
				if def.Role == model.RoleOrchestrator || def.Role.HoldsInterface() {
					want = append(want, "memory_continue_session", "memory_close_session")
				}
				if !slices.Equal(tools, want) {
					t.Fatalf("got %v, want %v", tools, want)
				}
				if len(want) == 1 {
					if err := memory.Continue(context.Background(), memory.Config{}, memory.ContinueRequest{Author: id, Session: "root", Directory: dir}); err == nil {
						t.Fatal("unauthorized direct continue accepted")
					}
					if _, err := memory.Close(context.Background(), memory.Config{}, memory.CloseRequest{Author: id, Session: "root", Directory: dir}); err == nil {
						t.Fatal("unauthorized direct close accepted")
					}
				}
			})
		}
	}
}

func TestCapabilitiesFailClosedAndReflectHolder(t *testing.T) {
	dir := t.TempDir()
	h, err := history.Open(filepath.Join(dir, ".takt-ai", "vfs"))
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched, SessionID: "root", WorkUnitID: "child", AttemptID: history.FirstAttempt, Cause: history.CauseNone, Agent: "architect", Artifact: "artifacts/report.md"}); err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		author, directory string
		count             int
	}{
		{"architect", dir, 3}, {"pm", dir, 1}, {"takt", dir, 3}, {"spec", dir, 1}, {"unknown", dir, 1}, {"architect", "", 1},
	} {
		t.Run(tt.author+tt.directory, func(t *testing.T) {
			got := memory.Capabilities(memory.CapabilitiesRequest{Author: tt.author, Session: "root", Directory: tt.directory})
			if len(got) != tt.count || got[0] != "memory_record" {
				t.Fatalf("unexpected capabilities: %v", got)
			}
		})
	}
}

func TestCapabilitiesDoesNotCreateHistory(t *testing.T) {
	dir := t.TempDir()
	if got := memory.Capabilities(memory.CapabilitiesRequest{Author: "architect", Session: "root", Directory: dir}); len(got) != 3 {
		t.Fatal(got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("capabilities wrote workspace: %v, %v", entries, err)
	}
	state := filepath.Join(dir, ".takt-ai", "vfs")
	if err := os.MkdirAll(state, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "history.sqlite"), []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := memory.Capabilities(memory.CapabilitiesRequest{Author: "architect", Session: "root", Directory: dir}); len(got) != 1 {
		t.Fatal(got)
	}
}

func TestCapabilitiesReevaluateOwnershipChanges(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, ".takt-ai", "vfs")
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	})
	for _, holder := range []string{"architect", "pm"} {
		if holder == "pm" {
			if err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorHandoff, SessionID: "root", WorkUnitID: "architect-child", AttemptID: history.FirstAttempt, Cause: history.CauseNone, Result: "Standard"}); err != nil {
				t.Fatal(err)
			}
		}
		if err := h.Append(history.Entry{Author: history.AuthorOrchestrator, Kind: history.KindInterlocutorSwitched, SessionID: "root", WorkUnitID: holder + "-child", AttemptID: history.FirstAttempt, Cause: history.CauseNone, Agent: holder, Artifact: "artifacts/report.md"}); err != nil {
			t.Fatal(err)
		}
		for _, author := range []string{"architect", "pm"} {
			got := memory.Capabilities(memory.CapabilitiesRequest{Author: author, Session: "root", Directory: dir})
			want := 1
			if author == holder {
				want = 3
			}
			if len(got) != want {
				t.Fatalf("holder %s, author %s: %v", holder, author, got)
			}
			if author != holder {
				if err := memory.Continue(context.Background(), memory.Config{}, memory.ContinueRequest{Author: author, Session: "root", Directory: dir}); err == nil {
					t.Fatal("non-holder continue allowed")
				}
				if _, err := memory.Close(context.Background(), memory.Config{}, memory.CloseRequest{Author: author, Session: "root", Directory: dir}); err == nil {
					t.Fatal("non-holder close allowed")
				}
			}
		}
	}
}

// TestRecordMatrixColumnOwnership checks that each traceability matrix column is
// written only by its author: qa creates the matrix and its rows, tpm the
// owning node, verify the evidence, each titled by the harness after its
// author's column; a dispute stays open to anyone, and entries that are not the
// matrix are unaffected.
func TestRecordMatrixColumnOwnership(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	entry := func(author, title string, rel *memory.RelatesTo) memory.RecordRequest {
		req := base()
		req.Author, req.Title, req.RelatesTo = author, title, rel
		return req
	}
	matrix, err := memory.Record(ctx, cfg, entry("qa", "Traceability Matrix", nil))
	if err != nil {
		t.Fatalf("qa creating the matrix: %v", err)
	}
	other, err := memory.Record(ctx, cfg, entry("dev", "Split the parser plan", nil))
	if err != nil {
		t.Fatal(err)
	}
	supplements := &memory.RelatesTo{ID: matrix.ID, Relation: "supplements"}
	requirements, err := memory.Record(ctx, cfg, entry("qa", "Traceability Matrix: Requirements", &memory.RelatesTo{ID: matrix.ID, Relation: "supersedes"}))
	if err != nil {
		t.Fatalf("qa superseding the matrix with its rows: %v", err)
	}
	for _, tc := range []struct {
		name      string
		req       memory.RecordRequest
		fragments []string // empty when the write is accepted
	}{
		{"qa refines its rows", entry("qa", "Traceability Matrix: Requirements", supplements), nil},
		{"tpm adds the owning node", entry("tpm", "Traceability Matrix: Owning node", supplements), nil},
		{"verify adds the evidence", entry("verify", "Traceability Matrix: Evidence", supplements), nil},
		{"anyone disputes it", entry("dev", "The matrix misses a row", &memory.RelatesTo{ID: matrix.ID, Relation: "disputes"}), nil},
		{"a non-matrix target is unaffected", entry("dev", "Parser evidence", &memory.RelatesTo{ID: other.ID, Relation: "supplements"}), nil},
		{"tpm creating a matrix", entry("tpm", "Traceability Matrix", nil), []string{"supplements"}},
		{"dev creating a matrix", entry("dev", "Traceability Matrix", nil), []string{"qa, tpm and verify", "disputes"}},

		{"qa supersedes its latest rows", entry("qa", "Traceability Matrix: Requirements", &memory.RelatesTo{ID: requirements.ID, Relation: "supersedes"}), nil},
		{"verify revising rows", entry("verify", "Traceability Matrix: Requirements", &memory.RelatesTo{ID: matrix.ID, Relation: "corrects"}), []string{"qa's", "supplements"}},
		{"dev adding to it", entry("dev", "Traceability Matrix: Evidence", supplements), []string{"qa, tpm and verify", "disputes"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := memory.Record(ctx, cfg, tc.req)
			if len(tc.fragments) == 0 {
				if err != nil {
					t.Fatalf("memory.Record() = %v; want the write accepted", err)
				}
				return
			}
			wantValidation(t, err)
			for _, fragment := range tc.fragments {
				if !strings.Contains(err.Error(), fragment) {
					t.Errorf("rejection lacks %q: %v", fragment, err)
				}
			}
		})
	}
	// The column is the author's, so the harness titles it whatever the
	// author called it.
	for _, title := range []string{"Traceability Matrix: Evidence", "Owning nodes"} {
		res, err := memory.Record(ctx, cfg, entry("tpm", title, supplements))
		if err != nil {
			t.Fatalf("tpm adding %q: %v", title, err)
		}
		if got := f.byID(res.ID).Title; !strings.HasSuffix(got, "Traceability Matrix: Owning node") {
			t.Errorf("tpm entry %q stored as %q; want its own column's title", title, got)
		}
	}
}
