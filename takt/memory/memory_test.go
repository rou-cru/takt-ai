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

package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/history"
)

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

func newFake(t *testing.T) (*fakeEngram, Config) {
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
				if err := json.NewEncoder(w).Encode(map[string]any{"id": id, "project": o.Project, "type": o.Type}); err != nil {
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
	return f, Config{
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

func base() RecordRequest {
	return RecordRequest{
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
			err := ValidateResultIDs(context.Background(), cfg, test.ids)
			if (err == nil) != test.ok {
				t.Fatalf("ValidateResultIDs(%v) = %v; want success %v", test.ids, err, test.ok)
			}
		})
	}
}

func wantValidation(t *testing.T, err error) {
	t.Helper()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want *ValidationError, got %v", err)
	}
}

func TestRecordValidation(t *testing.T) {
	_, cfg := newFake(t)
	cases := map[string]func(*RecordRequest){
		"foreign author":           func(r *RecordRequest) { r.Author = "gpt-helper" },
		"decision w/o evidence":    func(r *RecordRequest) { r.Nature = "decision" },
		"observation w/o evidence": func(r *RecordRequest) { r.Nature = "observation" },
		"personal w/o confirm":     func(r *RecordRequest) { r.Scope = "personal" },
		"bad relation":             func(r *RecordRequest) { r.RelatesTo = &RelatesTo{ID: 1, Relation: "likes"} },
		"path session":             func(r *RecordRequest) { r.Session = "../etc" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := base()
			mutate(&req)
			_, err := Record(context.Background(), cfg, req)
			wantValidation(t, err)
		})
	}
}

func TestRecordDecisionFromInterlocutor(t *testing.T) {
	f, cfg := newFake(t)
	req := base()
	req.Author, req.Nature, req.Evidence = "architect", "decision", "user said so"
	res, err := Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.byID(res.ID).Content; !strings.Contains(got, "**Authority**: user — user said so\n") {
		t.Fatalf("authority not user with evidence:\n%s", got)
	}
}

func TestRecordDecisionFromExecution(t *testing.T) {
	f, cfg := newFake(t)
	req := base()
	req.Nature, req.Evidence = "decision", "user: go with option B"
	res, err := Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	got := f.byID(res.ID).Content
	if !strings.Contains(got, "**Author**: dev\n") || !strings.Contains(got, "**Authority**: user — user: go with option B\n") {
		t.Fatalf("attribution wrong:\n%s", got)
	}
}

func TestRecordUserOrderAttribution(t *testing.T) {
	f, cfg := newFake(t)
	req := base()
	req.Content = "The user ordered: next steps will cover the CLI."
	req.UserOrder = true
	res, err := Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if got := f.byID(res.ID).Content; !strings.Contains(got, "**Authority**: user\n") {
		t.Fatalf("authority not user:\n%s", got)
	}
}

func TestRecordRelatesToMissing(t *testing.T) {
	f, cfg := newFake(t)
	req := base()
	req.RelatesTo = &RelatesTo{ID: 9999, Relation: "corrects"}
	_, err := Record(context.Background(), cfg, req)
	wantValidation(t, err)
	if len(f.sessions) != 0 {
		t.Fatalf("rejected record created a session: %v", f.sessions)
	}

	f.projects[50] = "other"
	req.RelatesTo = &RelatesTo{ID: 50, Relation: "corrects"}
	_, err = Record(context.Background(), cfg, req)
	wantValidation(t, err)
}

func TestRecordContentAndLazyAnchor(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	req := base()
	req.Nature, req.Evidence = "observation", "go test output"
	first, err := Record(ctx, cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	second := base()
	second.Title = "Parser split landed"
	if _, err := Record(ctx, cfg, second); err != nil {
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
	target, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	l, _ := loadSessionIndex(cfg.Root, "ses_1")
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
		req.RelatesTo = &RelatesTo{ID: target.ID, Relation: rel}
		res, err := Record(ctx, cfg, req)
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
	global := func() RecordRequest {
		r := base()
		r.Scope, r.Confirmed = "personal", true
		r.Title, r.Content = "The user works in short review cycles", "The user asked for small diffs reviewed one at a time."
		return r
	}
	for name, mutate := range map[string]func(*RecordRequest){
		"code excerpt":        func(r *RecordRequest) { r.Content += " ```go\nfunc main() {}\n```" },
		"workspace directory": func(r *RecordRequest) { r.Content += " Under /work/demo." },
	} {
		t.Run(name, func(t *testing.T) {
			req := global()
			mutate(&req)
			_, err := Record(ctx, cfg, req)
			wantValidation(t, err)
		})
	}

	// MEM-SCP-2 wants universal environment quirks in the global tier, and those
	// name paths, so a path outside the workspace is not a workspace artifact.
	quirk := global()
	quirk.Title = "The user keeps their editor config outside the project"
	quirk.Content = "Their editor config lives in ~/.config/nvim/init.lua."
	if _, err := Record(ctx, cfg, quirk); err != nil {
		t.Fatalf("global tier rejected an environment quirk: %v", err)
	}

	res, err := Record(ctx, cfg, global())
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
	workspace, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	cross := global()
	cross.Title, cross.RelatesTo = "The user restated the review habit", &RelatesTo{ID: workspace.ID, Relation: "supersedes"}
	_, err = Record(ctx, cfg, cross)
	wantValidation(t, err)

	inward := base()
	inward.Title, inward.RelatesTo = "Parser split contradicts the habit", &RelatesTo{ID: res.ID, Relation: "disputes"}
	_, err = Record(ctx, cfg, inward)
	wantValidation(t, err)
}

// MEM-RET-4/5 and MEM-TYP-6: applicability is derived from the recorded links,
// so a superseded entry stops being presented as a directive while its nature
// and content stay untouched.
func TestSupersededEntryStopsGoverning(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	old, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	fresh := base()
	fresh.Nature, fresh.Title, fresh.Evidence = "decision", "Parser stays as one unit", "user approved keeping it"
	fresh.RelatesTo = &RelatesTo{ID: old.ID, Relation: "supersedes"}
	newer, err := Record(ctx, cfg, fresh)
	if err != nil {
		t.Fatal(err)
	}
	open := base()
	open.Title, open.Content = "The split is still the better option", "The tradeoff was not settled."
	open.RelatesTo = &RelatesTo{ID: newer.ID, Relation: "disputes"}
	disputer, err := Record(ctx, cfg, open)
	if err != nil {
		t.Fatal(err)
	}

	res, err := Close(ctx, cfg, CloseRequest{Author: "takt", Session: "ses_1", Directory: "/work/demo", Objective: "Decide the parser shape", State: "Disagreement open"})
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
	a, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	b, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || a.Deduplicated || !b.Deduplicated {
		t.Fatalf("a=%+v b=%+v", a, b)
	}
	l, _ := loadSessionIndex(cfg.Root, "ses_1")
	if len(l.Entries) != 1 {
		t.Fatalf("entries = %d", len(l.Entries))
	}
}

func TestCloseChronology(t *testing.T) {
	f, cfg := newFake(t)
	ctx := context.Background()
	first, err := Record(ctx, cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	req := base()
	req.Author, req.Nature, req.Title, req.Evidence = "architect", "decision", "Froze the plugin boundary", "user approved the freeze"
	req.RelatesTo = &RelatesTo{ID: first.ID, Relation: "supplements"}
	second, err := Record(ctx, cfg, req)
	if err != nil {
		t.Fatal(err)
	}

	_, err = Close(ctx, cfg, CloseRequest{Author: "dev", Session: "ses_1", Directory: "/work/demo"})
	wantValidation(t, err)

	res, err := Close(ctx, cfg, CloseRequest{Author: "takt", Session: "ses_1", Directory: "/work/demo", Objective: "Split parser", State: "Parser split; CLI unwired"})
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
	l, _ := loadSessionIndex(cfg.Root, "ses_1")
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

	_, err = Close(ctx, cfg, CloseRequest{Author: "takt", Session: "ses_1", Directory: "/work/demo"})
	wantValidation(t, err)

	// A2: a closed session accepts no more entries.
	late := base()
	late.Title = "Renamed the parser"
	_, err = Record(ctx, cfg, late)
	wantValidation(t, err)

	// A3: anchors are not memory entries, so nothing relates to them.
	for _, anchor := range []int64{l.StartAnchor, res.EndAnchorID} {
		req := base()
		req.Session, req.RelatesTo = "ses_2", &RelatesTo{ID: anchor, Relation: "supplements"}
		_, err = Record(ctx, cfg, req)
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
	if _, err := Record(ctx, cfg, base()); err != nil {
		t.Fatal(err)
	}
	_, err := Close(ctx, cfg, CloseRequest{Author: "dev", Session: "ses_1", Directory: dir})
	wantValidation(t, err)
	if _, err := Close(ctx, cfg, CloseRequest{Author: "architect", Session: "ses_1", Directory: dir}); err != nil {
		t.Fatal(err)
	}
	// A fallback close bypasses the holder check.
	if _, err := Record(ctx, cfg, func() RecordRequest { r := base(); r.Session = "ses_2"; return r }()); err != nil {
		t.Fatal(err)
	}
	if _, err := Close(ctx, cfg, CloseRequest{Author: "dev", Session: "ses_2", Directory: dir, Fallback: true}); err != nil {
		t.Fatal(err)
	}
}

func TestCloseFallbackWithoutSessionIndex(t *testing.T) {
	f, cfg := newFake(t)
	cfg.EngramURL = "http://127.0.0.1:1" // must not be contacted
	res, err := Close(context.Background(), cfg, CloseRequest{Session: "ses_none", Directory: "/work/demo", Fallback: true})
	if err != nil || res != (CloseResult{}) {
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
	cont := ContinueRequest{Author: "takt", Session: "ses_2", Directory: dir, PreviousSession: "ses_1"}
	wantValidation(t, Continue(ctx, cfg, cont))

	if _, err := Record(ctx, cfg, base()); err != nil {
		t.Fatal(err)
	}
	wantValidation(t, Continue(ctx, cfg, cont)) // previous not closed yet
	prev, err := Close(ctx, cfg, CloseRequest{Author: "takt", Session: "ses_1", Directory: dir})
	if err != nil {
		t.Fatal(err)
	}

	bad := cont
	bad.Author = "dev"
	wantValidation(t, Continue(ctx, cfg, bad))

	cont.Author = "architect"
	if err := Continue(ctx, cfg, cont); err != nil {
		t.Fatal(err)
	}
	l, _ := loadSessionIndex(cfg.Root, "ses_2")
	if l.Continues != "ses_1" {
		t.Fatalf("continues = %q", l.Continues)
	}
	want := []fakeEdge{{l.StartAnchor, prev.EndAnchorID, "related", "takt:continues", "takt-harness"}}
	if got := f.edgesFrom(l.StartAnchor); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("edges = %v want %v", got, want)
	}
	wantValidation(t, Continue(ctx, cfg, cont))

	res, err := Close(ctx, cfg, CloseRequest{Author: "takt", Session: "ses_2", Directory: dir})
	if err != nil {
		t.Fatal(err)
	}
	if c := f.byID(res.EndAnchorID).Content; !strings.Contains(c, "**Continues**: ses_1\n") {
		t.Fatalf("close content:\n%s", c)
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
			_, err := Record(context.Background(), cfg, req)
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
	l, err := loadSessionIndex(cfg.Root, "ses_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != n || len(f.sessions) != 1 {
		t.Fatalf("entries = %d sessions = %d", len(l.Entries), len(f.sessions))
	}
}

func TestEnsureServeWithoutBinary(t *testing.T) {
	cfg := Config{Root: t.TempDir(), EngramURL: "http://127.0.0.1:1"}
	_, err := Record(context.Background(), cfg, base())
	var ve *ValidationError
	if err == nil || errors.As(err, &ve) {
		t.Fatalf("want internal error, got %v", err)
	}
}

func TestDetectProjectBasename(t *testing.T) {
	dir := t.TempDir() + "/My-Proj"
	if got := detectProject(context.Background(), dir); got != "my-proj" {
		t.Fatalf("project = %q", got)
	}
}

func TestMemoryAcceptsPreviouslyBlockedWords(t *testing.T) {
	for _, text := range []string{"se verificó todo", "The log quoted: \"will\"", "Next steps", "pending", "going to", "to do"} {
		t.Run(text, func(t *testing.T) {
			f, cfg := newFake(t)
			req := base()
			req.Title, req.Content = text, text
			result, err := Record(context.Background(), cfg, req)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(f.byID(result.ID).Content, "**Authority**: author\n") {
				t.Fatal("changed attribution")
			}
			if _, err := Close(context.Background(), cfg, CloseRequest{Author: "takt", Session: req.Session, Directory: req.Directory, Objective: text, State: text}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// MEM-AUT-6: the harness reports a session's own entries; the specialist never declares them.
func TestEntryIDsForSession(t *testing.T) {
	root := t.TempDir()
	ids, err := EntryIDsForSession(root, "ses_missing")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 0 {
		t.Fatalf("ids = %v; want empty", ids)
	}

	_, cfg := newFake(t)
	first, err := Record(context.Background(), cfg, base())
	if err != nil {
		t.Fatal(err)
	}
	req := base()
	req.Title = "Second finding"
	second, err := Record(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := EntryIDsForSession(cfg.Root, "ses_1")
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
	h, err := history.Open(dir)
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

	if err := holdsSession("takt", "root", dir); err != nil {
		t.Fatalf("orchestrator should always pass: %v", err)
	}
	if err := holdsSession("architect", "root", dir); err != nil {
		t.Fatalf("the current holder should pass: %v", err)
	}
	if err := holdsSession("pm", "root", dir); err == nil {
		t.Fatal("a direct_interlocutor that is not the current holder should fail")
	}
}

func TestHoldsSessionBaseHolderWithoutSwitch(t *testing.T) {
	dir := t.TempDir()
	if err := holdsSession("takt", "root", dir); err != nil {
		t.Fatalf("orchestrator should always pass: %v", err)
	}
	if err := holdsSession("architect", "root", dir); err != nil {
		t.Fatalf("the base holder should pass when no switch was ever recorded: %v", err)
	}
	if err := holdsSession("dev", "root", dir); err == nil {
		t.Fatal("a role that never holds the interface should fail")
	}
}
