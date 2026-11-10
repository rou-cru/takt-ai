package memory

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"math"

	"github.com/rou-cru/takt-ai/takt/memory/embed"
	// Importing vec installs sqlite-vec through sqlite3_auto_extension (vec/patches.go:44-47), which
	// makes vec_distance_cosine available on every connection opened afterwards.
	_ "modernc.org/sqlite/vec"
)

// embedBatch bounds the bodies embedded per write transaction, so each write holds the lock briefly.
const embedBatch = 64

// float32Bytes is the width of one vector component in the stored BLOB.
const float32Bytes = 4

// encodeVector returns the little-endian float32 BLOB sqlite-vec's scalar functions take.
func encodeVector(v []float32) []byte {
	out := make([]byte, float32Bytes*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[float32Bytes*i:], math.Float32bits(x))
	}
	return out
}

type pendingBody struct {
	entry int64
	text  string
}

// EmbedPending embeds every body without a chunk of the loaded model's identity, and stores one chunk
// per body section tagged with that identity.
// Embedding runs outside any write transaction. With a nil model nothing is embedded and writes and
// lexical search are unaffected.
func (s *Store) EmbedPending(ctx context.Context, model *embed.Model) error {
	if model == nil {
		return nil
	}
	for {
		batch, err := s.pendingBodies(ctx)
		if err != nil || len(batch) == 0 {
			return err
		}
		type chunk struct {
			entry   int64
			ordinal int
			vector  []byte
		}
		var chunks []chunk
		for _, b := range batch {
			if err := ctx.Err(); err != nil {
				return err
			}
			for i, sec := range splitSections(b.text) {
				chunks = append(chunks, chunk{b.entry, i, encodeVector(model.Embed(sec.Text))})
			}
		}
		err = s.write(ctx, func(tx *sql.Tx) error {
			for _, c := range chunks {
				if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO chunks (entry_id, ordinal, model_identity, vector) VALUES (?, ?, ?, ?)`,
					c.entry, c.ordinal, embed.ModelIdentity, c.vector); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
}

// pendingBodies returns up to embedBatch bodies of entries that have no chunk of the model.
func (s *Store) pendingBodies(ctx context.Context) (out []pendingBody, err error) {
	rows, err := s.reader.QueryContext(ctx, `SELECT e.id, e.body FROM entries e
		WHERE NOT EXISTS (SELECT 1 FROM chunks c WHERE c.entry_id = e.id AND c.model_identity = ?)
		LIMIT ?`, embed.ModelIdentity, embedBatch)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	for rows.Next() {
		var b pendingBody
		if err := rows.Scan(&b.entry, &b.text); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Coverage returns how many entries have a chunk of the model identity search uses, and how many entries
// exist, for status (MEM-RET-9). Before any model is recorded no entry is covered.
func (s *Store) Coverage(ctx context.Context) (covered, total int64, err error) {
	tx, err := s.reader.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries`).Scan(&total); err != nil {
		return 0, 0, err
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries e WHERE EXISTS (
		SELECT 1 FROM chunks c WHERE c.entry_id = e.id AND c.model_identity = ?)`, embed.ModelIdentity).Scan(&covered)
	return covered, total, err
}
