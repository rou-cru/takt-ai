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

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/session"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// runVFS exposes offline evidence, explicit recovery, and mutation IPC.
func runVFS(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New(usageVFS)
	}
	if args[0] == "journal" || args[0] == "recover" {
		return runVFSOffline(args, stdout, stderr)
	}
	return runVFSOperation(args, stdin, stdout, stderr)
}

// usageVFS lists valid vfs commands so callers can recover after a mistake.
const usageVFS = "usage: takt-ai vfs journal|recover --workspace <dir> --state <private-dir> [--restore] | claims|assign|release|bind|op|verify|consolidate --workspace <dir> --state <private-dir> < request.json"

const defaultVFSPageSize = 100

// vfsOffline is a validated offline invocation: journal or recover.
type vfsOffline struct {
	command, workspace, state, sessionID, unitID string
	after, limit                                 int
	restore                                      bool
}

// parseVFSOffline reads and validates the flags of an offline command.
func parseVFSOffline(args []string, stderr io.Writer) (vfsOffline, error) {
	o := vfsOffline{command: args[0]}
	flags := flag.NewFlagSet("vfs "+o.command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&o.workspace, "workspace", "", "workspace governed by the store")
	flags.StringVar(&o.state, "state", "", "existing private VFS state directory")
	flags.StringVar(&o.sessionID, "session", "", "journal session filter")
	flags.StringVar(&o.unitID, "unit", "", "journal work-unit filter")
	flags.IntVar(&o.after, "after", -1, "exclusive journal cursor")
	flags.IntVar(&o.limit, "limit", defaultVFSPageSize, fmt.Sprintf("page size (1..%d)", vfs.MaxJournalPageSize))
	flags.BoolVar(&o.restore, "restore", false, "explicitly restore the incomplete flush's pre-flush state")
	if err := flags.Parse(args[1:]); err != nil {
		return o, err
	}
	return o, validateVFSOffline(o, flags.NArg())
}

func validateVFSOffline(o vfsOffline, positional int) error {
	if positional != 0 || o.workspace == "" || o.state == "" {
		return errors.New("vfs: --workspace and --state are required; no positional arguments")
	}
	if o.after < -1 || o.limit < 1 || o.limit > vfs.MaxJournalPageSize {
		return errors.New("vfs: invalid pagination")
	}
	return validateVFSRecoveryFlags(o.command, o.restore)
}

func validateVFSRecoveryFlags(command string, restore bool) error {
	if command == "recover" && !restore {
		return errors.New("vfs: recovery requires explicit --restore; incompatible external changes will not be overwritten")
	}
	if command == "journal" && restore {
		return errors.New("vfs: --restore applies only to recover")
	}
	return nil
}

// runVFSOffline serves journal pagination and explicit recovery only.
func runVFSOffline(args []string, stdout, stderr io.Writer) (err error) {
	o, err := parseVFSOffline(args, stderr)
	if err != nil {
		return err
	}
	// Offline inspection must not initialize a new empty store on a typo.
	if _, err := os.Stat(filepath.Join(o.state, "vfs.sqlite")); err != nil {
		return fmt.Errorf("vfs: existing store required: %w", err)
	}
	fs, err := vfs.Open(o.workspace, o.state)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, fs.Close()) }()
	if o.command == "recover" {
		if err = fs.Recover(); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(map[string]string{"recovery": "complete"})
	}
	return json.NewEncoder(stdout).Encode(fs.JournalPage(o.sessionID, o.unitID, o.after, o.limit))
}

// IPCVersion is the wire contract version every mutation request must carry.
const IPCVersion = 4

// request wraps every mutation IPC call.
type request struct {
	// IPCVersion the caller was built against; must match IPCVersion.
	IPCVersion int `json:"ipc_version"`
	// Identity. The attempt and the invariant set's version are the harness's to
	// issue, so no request declares them.
	SessionID  string `json:"session_id"`
	WorkUnitID string `json:"work_unit_id"`
	AgentID    string `json:"agent_id"`
	Specialist string `json:"specialist"`
	// Invariants declares the governing documents on a bind, in precedence
	// order; a bind that joins an open attempt inherits that attempt's set.
	Invariants vfs.InvariantSet `json:"invariants,omitempty"`
	// Optional maintenance attribution; absent outside maintenance dispatches.
	CycleID      string `json:"cycle_id,omitempty"`
	MandateClass string `json:"mandate_class,omitempty"`
	// bind / orchestrator assign: these identity fields are injected by the
	// harness, never copied from model-provided target metadata.
	Scope []string `json:"scope,omitempty"`
	// op
	CallID           string `json:"call_id,omitempty"`
	ExpectedRevision uint64 `json:"expected_revision,omitempty"`
	Action           string `json:"action,omitempty"`
	Path             string `json:"path,omitempty"`
	Content          string `json:"content,omitempty"`
	// verify
	AuthorKey   string `json:"author_key,omitempty"`
	VerifierKey string `json:"verifier_key,omitempty"`
	// ViewKey is the author whose staged view a verifier reads through.
	ViewKey   string `json:"view_key,omitempty"`
	DeltaHash string `json:"delta_hash,omitempty"`
	Pass      bool   `json:"pass,omitempty"`
	Finding   string `json:"finding,omitempty"`
	// shell-prepare: the exact command the plan and any approval identify.
	Command string `json:"command,omitempty"`
	// consolidate
	Checkpoint string `json:"checkpoint,omitempty"`
	ClaimKey   string `json:"key,omitempty"`
}

// response is the single JSON shape every mutation subcommand returns.
type response struct {
	OK        bool   `json:"ok"`
	Key       string `json:"key,omitempty"`
	Content   string `json:"content,omitempty"`
	Revision  uint64 `json:"revision"`
	DeltaHash string `json:"delta_hash,omitempty"`
	Seq       int    `json:"seq,omitempty"`
	Error     string `json:"error,omitempty"`
	// AttemptID and InvariantsVersion report what the harness issued for a bind:
	// the attempt this dispatch belongs to and the version of the invariant set
	// that governs it.
	AttemptID         string `json:"attempt_id,omitempty"`
	InvariantsVersion string `json:"invariants_version,omitempty"`
	// Shell carries the sandbox plan for one shell command: its decision and the
	// projection paths the coordinator resolved from the binding's own scope.
	Shell     *vfs.ShellPlan       `json:"shell,omitempty"`
	Claims    []vfs.OwnershipClaim `json:"claims,omitempty"`
	Collision *vfs.CollisionError  `json:"collision,omitempty"`
}

// runVFSOperation serves bind/op/verify/consolidate.
func runVFSOperation(args []string, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	command := args[0]
	if !validVFSCommand(command) {
		return fmt.Errorf("unknown vfs command %q; %s", command, usageVFS)
	}
	flags := flag.NewFlagSet("vfs "+command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	workspace := flags.String("workspace", "", "workspace governed by the store")
	state := flags.String("state", "", "private VFS state directory")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if err := validateVFSFlags(flags.NArg(), *workspace, *state); err != nil {
		return err
	}
	var req request
	if err := json.NewDecoder(stdin).Decode(&req); err != nil {
		return fmt.Errorf("vfs: invalid request: %w", err)
	}
	if err := validateVFSRequest(req); err != nil {
		return err
	}
	if _, claim := claimCommandCapability[command]; claim {
		return runVFSClaimControl(*workspace, *state, command, req, stdout)
	}
	// A bind for an identity outside the catalog is refused before any store is
	// opened or created; the store would reject it anyway.
	if err := validateBindRequest(command, req); err != nil {
		return err
	}
	identity := vfs.Identity{
		SessionID:     req.SessionID,
		WorkUnitID:    req.WorkUnitID,
		AgentID:       vfs.AgentID(req.AgentID),
		Specialist:    req.Specialist,
		GateAuthorKey: vfs.AgentID(req.AuthorKey),
		Invariants:    req.Invariants,
		CycleID:       req.CycleID,
		MandateClass:  req.MandateClass,
	}
	if command == "bind" {
		// The attempt is issued once, by the delegation's admission: a bind
		// joins it instead of numbering attempts on its own (PR-DAG-MUT-8).
		if identity.AttemptID, err = admittedAttempt(*state, req.WorkUnitID); err != nil {
			return err
		}
	}
	// The store lock serializes concurrent plugin invocations; NewDurable either
	// resumes persisted state or initializes a new private store, wired to this
	// session's bus so every mutation the plugin drives lands on the journal in
	// order (see takt/session).
	sess, err := session.NewDurable(*workspace, *state, req.SessionID)
	if err != nil {
		return err
	}
	// Session.Close releases only the event store; the FS is closed here.
	defer func() { err = errors.Join(err, sess.Close(), sess.FS.Close()) }()
	if err := gc.GuardVFS(*state, command, identity, req.Action, req.Path, vfs.AgentID(req.AuthorKey), sess.FS); err != nil {
		return err
	}
	out, err := runVFSMutate(sess.FS, command, *state, req, identity)
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(out)
}

// admittedAttempt is the attempt the execution history admitted for unit,
// or empty when no delegation of it is in flight (maintenance work, whose
// attempts the store numbers itself).
func admittedAttempt(state, unit string) (attempt string, err error) {
	h, err := history.Open(state)
	if err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, h.Close()) }()
	if u := h.Project().Units[unit]; u.State == history.StateInFlight {
		return u.AttemptID, nil
	}
	return "", nil
}

func validateBindRequest(command string, req request) error {
	if command != "bind" {
		return nil
	}
	_, err := vfs.SpecialistRole(req.Specialist)
	if err != nil {
		return err
	}
	if err := vfs.RequireVFSCapability(req.Specialist, model.VFSCapabilityBind); err != nil {
		return err
	}
	if len(req.Scope) > 0 {
		return vfs.RequireVFSCapability(req.Specialist, model.VFSCapabilityWrite)
	}
	return nil
}

// claimCommandCapability maps each orchestrator claim command to the catalog
// grant that authorizes it.
var claimCommandCapability = map[string]model.VFSCapability{
	"claims":          model.VFSCapabilityClaimList,
	"assign":          model.VFSCapabilityClaimAssign,
	"assign-verifier": model.VFSCapabilityClaimAssign,
	"release":         model.VFSCapabilityClaimRelease,
}

func validVFSCommand(command string) bool {
	_, claim := claimCommandCapability[command]
	return claim || command == "bind" || command == "op" || command == "verify" || command == "consolidate" ||
		command == "shell-prepare" || command == "shell-import"
}

// claimIdentity is the harness-owned identity of an assignment target, with the
// attempt taken from its admitted work unit.
func claimIdentity(state string, req request) (vfs.Identity, error) {
	identity := vfs.Identity{SessionID: req.SessionID, WorkUnitID: req.WorkUnitID,
		AgentID: vfs.AgentID(req.AgentID), Specialist: req.Specialist, Invariants: req.Invariants,
		CycleID: req.CycleID, MandateClass: req.MandateClass}
	var err error
	identity.AttemptID, err = admittedAttempt(state, req.WorkUnitID)
	return identity, err
}

func runVFSClaimControl(workspace, state, command string, req request, stdout io.Writer) (err error) {
	if req.CycleID != "" || req.MandateClass != "" {
		return errors.New("gc: ordinary claims cannot declare a maintenance cycle")
	}
	if err := vfs.RequireVFSCapability(vfs.OrchestratorInstance, claimCommandCapability[command]); err != nil {
		return err
	}
	fs, err := vfs.Open(workspace, state)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, fs.Close()) }()
	switch command {
	case "claims":
		return json.NewEncoder(stdout).Encode(response{OK: true, Claims: fs.OwnershipClaims(req.SessionID)})
	case "assign":
		identity, err := claimIdentity(state, req)
		if err != nil {
			return err
		}
		// An author key names existing work: the unit's next attempt takes a new
		// scope and keeps the staged delta instead of starting empty.
		var key vfs.AgentID
		var assignErr error
		if req.AuthorKey != "" {
			key = vfs.AgentID(req.AuthorKey)
			assignErr = fs.ReassignScope(identity, key, req.Scope)
		} else {
			key, assignErr = fs.AssignScope(identity, req.Scope)
		}
		if assignErr != nil {
			var collision *vfs.CollisionError
			if errors.As(assignErr, &collision) {
				return json.NewEncoder(stdout).Encode(response{OK: false, Error: assignErr.Error(), Collision: collision})
			}
			return assignErr
		}
		return json.NewEncoder(stdout).Encode(response{OK: true, Key: string(key)})
	case "assign-verifier":
		if len(req.Scope) != 0 {
			return fmt.Errorf("vfs: assign-verifier does not accept scope")
		}
		identity, err := claimIdentity(state, req)
		if err != nil {
			return err
		}
		key, assignErr := fs.AssignVerifier(identity, vfs.AgentID(req.AuthorKey))
		if assignErr != nil {
			return assignErr
		}
		return json.NewEncoder(stdout).Encode(response{OK: true, Key: string(key)})
	default: // release
		if err := fs.RevokeOwnership(vfs.AgentID(req.ClaimKey)); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(response{OK: true})
	}
}

func validateVFSFlags(positional int, workspace, state string) error {
	if positional != 0 || workspace == "" || state == "" {
		return errors.New("vfs: --workspace and --state are required; no positional arguments")
	}
	return nil
}

func validateVFSRequest(req request) error {
	if req.IPCVersion != IPCVersion {
		return fmt.Errorf("vfs: ipc_version %d unsupported (want %d); run takt-ai setup sync to re-deploy the plugin", req.IPCVersion, IPCVersion)
	}
	if req.SessionID == "" {
		return errors.New("vfs: session_id is required")
	}
	return nil
}

// runVFSMutate dispatches one verified mutation.
func runVFSMutate(fs *vfs.FS, command, state string, req request, identity vfs.Identity) (response, error) {
	switch command {
	case "bind":
		return mutateBind(fs, req, identity)
	case "op":
		return mutateOperation(fs, req, identity)
	case "verify":
		return mutateVerify(fs, req, identity)
	case "consolidate":
		return mutateConsolidate(fs, req, identity)
	case "shell-prepare":
		return prepareShell(fs, state, req, identity)
	case "shell-import":
		return importShell(fs, state, req, identity)
	}
	return response{}, fmt.Errorf("vfs: unsupported command %q", command)
}

// verifyIdentity rejects a request whose declared identity does not match the
// identity actually bound to key. Role, attempt and invariants are
// harness-derived at bind time, so the request never restates them; both
// sides are cleared of those fields before the comparison. Because Identity
// also carries WorkUnitID, a mismatched work unit is rejected by the same
// check, so no separate WorkUnitID comparison is needed.
func verifyIdentity(fs *vfs.FS, key vfs.AgentID, identity vfs.Identity, field string) error {
	bound, ok := fs.BindingIdentity(key)
	if field == "verifier_key" && (!ok || bound.GateAuthorKey != identity.GateAuthorKey || identity.GateAuthorKey == "") {
		return fmt.Errorf("%w: caller is not the identity bound to %s", vfs.ErrIdentity, field)
	}
	bound.Role, bound.AttemptID, bound.InvariantsHash, bound.Invariants = "", "", "", nil
	bound.CycleID, bound.MandateClass = "", ""
	bound.GateAuthorKey = ""
	identity.Invariants = nil
	identity.CycleID, identity.MandateClass = "", ""
	identity.GateAuthorKey = ""
	if !ok || !reflect.DeepEqual(bound, identity) {
		return fmt.Errorf("%w: caller is not the identity bound to %s", vfs.ErrIdentity, field)
	}
	return nil
}

// prepareShell resolves the sandbox plan for one command. The writable paths,
// private scratch, and protected paths are derived from the binding's scope,
// workspace, state directory, and call ID. Preparation may create scratch and
// projection files; it does not execute the command.
// An empty AuthorKey skips identity verification and uses an unbound plan that
// keeps the workspace write-protected. A supplied key must match identity, and
// ExpectedRevision must match that binding's revision.
// Identity and preparation errors propagate to the caller. A policy denial is
// instead returned with OK true and Shell.Decision set to deny.
func prepareShell(fs *vfs.FS, state string, req request, identity vfs.Identity) (response, error) {
	key := vfs.AgentID(req.AuthorKey)
	if key != "" {
		if err := verifyIdentity(fs, key, identity, "author_key"); err != nil {
			return response{}, err
		}
	}
	plan, err := fs.PrepareShell(key, req.CallID, req.Command, state, req.ExpectedRevision)
	if err != nil {
		return response{}, err
	}
	return response{OK: true, Shell: &plan}, nil
}

// importShell admits the command's captured result as one VFS transaction.
func importShell(fs *vfs.FS, state string, req request, identity vfs.Identity) (response, error) {
	key := vfs.AgentID(req.AuthorKey)
	if err := verifyIdentity(fs, key, identity, "author_key"); err != nil {
		return response{}, err
	}
	result, err := fs.ImportShell(key, req.CallID, state, req.ExpectedRevision)
	if err != nil {
		return response{}, err
	}
	return response{OK: true, Revision: result.Revision, DeltaHash: result.DeltaHash}, nil
}

func mutateBind(fs *vfs.FS, req request, identity vfs.Identity) (response, error) {
	key, err := fs.Bind(identity, req.Scope)
	if err != nil {
		return response{}, err
	}
	// The caller learns its attempt and the pinned invariant set from the result,
	// which is the only place either is issued.
	bound, _ := fs.BindingIdentity(key)
	return response{OK: true, Key: string(key), AttemptID: bound.AttemptID, InvariantsVersion: bound.InvariantsHash}, nil
}

func mutateOperation(fs *vfs.FS, req request, identity vfs.Identity) (response, error) {
	key := vfs.AgentID(req.AuthorKey)
	if key == "" {
		key = identity.AgentID
	}
	if err := verifyIdentity(fs, key, identity, "author_key"); err != nil {
		return response{}, err
	}
	action, err := parseAction(req.Action)
	if err != nil {
		return response{}, err
	}
	op := vfs.Operation{Key: key, CallID: req.CallID, ExpectedRevision: req.ExpectedRevision, Action: action, Path: req.Path, Content: []byte(req.Content)}
	var result vfs.OperationResult
	if req.ViewKey != "" {
		result, err = fs.ReadAs(op, vfs.AgentID(req.ViewKey))
	} else {
		result, err = fs.Apply(op)
	}
	if err != nil {
		return response{}, err
	}
	return response{OK: true, Content: string(result.Content), Revision: result.Revision, DeltaHash: result.DeltaHash}, nil
}

func mutateVerify(fs *vfs.FS, req request, identity vfs.Identity) (response, error) {
	// Verifier bindings are preassigned against one author. The verify request
	// names that author, so include the harness-issued relationship when checking
	// the caller against the binding; all other identity fields remain exact.
	identity.GateAuthorKey = vfs.AgentID(req.AuthorKey)
	if err := verifyIdentity(fs, vfs.AgentID(req.VerifierKey), identity, "verifier_key"); err != nil {
		return response{}, err
	}
	if err := fs.Verify(vfs.AgentID(req.VerifierKey), vfs.AgentID(req.AuthorKey), req.CallID, req.ExpectedRevision, req.DeltaHash, req.Pass, req.Finding); err != nil {
		return response{}, err
	}
	return response{OK: true}, nil
}

func mutateConsolidate(fs *vfs.FS, req request, identity vfs.Identity) (response, error) {
	key := vfs.AgentID(req.AuthorKey)
	if err := verifyIdentity(fs, key, identity, "author_key"); err != nil {
		return response{}, err
	}
	if err := vfs.RequireVFSCapability(vfs.OrchestratorInstance, model.VFSCapabilityConsolidate); err != nil {
		return response{}, err
	}
	if err := fs.ConsolidateCheckpoint(key, req.Checkpoint, req.ExpectedRevision); err != nil {
		return response{}, err
	}
	return response{OK: true}, nil
}

// parseAction converts the wire action name to its VFS operation type.
func parseAction(action string) (vfs.OperationType, error) {
	switch action {
	case "read":
		return vfs.OpRead, nil
	case "create":
		return vfs.OpCreate, nil
	case "patch":
		return vfs.OpPatch, nil
	case "delete":
		return vfs.OpDelete, nil
	case "rollback":
		return vfs.OpRollback, nil
	}
	return "", fmt.Errorf("vfs: unsupported action %q", action)
}
