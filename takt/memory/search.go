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
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// judged is the Engram status of a relation recorded as a verdict; only a
// judged relation changes whether an entry governs.
const judged = "judged"

// defaultSearchLimit is the governing-entry count a request without a limit receives.
const defaultSearchLimit = 10

// SearchRequest is one memory search made on behalf of a crew member.
type SearchRequest struct {
	Directory      string `json:"directory"`
	Query          string `json:"query"`
	Scope          string `json:"scope"`
	Type           string `json:"type,omitempty"`
	Limit          int    `json:"limit,omitempty"`
	IncludeHistory bool   `json:"include_history,omitempty"`
	// AllWorkspaces searches the workspace tier of every project.
	AllWorkspaces bool `json:"all_workspaces,omitempty"`
}

// SearchHit is one matching entry with the applicability its recorded
// evolution gives it.
type SearchHit struct {
	ID         int64  `json:"id"`
	Type       string `json:"type"`
	Title      string `json:"title"`
	Preview    string `json:"preview"`
	Project    string `json:"project,omitempty"`
	Superseded bool   `json:"superseded,omitempty"`
	Disputed   bool   `json:"disputed,omitempty"`
	Exception  bool   `json:"exception,omitempty"`
}

// SearchResult lists the matching entries; superseded entries are left out
// and counted unless the request asked for history.
type SearchResult struct {
	Results           []SearchHit `json:"results"`
	OmittedSuperseded int         `json:"omitted_superseded"`
}

// compactRelation is one relation Engram attaches to a compact search hit.
type compactRelation struct {
	Relation       string `json:"relation"`
	JudgmentStatus string `json:"judgment_status"`
}

// compactHit is one entry of Engram's compact search response.
type compactHit struct {
	ID        int64  `json:"id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Preview   string `json:"preview"`
	Project   string `json:"project"`
	Relations struct {
		AsSource []compactRelation `json:"as_source"`
		AsTarget []compactRelation `json:"as_target"`
	} `json:"relations"`
}

// Search runs a full-text memory search in one tier and returns the entries
// that still govern, marking open disputes and scoped exceptions.
func Search(ctx context.Context, cfg Config, req SearchRequest) (SearchResult, error) {
	if err := validateSearch(req); err != nil {
		return SearchResult{}, err
	}
	c := newClient(cfg)
	if err := c.ensureServe(ctx); err != nil {
		return SearchResult{}, err
	}
	query := url.Values{"q": {req.Query}, "scope": {req.Scope}, "response_format": {"compact"}}
	if tierOf(req.Scope) == tierGlobal || req.AllWorkspaces {
		query.Set("all_projects", "true")
	} else {
		query.Set("project", c.project(ctx, req.Directory))
	}
	if req.Type != "" {
		query.Set("type", req.Type)
	}
	limit := req.Limit
	if limit == 0 {
		limit = defaultSearchLimit
	}
	// Engram applies its limit before Takt knows which hits are superseded:
	// widen the window by the hits it hid until the limit is met or Engram runs out.
	for fetch := limit; ; {
		query.Set("limit", strconv.Itoa(fetch))
		var page struct {
			Results []compactHit `json:"results"`
		}
		if _, err := c.call(ctx, http.MethodGet, "/search?"+query.Encode(), nil, &page); err != nil {
			return SearchResult{}, err
		}
		out := governing(page.Results, req.IncludeHistory)
		if len(out.Results) >= limit || len(page.Results) < fetch {
			out.Results = out.Results[:min(limit, len(out.Results))]
			return out, nil
		}
		fetch += out.OmittedSuperseded
	}
}

// governing marks each hit's applicability and, unless history is wanted,
// leaves the superseded ones out and counts them.
func governing(hits []compactHit, includeHistory bool) SearchResult {
	out := SearchResult{Results: []SearchHit{}}
	for _, h := range hits {
		hit := SearchHit{
			ID: h.ID, Type: h.Type, Title: h.Title, Preview: h.Preview, Project: h.Project,
			Superseded: hasJudged(h.Relations.AsTarget, edgeSupersedes),
			Disputed:   hasJudged(h.Relations.AsSource, edgeConflicts) || hasJudged(h.Relations.AsTarget, edgeConflicts),
			Exception:  hasJudged(h.Relations.AsTarget, edgeScoped),
		}
		if hit.Superseded && !includeHistory {
			out.OmittedSuperseded++
			continue
		}
		out.Results = append(out.Results, hit)
	}
	return out
}

func validateSearch(req SearchRequest) error {
	if strings.TrimSpace(req.Query) == "" {
		return reject("query is required")
	}
	if err := validateScope(req.Scope); err != nil {
		return err
	}
	if req.Scope == scopeProject && !req.AllWorkspaces && strings.TrimSpace(req.Directory) == "" {
		return reject("directory is required for a project search")
	}
	if req.Limit < 0 {
		return reject("limit must not be negative")
	}
	return nil
}

func hasJudged(relations []compactRelation, edge string) bool {
	for _, r := range relations {
		if r.Relation == edge && r.JudgmentStatus == judged {
			return true
		}
	}
	return false
}
