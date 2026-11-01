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
	"errors"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

const searchDirectory = "/work/demo"

// recordCachePolicy records one project proposal about the cache policy and returns its id.
func recordCachePolicy(t *testing.T, cfg memory.Config, title string, rel *memory.RelatesTo) int64 {
	t.Helper()
	res, err := memory.Record(context.Background(), cfg, memory.RecordRequest{
		Author: "dev", Session: "ses_1", Directory: searchDirectory,
		Nature: "proposal", Scope: "project", Title: title, Content: "Cache policy option.", RelatesTo: rel,
	})
	if err != nil {
		t.Fatal(err)
	}
	return res.ID
}

func hitsByID(res memory.SearchResult) map[int64]memory.SearchHit {
	out := map[int64]memory.SearchHit{}
	for _, h := range res.Results {
		out[h.ID] = h
	}
	return out
}

func TestSearchReturnsGoverningEntriesWithTheirOpenEvolution(t *testing.T) {
	_, cfg := newFake(t)
	old := recordCachePolicy(t, cfg, "Cache for one hour", nil)
	current := recordCachePolicy(t, cfg, "Cache for ten minutes", &memory.RelatesTo{ID: old, Relation: "supersedes"})
	dispute := recordCachePolicy(t, cfg, "Cache must not expire", &memory.RelatesTo{ID: current, Relation: "disputes"})
	exception := recordCachePolicy(t, cfg, "Cache images for a day", &memory.RelatesTo{ID: current, Relation: "exception-to"})
	req := memory.SearchRequest{Directory: searchDirectory, Query: "cache policy", Scope: "project"}

	res, err := memory.Search(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	hits := hitsByID(res)
	if _, found := hits[old]; found || res.OmittedSuperseded != 1 {
		t.Fatalf("superseded #%d must be omitted and counted: %+v", old, res)
	}
	if h := hits[current]; !h.Disputed || !h.Exception || h.Superseded {
		t.Fatalf("current entry = %+v, want disputed with an exception", h)
	}
	if h := hits[dispute]; !h.Disputed {
		t.Fatalf("disputing entry = %+v, want disputed", h)
	}
	if _, found := hits[exception]; !found {
		t.Fatalf("exception #%d missing: %+v", exception, res)
	}

	req.IncludeHistory = true
	res, err = memory.Search(context.Background(), cfg, req)
	if err != nil {
		t.Fatal(err)
	}
	if h := hitsByID(res)[old]; !h.Superseded || res.OmittedSuperseded != 0 {
		t.Fatalf("history must show #%d as superseded: %+v", old, res)
	}
}

func TestSearchStaysInsideTheRequestedTier(t *testing.T) {
	_, cfg := newFake(t)
	workspace := recordCachePolicy(t, cfg, "Cache for one hour", nil)
	res, err := memory.Record(context.Background(), cfg, memory.RecordRequest{
		Author: "dev", Session: "ses_1", Directory: searchDirectory, Confirmed: true,
		Nature: "proposal", Scope: "personal", Title: "Prefers short cache policy names", Content: "Cache policy naming.",
	})
	if err != nil {
		t.Fatal(err)
	}

	global, err := memory.Search(context.Background(), cfg, memory.SearchRequest{Query: "cache policy", Scope: "personal"})
	if err != nil {
		t.Fatal(err)
	}
	if hits := hitsByID(global); len(hits) != 1 || hits[res.ID].ID != res.ID {
		t.Fatalf("personal search = %+v, want only #%d", global, res.ID)
	}
	project, err := memory.Search(context.Background(), cfg, memory.SearchRequest{Directory: searchDirectory, Query: "cache policy", Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if hits := hitsByID(project); len(hits) != 1 || hits[workspace].ID != workspace {
		t.Fatalf("project search = %+v, want only #%d", project, workspace)
	}
	every, err := memory.Search(context.Background(), cfg, memory.SearchRequest{Query: "cache policy", Scope: "project", AllWorkspaces: true})
	if err != nil {
		t.Fatal(err)
	}
	if hits := hitsByID(every); len(hits) != 1 || hits[workspace].ID != workspace {
		t.Fatalf("all-workspaces search = %+v, want only #%d", every, workspace)
	}
}

func TestSearchFiltersByType(t *testing.T) {
	_, cfg := newFake(t)
	recordCachePolicy(t, cfg, "Cache for one hour", nil)
	res, err := memory.Search(context.Background(), cfg, memory.SearchRequest{Directory: searchDirectory, Query: "ses_1", Scope: "project", Type: "session_anchor"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].Type != "session_anchor" {
		t.Fatalf("anchor search = %+v, want the session start anchor alone", res)
	}
}

func TestSearchRejectsInvalidRequests(t *testing.T) {
	_, cfg := newFake(t)
	for name, req := range map[string]memory.SearchRequest{
		"empty query":           {Directory: searchDirectory, Query: " ", Scope: "project"},
		"unknown scope":         {Directory: searchDirectory, Query: "cache", Scope: "team"},
		"project, no directory": {Query: "cache", Scope: "project"},
		"negative limit":        {Directory: searchDirectory, Query: "cache", Scope: "project", Limit: -1},
	} {
		var invalid *memory.ValidationError
		if _, err := memory.Search(context.Background(), cfg, req); !errors.As(err, &invalid) {
			t.Errorf("%s: err = %v, want a validation error", name, err)
		}
	}
}
