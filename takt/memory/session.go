package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// States of a listed session.
const (
	SessionClosed  = "closed"
	SessionOpen    = "open"
	SessionCurrent = "current" // the caller's own open session
)

// chronologyLimit bounds the entry lines listed per session, so a long session never floods a listing;
// the most recent entries are shown and the earlier ones are counted.
const chronologyLimit = 20

// defaultSearchLimit is the result limit when a caller sends none.
const defaultSearchLimit = 10

// RecordedClose is a recorded session close and the sequence range it covers.
type RecordedClose struct {
	Session       string
	FirstSequence int64
	LastSequence  int64
	Objective     string
	State         string
}

// isClosed defines a closed session: it was closed and committed nothing after its last close,
// so a new entry reopens it.
func isClosed(lastClose, lastEntry int64) bool { return lastClose > 0 && lastClose >= lastEntry }

// CloseSession records the objective, the state, and the sequence range of the caller's session
// entries committed since its previous close, and clears the unconsumed grants of its bindings.
// A crew caller must be the root of its own binding; an external caller
// closes its own session. Nothing new to close is refused and records nothing.
func (s *Store) CloseSession(ctx context.Context, id Identity, objective, state string) (RecordedClose, error) {
	if strings.TrimSpace(objective) == "" || strings.TrimSpace(state) == "" {
		return RecordedClose{}, refuse("objective and state are both required; state what the session set out to do and where it stands")
	}
	var c RecordedClose
	err := s.write(ctx, func(tx *sql.Tx) error {
		w, err := resolveWriter(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := requireRoot(id, w, ToolCloseSession); err != nil {
			return err
		}
		var first, last sql.NullInt64
		err = tx.QueryRowContext(ctx, `SELECT MIN(id), MAX(id) FROM entries WHERE session = ?
			AND id > (SELECT COALESCE(MAX(last_sequence), 0) FROM closes WHERE session = ?)`, w.session, w.session).Scan(&first, &last)
		if err != nil {
			return err
		}
		if !first.Valid {
			return refuse("nothing new to close: this session has recorded no entry since its last close, so no close is needed")
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO closes (session, first_sequence, last_sequence, objective, state)
			VALUES (?, ?, ?, ?, ?)`, w.session, first.Int64, last.Int64, objective, state)
		if err != nil {
			return err
		}
		rowid, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO closes_fts (rowid, objective, state) VALUES (?, ?, ?)`, rowid, objective, state); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE bindings SET pending_grants = ? WHERE root = ?`, emptyGrants, w.root); err != nil {
			return err
		}
		c = RecordedClose{Session: w.session, FirstSequence: first.Int64, LastSequence: last.Int64, Objective: objective, State: state}
		return nil
	})
	return c, err
}

// requireRoot refuses a crew caller that is not the root of its own binding; an external
// caller always acts on its own session.
func requireRoot(id Identity, w writer, tool string) error {
	if id.Principal == PrincipalCrew && w.root != id.HostSession {
		return refuse("%s is accepted only from the root session of the conversation, which makes this call; continue without it", tool)
	}
	return nil
}

// ContinueSession declares, replaces, or (with an empty previousID) withdraws the pending
// continuity of the caller's session, and returns the pending declaration
// (MEM-OPS-5). The target must be a closed session, of any workspace; the external principal
// cannot name a crew session. Once the caller's session has its first entry, the declaration is fixed
// and any further call is refused. An external caller keeps its pending declaration in a
// bindings row keyed by its own session key.
func (s *Store) ContinueSession(ctx context.Context, id Identity, previousID string) (string, error) {
	err := s.write(ctx, func(tx *sql.Tx) error {
		w, err := resolveWriter(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := requireRoot(id, w, ToolContinueSession); err != nil {
			return err
		}
		var exists int
		err = tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, w.session).Scan(&exists)
		if err == nil {
			return refuse("this session already has an entry, so its continuity is fixed; continue without the link")
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if previousID != "" {
			if err := checkContinuationTarget(ctx, tx, id, previousID); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO bindings (host_session, root, agent, workspace, pending_continuity, pending_grants)
			VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (host_session) DO UPDATE SET pending_continuity = excluded.pending_continuity`,
			w.host, w.root, w.agent, w.workspace, previousID, emptyGrants)
		return err
	})
	if err != nil {
		return "", err
	}
	return previousID, nil
}

func checkContinuationTarget(ctx context.Context, tx *sql.Tx, id Identity, target string) error {
	if id.Principal == PrincipalExternal && strings.HasPrefix(target, sessionKey(PrincipalCrew, "")) {
		return refuse("session %q is a crew session and cannot be continued from this connection; name a closed external session, which the %s tool lists", target, ToolSessions)
	}
	var lastClose, lastEntry int64
	err := tx.QueryRowContext(ctx, `SELECT COALESCE((SELECT MAX(last_sequence) FROM closes WHERE session = ?), 0),
		COALESCE((SELECT MAX(id) FROM entries WHERE session = ?), 0)`, target, target).Scan(&lastClose, &lastEntry)
	if err != nil {
		return err
	}
	if lastEntry == 0 {
		return refuse("session %q does not exist; take the ID from the %s tool", target, ToolSessions)
	}
	if !isClosed(lastClose, lastEntry) {
		return refuse("session %q is not closed; continuity names a closed session, so choose one the %s tool lists as closed, or resume an open session without a continuity link", target, ToolSessions)
	}
	return nil
}

// SessionClose is one close of a listed session.
type SessionClose struct {
	Objective string
	State     string
}

// SessionLine is one entry of a session chronology; it never carries a body (MEM-AUT-5).
type SessionLine struct {
	ID      int64
	Title   string
	Nature  string
	Author  string
	Current bool // false when a corrects or supersedes relation retired the entry
}

// SessionInfo is one session of a listing. ID is the value ContinueSession accepts.
type SessionInfo struct {
	ID         string
	Workspace  string
	State      string
	Closes     []SessionClose
	Omitted    int // earlier entries not shown in Chronology
	Chronology []SessionLine
}

// Sessions lists the sessions that have records in the caller's workspace, or in every workspace,
// most recent first. A query keeps the session it names by ID, the sessions whose close objectives
// or states match, and those whose entry titles contain every query word; close matches come first, recency breaks ties. A
// limit of zero or less means defaultSearchLimit. Bindings, pending continuity, and grants are never listed.
func (s *Store) Sessions(ctx context.Context, id Identity, query string, allWorkspaces bool, limit int) ([]SessionInfo, error) {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	tx, err := s.reader.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	w, err := resolveWriter(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	q, args := sessionsQuery(query, allWorkspaces, w.workspace, limit)
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	var out []SessionInfo
	lastEntries := map[string]int64{}
	for rows.Next() {
		var info SessionInfo
		var hit, last int64
		if err := rows.Scan(&info.ID, &info.Workspace, &hit, &last); err != nil {
			return nil, errors.Join(err, rows.Close())
		}
		lastEntries[info.ID] = last
		out = append(out, info)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return nil, err
	}
	for i := range out {
		if err := fillSession(ctx, tx, &out[i], lastEntries[out[i].ID], w.session); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// sessionsQuery builds the listing query: columns are session, workspace, close-match flag, last entry.
func sessionsQuery(query string, all bool, workspace string, limit int) (string, []any) {
	words := strings.Fields(query)
	hit := "0"
	where := ""
	var args []any
	args = append(args, all, workspace)
	if len(words) > 0 {
		titles := make([]string, len(words))
		var likes []any
		for i, word := range words {
			titles[i] = `t.title LIKE ? ESCAPE '\'`
			likes = append(likes, "%"+likeEscaper.Replace(word)+"%")
		}
		hit = `CASE WHEN s.id IN (SELECT c.session FROM closes c WHERE c.rowid IN
			(SELECT rowid FROM closes_fts WHERE closes_fts MATCH ?)) THEN 1 ELSE 0 END`
		where = ` AND (hit = 1 OR s.id = ? OR EXISTS (SELECT 1 FROM entries t WHERE t.session = s.id AND ` + strings.Join(titles, " AND ") + `))`
		args = append([]any{ftsQuery(words, " AND ")}, args...)
		args = append(args, strings.TrimSpace(query))
		args = append(args, likes...)
	}
	args = append(args, limit)
	return `SELECT s.id, s.workspace, ` + hit + ` AS hit, MAX(e.id) AS last FROM sessions s JOIN entries e ON e.session = s.id
		WHERE (? OR s.workspace = ?)` + where + ` GROUP BY s.id ORDER BY hit DESC, last DESC LIMIT ?`, args
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// fillSession loads the closes and the bounded chronology of a listed session and sets its state.
func fillSession(ctx context.Context, tx *sql.Tx, info *SessionInfo, lastEntry int64, own string) error {
	var lastClose int64
	rows, err := tx.QueryContext(ctx, `SELECT objective, state, last_sequence FROM closes WHERE session = ? ORDER BY rowid`, info.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var c SessionClose
		if err := rows.Scan(&c.Objective, &c.State, &lastClose); err != nil {
			return errors.Join(err, rows.Close())
		}
		info.Closes = append(info.Closes, c)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	switch {
	case isClosed(lastClose, lastEntry):
		info.State = SessionClosed
	case info.ID == own:
		info.State = SessionCurrent
	default:
		info.State = SessionOpen
	}
	rows, err = tx.QueryContext(ctx, `SELECT e.id, e.title, e.nature, e.author, c.entry_id IS NOT NULL
		FROM entries e LEFT JOIN current c ON c.entry_id = e.id WHERE e.session = ? ORDER BY e.id DESC LIMIT ?`, info.ID, chronologyLimit)
	if err != nil {
		return err
	}
	for rows.Next() {
		var l SessionLine
		if err := rows.Scan(&l.ID, &l.Title, &l.Nature, &l.Author, &l.Current); err != nil {
			return errors.Join(err, rows.Close())
		}
		info.Chronology = append([]SessionLine{l}, info.Chronology...)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return err
	}
	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM entries WHERE session = ?`, info.ID).Scan(&total); err != nil {
		return err
	}
	info.Omitted = total - len(info.Chronology)
	return nil
}

// FormatSessions renders a listing as the text the sessions tool returns.
func FormatSessions(list []SessionInfo) string {
	if len(list) == 0 {
		return "No sessions."
	}
	var b strings.Builder
	for _, info := range list {
		fmt.Fprintf(&b, "%s [%s]", info.ID, info.State)
		if info.Workspace != "" {
			fmt.Fprintf(&b, " workspace %s", info.Workspace)
		}
		b.WriteString("\n")
		for _, c := range info.Closes {
			fmt.Fprintf(&b, "  close: %s | %s\n", c.Objective, c.State)
		}
		if info.Omitted > 0 {
			fmt.Fprintf(&b, "  (%d earlier entries not shown)\n", info.Omitted)
		}
		for _, l := range info.Chronology {
			applicability := "current"
			if !l.Current {
				applicability = "superseded"
			}
			fmt.Fprintf(&b, "  #%d %s (%s, %s, %s)\n", l.ID, l.Title, l.Nature, l.Author, applicability)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}
