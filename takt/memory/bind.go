package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// emptyGrants is the stored form of a binding with no pending grants.
const emptyGrants = "[]"

// BindArgs is one crew bind. The hook sends it before every memory call.
type BindArgs struct {
	// HostSession is the runtime's session ID of the caller.
	HostSession string `json:"host_session"`
	// Agent is the caller's current agent; it is refreshed on every bind.
	Agent string `json:"agent"`
	// ClaimedRoot is the top of the parentID chain, or the session itself.
	ClaimedRoot string `json:"claimed_root"`
	// Workspace is the workspace key the binding is created with.
	Workspace string `json:"workspace"`
	// Grants are the record arguments the user approved for this call.
	Grants []RecordArgs `json:"grants,omitempty"`
}

// Bind upserts the crew binding of a host session and appends the approved grants to its
// pending grants. An existing binding keeps its root; for a new one, a claimed root that is
// itself bound resolves to that binding's root.
func (s *Store) Bind(ctx context.Context, a BindArgs) error {
	if a.HostSession == "" || a.ClaimedRoot == "" || a.Workspace == "" || a.Agent == "" {
		return refuse("bind needs host_session, agent, claimed_root, and workspace")
	}
	return s.write(ctx, func(tx *sql.Tx) error {
		b, found, err := loadBinding(ctx, tx, a.HostSession)
		if err != nil {
			return err
		}
		pending := []RecordArgs{}
		if found {
			if pending, err = decodeGrants(b.grants); err != nil {
				return err
			}
		}
		pending = append(pending, a.Grants...)
		grants, err := json.Marshal(pending)
		if err != nil {
			return err
		}
		if found {
			_, err = tx.ExecContext(ctx, `UPDATE bindings SET agent = ?, pending_grants = ? WHERE host_session = ?`,
				a.Agent, string(grants), a.HostSession)
		} else {
			root := a.ClaimedRoot
			claimed, ok, lerr := loadBinding(ctx, tx, a.ClaimedRoot)
			if lerr != nil {
				return lerr
			}
			if ok {
				root = claimed.root
			}
			_, err = tx.ExecContext(ctx, `INSERT INTO bindings
				(host_session, root, agent, workspace, pending_continuity, pending_grants) VALUES (?, ?, ?, ?, '', ?)`,
				a.HostSession, root, a.Agent, a.Workspace, string(grants))
		}
		return err
	})
}

type binding struct {
	root, agent, workspace, continuity, grants string
}

func loadBinding(ctx context.Context, tx *sql.Tx, hostSession string) (binding, bool, error) {
	var b binding
	err := tx.QueryRowContext(ctx, `SELECT root, agent, workspace, pending_continuity, pending_grants
		FROM bindings WHERE host_session = ?`, hostSession).Scan(&b.root, &b.agent, &b.workspace, &b.continuity, &b.grants)
	if errors.Is(err, sql.ErrNoRows) {
		return binding{}, false, nil
	}
	return b, err == nil, err
}

func decodeGrants(raw string) ([]RecordArgs, error) {
	var grants []RecordArgs
	err := json.Unmarshal([]byte(raw), &grants)
	return grants, err
}
