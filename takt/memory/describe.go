package memory

import (
	"context"
	"database/sql"
	"errors"
)

// DescribedRelation is the entry at the other end of a relation and its kind.
type DescribedRelation struct {
	Entry int64  `json:"entry"`
	Kind  string `json:"kind"`
}

// Described is what Describe reports for one ID. CurrentVersion is
// the head of the entry's corrects/supersedes chain, the entry itself when it was never revised.
type Described struct {
	ID                int64               `json:"id"`
	Exists            bool                `json:"exists"`
	WriterHostSession string              `json:"writer_host_session,omitempty"`
	Title             string              `json:"title,omitempty"`
	Outgoing          []DescribedRelation `json:"outgoing,omitempty"`
	CurrentVersion    int64               `json:"current_version,omitempty"`
}

// DescribeResult holds one Described per requested ID, in order.
type DescribeResult struct {
	Entries []Described `json:"entries"`
}

// Describe reports the metadata of each entry ID, with its outgoing relations and its
// current version. An unknown ID is reported as not existing.
func (s *Store) Describe(ctx context.Context, ids []int64) (DescribeResult, error) {
	tx, err := s.reader.BeginTx(ctx, nil)
	if err != nil {
		return DescribeResult{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var res DescribeResult
	for _, id := range ids {
		d := Described{ID: id}
		err := tx.QueryRowContext(ctx, `SELECT writer_host_session, title FROM entries WHERE id = ?`, id).
			Scan(&d.WriterHostSession, &d.Title)
		if errors.Is(err, sql.ErrNoRows) {
			res.Entries = append(res.Entries, d)
			continue
		}
		if err != nil {
			return DescribeResult{}, err
		}
		d.Exists = true
		if d.Outgoing, err = describeRelations(ctx, tx, `SELECT target, kind FROM relations WHERE source = ? ORDER BY target, kind`, id); err != nil {
			return DescribeResult{}, err
		}
		if d.CurrentVersion, err = chainHead(ctx, tx, id); err != nil {
			return DescribeResult{}, err
		}
		res.Entries = append(res.Entries, d)
	}
	return res, nil
}

func describeRelations(ctx context.Context, tx *sql.Tx, query string, id int64) (out []DescribedRelation, err error) {
	rows, err := tx.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var r DescribedRelation
		if err := rows.Scan(&r.Entry, &r.Kind); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// chainHead follows corrects and supersedes relations from an entry to the head of its chain.
func chainHead(ctx context.Context, tx *sql.Tx, id int64) (int64, error) {
	for {
		var next int64
		err := tx.QueryRowContext(ctx, `SELECT source FROM relations WHERE target = ? AND kind IN (?, ?) ORDER BY source LIMIT 1`,
			id, RelationCorrects, RelationSupersedes).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			return id, nil
		}
		if err != nil {
			return 0, err
		}
		id = next
	}
}
