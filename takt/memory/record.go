package memory

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Natures of an entry.
const (
	NatureProposal    = "proposal"
	NatureDecision    = "decision"
	NatureObservation = "observation"
	NatureHypothesis  = "hypothesis"
)

// Scopes of an entry; each names the tier the entry lives in.
const (
	ScopeWorkspace = "workspace"
	ScopeGlobal    = "global"
)

// Relation kinds between entries (MEM-SCP-4).
const (
	RelationCorrects    = "corrects"
	RelationSupersedes  = "supersedes"
	RelationSupplements = "supplements"
	RelationExceptionTo = "exception-to"
	RelationDisputes    = "disputes"
)

// Principals that reach the service.
const (
	PrincipalCrew     = "crew"
	PrincipalExternal = "external"
)

// authorExternal attributes every external record.
const authorExternal = "external"

// workspaceFlag is the flag an external client passes to get the workspace tier.
const workspaceFlag = "--workspace"

// Identity is what the service resolved for a call; the server fills it from MCP _meta and
// the model never states it (MEM-AUT-7). For the crew, HostSession is the host session and
// Workspace is ignored (the binding owns it). For the external principal, HostSession is
// the adapter's session key and Workspace the --workspace key, empty without the flag.
type Identity struct {
	Principal   string
	HostSession string
	Workspace   string
}

// RelatesTo names the entry a record relates to and how.
type RelatesTo struct {
	ID       int64  `json:"id" jsonschema:"ID of an existing entry in the same tier, taken from Takt memory results and never guessed; any other ID is refused. corrects and supersedes must name the head of that entry's chain; otherwise the call is refused, naming the head to relate to instead."`
	Relation string `json:"relation" jsonschema:"corrects: the entry was wrong. supersedes: the entry was valid and no longer governs. Both make this entry its complete new version; to revise an entry you did not write, use them only with user approval, or with verified evidence when that entry is not a decision; otherwise use disputes. supplements: a distinct artifact that adds to the entry without changing it, never a revision of the same artifact. exception-to: the entry still governs except in a stated scope. disputes: an unresolved disagreement, which neither a later entry nor a different author settles."`
}

// RecordArgs are the arguments of a record. Content is the complete body, also for a revision.
// Grants carry these same arguments.
type RecordArgs struct {
	Nature    string     `json:"nature" jsonschema:"What the entry is, fixed for its life: proposal (an option raised and not adopted, with its tradeoff), decision (a choice the user approved, with the approval as evidence), observation (a result verified against evidence), or hypothesis (an interpretation not yet verified). Recording or citing never upgrades a nature: a confirmed hypothesis becomes a new observation that supersedes it."`
	Scope     string     `json:"scope" jsonschema:"Tier of the entry. workspace: the active repository's architectural invariants, technical constraints, domain models, and verified facts. global: durable user preferences, communication habits, and environment quirks that hold across projects, never workspace content such as code, repository paths, proprietary business logic, or copies of workspace entries. When unsure, use workspace. A connection without a workspace refuses every workspace-scope call, naming how to provide one; global scope still works."`
	Title     string     `json:"title" jsonschema:"Short, searchable name of what happened or is true."`
	Content   string     `json:"content,omitempty" jsonschema:"The entry body: what happened or is true, why, and the files or artifacts involved. Keep routine entries concise; record an artifact or a user-ordered Takt memory entry whole, at any length."`
	Evidence  string     `json:"evidence,omitempty" jsonschema:"What supports the entry, distinct from its writer: a file, command, test output, entry #<id>, or the user's approval. Required for observation and decision; add it, or choose the nature the support actually allows."`
	RelatesTo *RelatesTo `json:"relates_to,omitempty" jsonschema:"The existing entry this entry relates to, and how."`
	UserOrder bool       `json:"user_order,omitempty" jsonschema:"Set only when the user directly ordered recording this entry. Such an entry may record a plan, next step, or prediction, attributed to that order."`
}

// RecordResult is the outcome of a record: Recorded #ID.
type RecordResult struct {
	ID int64
}

// RefusalError is a refused call; its message names the correction.
type RefusalError struct{ Message string }

func (e *RefusalError) Error() string { return e.Message }

func refuse(format string, args ...any) error {
	return &RefusalError{Message: fmt.Sprintf(format, args...)}
}

// sessionKey namespaces a session by principal, so a crew session cannot be addressed as external.
func sessionKey(principal, id string) string { return principal + ":" + id }

func partitionKey(tier, workspace string) string {
	if tier == ScopeGlobal {
		return ""
	}
	return workspace
}

// Natures and Relations list every valid value; the MCP input schema enums are built from them.
var (
	Natures   = []string{NatureProposal, NatureDecision, NatureObservation, NatureHypothesis}
	Relations = []string{RelationCorrects, RelationSupersedes, RelationSupplements, RelationExceptionTo, RelationDisputes}
)

func revises(r *RelatesTo) bool {
	return r != nil && (r.Relation == RelationCorrects || r.Relation == RelationSupersedes)
}

// validateArgs applies the argument rules that need no stored fact.
func validateArgs(a RecordArgs) error {
	if strings.TrimSpace(a.Title) == "" {
		return refuse("title is required; give the entry a short descriptive title")
	}
	if (a.Nature == NatureObservation || a.Nature == NatureDecision) && strings.TrimSpace(a.Evidence) == "" {
		return refuse("a %s requires evidence; state what it rests on", a.Nature)
	}
	if a.Content == "" {
		return refuse("content is required; send the complete body, also when revising with corrects or supersedes")
	}
	return nil
}

// Record writes one entry as the identity in a single write transaction. Every
// refusal is a *RefusalError naming the correction, and a busy store fails as ErrBusy.
func (s *Store) Record(ctx context.Context, id Identity, a RecordArgs) (RecordResult, error) {
	if err := validateArgs(a); err != nil {
		return RecordResult{}, err
	}
	var res RecordResult
	err := s.write(ctx, func(tx *sql.Tx) error {
		w, err := resolveWriter(ctx, tx, id)
		if err != nil {
			return err
		}
		// A crew binding always has a workspace (Bind refuses an empty one); only external can lack it.
		if a.Scope == ScopeWorkspace && w.workspace == "" {
			return refuse("the workspace tier needs a workspace; start the adapter with %s <dir>, or use scope global", workspaceFlag)
		}
		var target *entryRef
		if a.RelatesTo != nil {
			if target, err = loadTarget(ctx, tx, w, a); err != nil {
				return err
			}
		}
		if revises(a.RelatesTo) {
			if err := checkHead(ctx, tx, a.RelatesTo); err != nil {
				return err
			}
		}
		grantJSON, err := consumeGrant(ctx, tx, id, w, a)
		if err != nil {
			return err
		}
		res.ID, err = insertEntry(ctx, tx, id, w, a, target, grantJSON)
		return err
	})
	if err != nil {
		return RecordResult{}, err
	}
	return res, nil
}

// writer is the identity the service resolved for a record.
type writer struct {
	session   string // memory session key, namespaced by principal
	host      string // writer host session stored on the entry; the external key is namespaced
	root      string // binding key of the session's root: the crew root host session, or the external session key itself
	agent     string
	workspace string
}

func resolveWriter(ctx context.Context, tx *sql.Tx, id Identity) (writer, error) {
	if id.Principal == PrincipalExternal {
		key := sessionKey(id.Principal, id.HostSession)
		return writer{session: key, host: key, root: key, agent: authorExternal, workspace: id.Workspace}, nil
	}
	b, found, err := loadBinding(ctx, tx, id.HostSession)
	if err != nil {
		return writer{}, err
	}
	if !found {
		return writer{}, refuse("this session has no Takt memory binding; crew agents reach it only through the Takt crew integration")
	}
	return writer{session: sessionKey(PrincipalCrew, b.root), host: id.HostSession, root: b.root, agent: b.agent, workspace: b.workspace}, nil
}

type entryRef struct {
	id        int64
	tier      string
	title     string
	partition sql.NullInt64 // set while the entry is in the current set
}

func loadTarget(ctx context.Context, tx *sql.Tx, w writer, a RecordArgs) (*entryRef, error) {
	t := &entryRef{id: a.RelatesTo.ID}
	var workspace string
	err := tx.QueryRowContext(ctx, `SELECT e.tier, e.workspace, e.title, c.partition_id
		FROM entries e LEFT JOIN current c ON c.entry_id = e.id WHERE e.id = ?`, t.id).
		Scan(&t.tier, &workspace, &t.title, &t.partition)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, refuse("entry #%d does not exist; relates_to must name an existing entry", t.id)
	}
	if err != nil {
		return nil, err
	}
	if t.tier != a.Scope {
		return nil, refuse("entry #%d is in the %s tier; relates_to must name an entry of the same tier (%s)", t.id, t.tier, a.Scope)
	}
	if a.Scope == ScopeWorkspace && workspace != w.workspace {
		return nil, refuse("entry #%d belongs to another workspace; relates_to must name an entry of this workspace", t.id)
	}
	return t, nil
}

// checkHead refuses a corrects or supersedes whose target is no longer the head of its chain.
func checkHead(ctx context.Context, tx *sql.Tx, r *RelatesTo) error {
	head, err := chainHead(ctx, tx, r.ID)
	if err != nil {
		return err
	}
	if head != r.ID {
		return refuse("entry #%d was already revised; its head is #%d, so use relates_to #%d", r.ID, head, head)
	}
	return nil
}

// consumeGrant requires and consumes the matching pending grant of a crew global-tier or
// user-order record, and returns it as stored on the entry; other records need none.
func consumeGrant(ctx context.Context, tx *sql.Tx, id Identity, w writer, a RecordArgs) (string, error) {
	if id.Principal == PrincipalExternal || (a.Scope != ScopeGlobal && !a.UserOrder) {
		return "", nil
	}
	b, _, err := loadBinding(ctx, tx, id.HostSession)
	if err != nil {
		return "", err
	}
	grants, err := decodeGrants(b.grants)
	if err != nil {
		return "", err
	}
	want, err := json.Marshal(a)
	if err != nil {
		return "", err
	}
	for i, g := range grants {
		have, err := json.Marshal(g)
		if err != nil {
			return "", err
		}
		if !bytes.Equal(have, want) {
			continue
		}
		rest, err := json.Marshal(append(grants[:i:i], grants[i+1:]...))
		if err != nil {
			return "", err
		}
		_, err = tx.ExecContext(ctx, `UPDATE bindings SET pending_grants = ? WHERE host_session = ?`, string(rest), id.HostSession)
		return string(have), err
	}
	return "", refuse("a global-tier or user-order record needs the user's confirmation of exactly these arguments; call %s again with the same arguments, which asks the user", ToolRecord)
}

func insertEntry(ctx context.Context, tx *sql.Tx, id Identity, w writer, a RecordArgs, target *entryRef, grant string) (int64, error) {
	partition, err := partitionFor(ctx, tx, a.Scope, partitionKey(a.Scope, w.workspace))
	if err != nil {
		return 0, err
	}
	if err := openSession(ctx, tx, id, w); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO entries (session, writer_host_session, author, workspace, tier, nature,
		title, body, evidence, user_order, grant, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.session, w.host, w.agent, w.workspace, a.Scope, a.Nature,
		a.Title, a.Content, a.Evidence, a.UserOrder, grant, time.Now().UnixMilli())
	if err != nil {
		return 0, err
	}
	entry, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO current (entry_id, partition_id) VALUES (?, ?)`, entry, partition); err != nil {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`INSERT INTO fts_current_%d (rowid, title, body) VALUES (?, ?, ?)`, partition),
		entry, a.Title, a.Content); err != nil {
		return 0, err
	}
	if a.RelatesTo != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO relations (source, target, kind) VALUES (?, ?, ?)`,
			entry, a.RelatesTo.ID, a.RelatesTo.Relation); err != nil {
			return 0, err
		}
	}
	if revises(a.RelatesTo) {
		if err := retire(ctx, tx, target); err != nil {
			return 0, err
		}
	}
	return entry, nil
}

// retire removes a revised entry from the current set and moves its text row to the superseded index.
func retire(ctx context.Context, tx *sql.Tx, t *entryRef) error {
	var title, body string
	p := t.partition.Int64
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT title, body FROM fts_current_%d WHERE rowid = ?`, p), t.id).Scan(&title, &body)
	if err != nil {
		return err
	}
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{fmt.Sprintf(`DELETE FROM fts_current_%d WHERE rowid = ?`, p), []any{t.id}},
		{fmt.Sprintf(`INSERT INTO fts_superseded_%d (rowid, title, body) VALUES (?, ?, ?)`, p), []any{t.id, title, body}},
		{`DELETE FROM current WHERE entry_id = ?`, []any{t.id}},
	} {
		if _, err := tx.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			return err
		}
	}
	return nil
}

// partitionFor returns the partition of a tier and workspace key, creating it with its first entry.
func partitionFor(ctx context.Context, tx *sql.Tx, tier, key string) (int64, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM partitions WHERE tier = ? AND workspace_key = ?`, tier, key).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return createPartition(ctx, tx, tier, key)
	}
	return id, err
}

// openSession creates the session record with its first entry; on that first entry, the pending
// continuity of the root's binding (crew binding, or the external session's own row) becomes the session's target and is cleared (MEM-OPS-5).
func openSession(ctx context.Context, tx *sql.Tx, id Identity, w writer) error {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM sessions WHERE id = ?`, w.session).Scan(&exists)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	target := ""
	root, found, err := loadBinding(ctx, tx, w.root)
	if err != nil {
		return err
	}
	if found && root.continuity != "" {
		target = root.continuity
		if _, err := tx.ExecContext(ctx, `UPDATE bindings SET pending_continuity = '' WHERE host_session = ?`, w.root); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO sessions (id, workspace, continuity_target) VALUES (?, ?, ?)`,
		w.session, w.workspace, target)
	return err
}
