package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/rou-cru/takt-ai/takt/memory/embed"
)

// Search modes.
const (
	ModeLexical  = "lexical"
	ModeSemantic = "semantic"
)

// dateLayout is how hit and read lines show an entry's date.
const dateLayout = "2006-01-02"

// Kinds of full-text index and of candidate set: the current set, and the superseded history.
const (
	kindCurrent    = "current"
	kindSuperseded = "superseded"
)

// SearchArgs are the arguments of a search. Scope is required; Mode defaults to lexical and
// Limit to defaultSearchLimit.
type SearchArgs struct {
	Query          string `json:"query" jsonschema:"Terms to match in lexical mode, or the concept to find in semantic mode."`
	Mode           string `json:"mode,omitempty" jsonschema:"lexical, the default: matches names, IDs, and exact terms. semantic: matches concepts and paraphrases, and also reports how many entries in scope have no semantic representation yet, which lexical mode still finds by their terms. semantic is refused while the semantic model is unavailable; use lexical."`
	Scope          string `json:"scope" jsonschema:"Tier to search: workspace or global."`
	Limit          int    `json:"limit,omitempty" jsonschema:"Maximum number of hits."`
	IncludeHistory bool   `json:"include_history,omitempty" jsonschema:"Also return corrected and superseded entries, each marked [superseded by #<id>] with its current version. They are history, never current guidance."`
	AllWorkspaces  bool   `json:"all_workspaces,omitempty" jsonschema:"With workspace scope, search every workspace instead of the active one."`
}

// Hit is one search result.
type Hit struct {
	ID        int64
	Nature    string
	Tier      string
	Title     string
	Author    string
	CreatedAt int64
	Disputed  bool
	Exception bool
	// SupersededBy is the current version of a corrected or superseded entry; 0 for a current entry.
	SupersededBy int64
}

// SearchResult is the outcome of a search. Uncovered is meaningful in semantic mode only: the entries in
// scope with no semantic representation, which lexical mode still finds by their terms (MEM-RET-9).
type SearchResult struct {
	Semantic  bool
	Hits      []Hit
	Uncovered int64
}

type candidate struct {
	id    int64
	score float64
}

func formatDate(ms int64) string { return time.UnixMilli(ms).UTC().Format(dateLayout) }

// terms splits a query into the words the full-text index would tokenize it into.
func terms(query string) []string {
	return strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// ftsQuery quotes every word, so none of them acts as an FTS5 operator, and joins them with op: " OR "
// matches any of them, " AND " all of them.
func ftsQuery(words []string, op string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = `"` + strings.ReplaceAll(w, `"`, `""`) + `"`
	}
	return strings.Join(quoted, op)
}

func validateSearch(a SearchArgs) error {
	if strings.TrimSpace(a.Query) == "" {
		return refuse("query is required; give the terms or the concept to find")
	}
	if a.Mode != ModeSemantic && len(terms(a.Query)) == 0 {
		return refuse("query has no searchable terms; use words or numbers, or mode semantic")
	}
	if a.Limit < 0 {
		return refuse("limit %d is not valid; use a positive number, or omit it for %d", a.Limit, defaultSearchLimit)
	}
	if a.AllWorkspaces && a.Scope != ScopeWorkspace {
		return refuse("all_workspaces applies only to scope workspace; use scope workspace, or drop all_workspaces")
	}
	return nil
}

// Search finds entries as the identity. Scope and applicability select the full-text
// indexes or candidate sets before any ranking and before any body is read (MEM-RET-6); results are
// ordered by relevance, the higher sequence first on equal scores (MEM-AUT-5). Semantic mode needs
// the loaded model and is refused naming lexical mode when model is nil. Every refusal is a *RefusalError.
func (s *Store) Search(ctx context.Context, id Identity, a SearchArgs, model *embed.Model) (SearchResult, error) {
	if err := validateSearch(a); err != nil {
		return SearchResult{}, err
	}
	semantic := a.Mode == ModeSemantic
	if semantic && model == nil {
		return SearchResult{}, refuse("semantic search is unavailable because the semantic model is not loaded; use mode lexical")
	}
	limit := a.Limit
	if limit == 0 {
		limit = defaultSearchLimit
	}
	tx, err := s.reader.BeginTx(ctx, nil)
	if err != nil {
		return SearchResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	w, err := resolveWriter(ctx, tx, id)
	if err != nil {
		return SearchResult{}, err
	}
	partitions, err := selectPartitions(ctx, tx, w, a)
	if err != nil {
		return SearchResult{}, err
	}
	kinds := []string{kindCurrent}
	if a.IncludeHistory {
		kinds = append(kinds, kindSuperseded)
	}
	res := SearchResult{Semantic: semantic}
	var found []candidate
	var vector []byte
	if semantic {
		vector = encodeVector(model.Embed(a.Query))
	}
	for _, p := range partitions {
		for _, kind := range kinds {
			var part []candidate
			if semantic {
				part, err = semanticCandidates(ctx, tx, p, kind, vector, limit)
			} else {
				part, err = lexicalCandidates(ctx, tx, p, kind, ftsQuery(terms(a.Query), " OR "), limit)
			}
			if err != nil {
				return SearchResult{}, err
			}
			found = append(found, part...)
			if semantic {
				n, err := countUncovered(ctx, tx, p, kind)
				if err != nil {
					return SearchResult{}, err
				}
				res.Uncovered += n
			}
		}
	}
	sort.Slice(found, func(i, j int) bool {
		if found[i].score != found[j].score {
			return found[i].score > found[j].score
		}
		return found[i].id > found[j].id
	})
	if len(found) > limit {
		found = found[:limit]
	}
	for _, c := range found {
		h, err := loadHit(ctx, tx, c.id)
		if err != nil {
			return SearchResult{}, err
		}
		res.Hits = append(res.Hits, h)
	}
	return res, nil
}

// selectPartitions returns the partitions the scope selects for the writer's workspace; the global tier
// has one, the workspace tier one per workspace unless all_workspaces is set.
func selectPartitions(ctx context.Context, tx *sql.Tx, w writer, a SearchArgs) (ids []int64, err error) {
	q := `SELECT id FROM partitions WHERE tier = ?`
	args := []any{a.Scope}
	if a.Scope == ScopeWorkspace && !a.AllWorkspaces {
		if w.workspace == "" {
			return nil, refuse("the workspace tier needs a workspace; start the adapter with %s <dir>, use scope global, or set all_workspaces", workspaceFlag)
		}
		q += ` AND workspace_key = ?`
		args = append(args, w.workspace)
	}
	rows, err := tx.QueryContext(ctx, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func scanCandidates(rows *sql.Rows, err error) (out []candidate, scanErr error) {
	if err != nil {
		return nil, err
	}
	defer func() { scanErr = errors.Join(scanErr, rows.Close()) }()
	for rows.Next() {
		var c candidate
		var score sql.NullFloat64
		if err := rows.Scan(&c.id, &score); err != nil {
			return nil, err
		}
		if score.Valid {
			c.score = score.Float64
			out = append(out, c)
		}
	}
	return out, rows.Err()
}

// lexicalCandidates ranks one full-text index by BM25 (higher score is better).
func lexicalCandidates(ctx context.Context, tx *sql.Tx, partition int64, kind, match string, limit int) ([]candidate, error) {
	table := fmt.Sprintf("fts_%s_%d", kind, partition)
	q := fmt.Sprintf(`SELECT rowid, -bm25(%[1]s) AS score FROM %[1]s WHERE %[1]s MATCH ? ORDER BY score DESC, rowid DESC LIMIT ?`, table)
	rows, err := tx.QueryContext(ctx, q, match, limit)
	return scanCandidates(rows, err)
}

// candidateSource is the set of entry IDs of a partition's current set or its superseded history.
func candidateSource(kind string, partition int64) string {
	if kind == kindCurrent {
		return fmt.Sprintf(`SELECT entry_id AS id FROM current WHERE partition_id = %d`, partition)
	}
	return fmt.Sprintf(`SELECT rowid AS id FROM fts_superseded_%d`, partition)
}

// semanticCandidates scores each candidate entry by its best chunk of the current model: negated cosine
// distance, so a higher score is better. Entries with no usable chunk are not candidates here.
func semanticCandidates(ctx context.Context, tx *sql.Tx, partition int64, kind string, vector []byte, limit int) ([]candidate, error) {
	q := fmt.Sprintf(`SELECT id, -d AS score FROM (
		SELECT e.id AS id, MIN(vec_distance_cosine(c.vector, ?)) AS d
		FROM (%s) s JOIN entries e ON e.id = s.id
		JOIN chunks c ON c.entry_id = e.id AND c.model_identity = ?
		GROUP BY e.id) WHERE d IS NOT NULL ORDER BY d ASC, id DESC LIMIT ?`, candidateSource(kind, partition))
	rows, err := tx.QueryContext(ctx, q, vector, embed.ModelIdentity, limit)
	return scanCandidates(rows, err)
}

// countUncovered counts the candidate entries with no chunk of the model (MEM-RET-9).
func countUncovered(ctx context.Context, tx *sql.Tx, partition int64, kind string) (int64, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM (%s) s JOIN entries e ON e.id = s.id
		WHERE NOT EXISTS (SELECT 1 FROM chunks c WHERE c.entry_id = e.id AND c.model_identity = ?)`, candidateSource(kind, partition))
	var n int64
	err := tx.QueryRowContext(ctx, q, embed.ModelIdentity).Scan(&n)
	return n, err
}

// loadHit reads a ranked entry's metadata; it runs only for entries that made the limit.
func loadHit(ctx context.Context, tx *sql.Tx, id int64) (Hit, error) {
	h := Hit{ID: id}
	var current bool
	err := tx.QueryRowContext(ctx, `SELECT e.nature, e.tier, e.title, e.author, e.created_at,
		EXISTS (SELECT 1 FROM current WHERE entry_id = e.id),
		EXISTS (SELECT 1 FROM relations r WHERE r.kind = ? AND (r.source = e.id OR r.target = e.id)),
		EXISTS (SELECT 1 FROM relations r WHERE r.kind = ? AND (r.source = e.id OR r.target = e.id))
		FROM entries e WHERE e.id = ?`, RelationDisputes, RelationExceptionTo, id).
		Scan(&h.Nature, &h.Tier, &h.Title, &h.Author, &h.CreatedAt, &current, &h.Disputed, &h.Exception)
	if err != nil {
		return Hit{}, err
	}
	if !current {
		if h.SupersededBy, err = chainHead(ctx, tx, id); err != nil {
			return Hit{}, err
		}
	}
	return h, nil
}

// Text formats the result as the text the search tool returns: one line per hit and, in semantic mode,
// the count of entries in scope without a semantic representation.
func (r SearchResult) Text() string {
	var b strings.Builder
	if len(r.Hits) == 0 {
		b.WriteString("No matching entries.\n")
	}
	for _, h := range r.Hits {
		fmt.Fprintf(&b, "#%d | %s | %s | %s | %s | %s", h.ID, h.Nature, h.Tier, h.Title, h.Author, formatDate(h.CreatedAt))
		if h.Disputed {
			b.WriteString(" [disputed]")
		}
		if h.Exception {
			b.WriteString(" [exception]")
		}
		if h.SupersededBy != 0 {
			fmt.Fprintf(&b, " [superseded by #%d]", h.SupersededBy)
		}
		b.WriteByte('\n')
	}
	if r.Semantic {
		fmt.Fprintf(&b, "Entries in scope without a semantic representation: %d (lexical mode finds them by their terms)\n", r.Uncovered)
	}
	return b.String()
}
