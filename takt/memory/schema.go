package memory

import (
	"context"
	"database/sql"
	"fmt"
)

// schemaVersion is stored in user_version; a store with a larger value is refused (PR-MSV-36).
const schemaVersion = 1

// ftsTokenizer is shared by every full-text index of the store.
const ftsTokenizer = "tokenize = 'unicode61 remove_diacritics 2'"

// schemaStatements lists the tables of the store. Timestamps are Unix milliseconds.
var schemaStatements = []string{
	`CREATE TABLE entries (
		id INTEGER PRIMARY KEY,
		session TEXT NOT NULL,
		writer_host_session TEXT NOT NULL,
		author TEXT NOT NULL,
		workspace TEXT NOT NULL,
		tier TEXT NOT NULL,
		nature TEXT NOT NULL,
		title TEXT NOT NULL,
		body TEXT NOT NULL,
		evidence TEXT NOT NULL,
		user_order INTEGER NOT NULL,
		grant TEXT NOT NULL,
		created_at INTEGER NOT NULL
	)`,
	`CREATE TABLE relations (
		source INTEGER NOT NULL,
		target INTEGER NOT NULL,
		kind TEXT NOT NULL
	)`,
	`CREATE INDEX relations_target ON relations (target)`,
	`CREATE INDEX relations_source ON relations (source)`,
	`CREATE TABLE partitions (
		id INTEGER PRIMARY KEY,
		tier TEXT NOT NULL,
		workspace_key TEXT NOT NULL,
		UNIQUE (tier, workspace_key)
	)`,
	`CREATE TABLE current (
		entry_id INTEGER PRIMARY KEY,
		partition_id INTEGER NOT NULL
	)`,
	`CREATE INDEX current_partition ON current (partition_id)`,
	`CREATE INDEX entries_session ON entries (session, id)`,
	`CREATE TABLE sessions (
		id TEXT PRIMARY KEY,
		workspace TEXT NOT NULL,
		continuity_target TEXT NOT NULL
	)`,
	`CREATE INDEX sessions_workspace ON sessions (workspace)`,
	`CREATE TABLE bindings (
		host_session TEXT PRIMARY KEY,
		root TEXT NOT NULL,
		agent TEXT NOT NULL,
		workspace TEXT NOT NULL,
		pending_continuity TEXT NOT NULL,
		pending_grants TEXT NOT NULL
	)`,
	`CREATE INDEX bindings_root ON bindings (root)`,
	`CREATE TABLE closes (
		session TEXT NOT NULL,
		first_sequence INTEGER NOT NULL,
		last_sequence INTEGER NOT NULL,
		objective TEXT NOT NULL,
		state TEXT NOT NULL
	)`,
	`CREATE INDEX closes_session ON closes (session)`,
	`CREATE VIRTUAL TABLE closes_fts USING fts5(objective, state, ` + ftsTokenizer + `)`,
	`CREATE TABLE chunks (
		entry_id INTEGER NOT NULL,
		ordinal INTEGER NOT NULL,
		model_identity TEXT NOT NULL,
		vector BLOB NOT NULL,
		PRIMARY KEY (entry_id, model_identity, ordinal)
	)`,
}

// applySchema creates every table and stamps the schema version.
func applySchema(ctx context.Context, tx *sql.Tx) error {
	for _, stmt := range schemaStatements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", schemaVersion))
	return err
}

// createPartition inserts the partition row for a tier and workspace key, and its two
// full-text tables over title and body, named by the partition ID (rowid = entry ID).
// It runs inside the caller's write transaction.
func createPartition(ctx context.Context, tx *sql.Tx, tier, workspaceKey string) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO partitions (tier, workspace_key) VALUES (?, ?)`, tier, workspaceKey)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, kind := range []string{"current", "superseded"} {
		ddl := fmt.Sprintf(`CREATE VIRTUAL TABLE fts_%s_%d USING fts5(title, body, %s)`, kind, id, ftsTokenizer)
		if _, err := tx.ExecContext(ctx, ddl); err != nil {
			return 0, err
		}
	}
	return id, nil
}
