// Package memory is Takt's own memory store: a SQLite database owned by the memory service.
package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// StoreFile is the name of the store file inside the memory directory.
const StoreFile = "memory.db"

// busyWaitMillis bounds how long a write waits for the lock before it fails as retryable.
const busyWaitMillis = 5000

// driverName is the name modernc.org/sqlite registers with database/sql.
const driverName = "sqlite"

// storeFileMode keeps the store private to the local user.
const storeFileMode = 0o600

// sqliteResultMask strips the extended bits from a SQLite result code.
const sqliteResultMask = 0xff

var (
	// ErrNoStore reports that the memory directory holds no store file.
	ErrNoStore = errors.New("the Takt memory store does not exist; create it with init")
	// ErrStoreExists reports that a store file already exists; init never replaces one.
	ErrStoreExists = errors.New("memory store already exists; open it instead of running init")
	// ErrNewerStore reports a store whose schema version is newer than this build supports; the file is left unchanged.
	ErrNewerStore = errors.New("the Takt memory store schema is newer than this build; use a newer takt-ai")
	// ErrCorrupt reports a store that fails its integrity check or is not a store of this schema; the file is left unchanged.
	ErrCorrupt = errors.New("the Takt memory store is corrupt; restore it from a backup")
	// ErrBusy reports a write that could not take the write lock in time; it had no effect and can be retried.
	ErrBusy = errors.New("the Takt memory store is busy; retry the call")
)

// Store is an open memory store: one writer connection and a pool of readers.
type Store struct {
	writer *sql.DB
	reader *sql.DB
}

func storePath(dir string) string { return filepath.Join(dir, StoreFile) }

func dsn(path string, query url.Values) string {
	u := url.URL{Scheme: "file", OmitHost: true, Path: path, RawQuery: query.Encode()}
	return u.String()
}

func busyPragma() string { return fmt.Sprintf("busy_timeout(%d)", busyWaitMillis) }

// openWriter opens the single writer connection in WAL mode; write transactions take the lock up front.
func openWriter(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", busyPragma())
	q.Add("_pragma", "journal_mode(WAL)")
	q.Set("_txlock", "immediate")
	db, err := sql.Open(driverName, dsn(path, q))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

func openReader(path string) (*sql.DB, error) {
	q := url.Values{}
	q.Set("mode", "ro")
	q.Add("_pragma", busyPragma())
	db, err := sql.Open(driverName, dsn(path, q))
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, errors.Join(err, db.Close())
	}
	return db, nil
}

// mapBusy turns a SQLite busy error into ErrBusy.
func mapBusy(err error) error {
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code()&sqliteResultMask == sqlite3.SQLITE_BUSY {
		return fmt.Errorf("%w: %v", ErrBusy, err)
	}
	return err
}

// write runs fn in a write transaction on the single writer connection. A busy lock fails as ErrBusy; any error rolls back.
func (s *Store) write(ctx context.Context, fn func(*sql.Tx) error) error {
	return writeTx(ctx, s.writer, fn)
}

func writeTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return mapBusy(err)
	}
	if err := fn(tx); err != nil {
		return mapBusy(errors.Join(err, tx.Rollback()))
	}
	return mapBusy(tx.Commit())
}

// asCorrupt reports the SQLite corruption codes as ErrCorrupt. Every other failure,
// such as a denied permission, an I/O error or a held lock, is not corruption: it
// passes through unchanged, so only a damaged store tells the user to restore a backup.
func asCorrupt(err error) error {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() & sqliteResultMask {
		case sqlite3.SQLITE_CORRUPT, sqlite3.SQLITE_NOTADB:
			return fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
	}
	return mapBusy(err)
}

// verify checks the store read-only, so a refused file is never modified.
func verify(path string) (err error) {
	db, err := openReader(path)
	if err != nil {
		return asCorrupt(err)
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return asCorrupt(err)
	}
	if version > schemaVersion {
		return fmt.Errorf("%w (store %d, build %d)", ErrNewerStore, version, schemaVersion)
	}
	if version != schemaVersion {
		return fmt.Errorf("%w: schema version %d", ErrCorrupt, version)
	}
	rows, err := db.Query("PRAGMA quick_check")
	if err != nil {
		return asCorrupt(err)
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return asCorrupt(err)
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return asCorrupt(err)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrCorrupt, strings.Join(problems, "; "))
	}
	return nil
}

// Open opens the existing store in the existing memory directory dir. It never creates the
// directory or the store: it returns ErrNoStore when there is no store file, ErrNewerStore
// for a newer schema, and ErrCorrupt when the integrity check fails, leaving the file unchanged.
func Open(dir string) (*Store, error) {
	path := storePath(dir)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoStore
		}
		return nil, err
	}
	if err := verify(path); err != nil {
		return nil, err
	}
	writer, err := openWriter(path)
	if err != nil {
		return nil, mapBusy(err)
	}
	reader, err := openReader(path)
	if err != nil {
		return nil, errors.Join(err, writer.Close())
	}
	return &Store{writer: writer, reader: reader}, nil
}

// Init creates the store in the existing memory directory dir. It returns ErrStoreExists
// when a store file is already there and never replaces it.
func Init(dir string) error {
	path := storePath(dir)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, storeFileMode)
	if errors.Is(err, os.ErrExist) {
		return ErrStoreExists
	}
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return errors.Join(err, os.Remove(path))
	}
	if err := initSchema(path); err != nil {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			// The sidecar files exist only while the database was open.
			if rerr := os.Remove(path + suffix); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				err = errors.Join(err, rerr)
			}
		}
		return err
	}
	return nil
}

func initSchema(path string) (err error) {
	db, err := openWriter(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, db.Close()) }()
	return writeTx(context.Background(), db, func(tx *sql.Tx) error {
		return applySchema(context.Background(), tx)
	})
}

// Backup writes a consistent copy of the store with VACUUM INTO to path, which must be
// absolute and must not exist yet, and returns the path and its size in bytes. The copy is private to the
// local user like the store: the target is created exclusively with storeFileMode, which VACUUM INTO accepts empty.
func (s *Store) Backup(ctx context.Context, path string) (string, int64, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, storeFileMode)
	if err != nil {
		return "", 0, err
	}
	if err := f.Close(); err != nil {
		return "", 0, errors.Join(err, os.Remove(path))
	}
	if _, err := s.reader.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
		return "", 0, errors.Join(err, os.Remove(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, err
	}
	return path, info.Size(), nil
}

// Close closes the writer and reader connections.
func (s *Store) Close() error {
	return errors.Join(s.reader.Close(), s.writer.Close())
}
