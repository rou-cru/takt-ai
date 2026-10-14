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
	"slices"
	"strings"

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
	if args[0] == "journal" || args[0] == "history" || args[0] == "recover" {
		return runVFSOffline(args, stdout, stderr)
	}
	return runVFSOperation(args, stdin, stdout, stderr)
}

// vfsCommands lists every request-driven vfs command.
var vfsCommands = []string{"claims", "assign", "release", "bind", "adopt-gates", "op", "verify", "consolidate", "discard",
	"shell-prepare", "shell-complete", "shell-guard", "shell-continue", "shell-release", "edit-guard", "native-inspection-start", "native-inspection-end", "native-inspections"}

// usageVFS lists valid vfs commands so callers can recover after a mistake.
var usageVFS = "usage: takt-ai vfs journal|history|recover --workspace <dir> --state <private-dir> [--restore] | " +
	strings.Join(vfsCommands, "|") + " --workspace <dir> --state <private-dir> < request.json"

const defaultVFSPageSize = 100

// vfsOffline is a validated offline invocation: journal, history or recover.
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
	if command != "recover" && restore {
		return errors.New("vfs: --restore applies only to recover")
	}
	return nil
}

// runVFSOffline serves read-only journal and history pagination, which never
// open the workspace lock, and explicit recovery.
func runVFSOffline(args []string, stdout, stderr io.Writer) (err error) {
	o, err := parseVFSOffline(args, stderr)
	if err != nil {
		return err
	}
	// Offline inspection must not initialize a new empty store on a typo.
	if _, err := os.Stat(filepath.Join(o.state, "vfs.sqlite")); err != nil {
		return fmt.Errorf("vfs: existing store required: %w", err)
	}
	enc := json.NewEncoder(stdout)
	switch o.command {
	case "journal":
		page, err := vfs.ReadJournal(o.state, o.sessionID, o.unitID, o.after, o.limit)
		if err != nil {
			return err
		}
		return enc.Encode(page)
	case "history":
		page, err := history.ReadPage(o.state, o.sessionID, o.unitID, o.after, o.limit)
		if err != nil {
			return err
		}
		return enc.Encode(page)
	}
	fs, err := vfs.Open(o.workspace, o.state)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, fs.Close()) }()
	if err = fs.Recover(); err != nil {
		return err
	}
	return enc.Encode(map[string]string{"recovery": "complete"})
}

// IPCVersion is the wire contract version every mutation request must carry.
const IPCVersion = 5

// request wraps every mutation IPC call.
type request struct {
	// IPCVersion the caller was built against; must match IPCVersion.
	IPCVersion int                 `json:"ipc_version"`
	Repair     *vfs.RepairApproval `json:"repair,omitempty"`
	// Caller block, delivered by the harness on every command, never by a
	// model: the root session, the host session running the tool, the
	// host-delivered agent and the unit that session executes. A claim command
	// names its target unit in WorkUnitID instead.
	SessionID      string `json:"session_id"`
	CallerSession  string `json:"caller_session"`
	CallerAgent    string `json:"caller_agent"`
	CallerDispatch string `json:"caller_dispatch,omitempty"`
	CallerAttempt  string `json:"caller_attempt,omitempty"`
	WorkUnitID     string `json:"work_unit_id"`
	// AgentID and Specialist name a claim command's target.
	AgentID    string `json:"agent_id"`
	Specialist string `json:"specialist"`
	// Invariants declares the governing invariants on a bind or an assignment,
	// in precedence order: Engram references (vfs.EngramInvariant) and
	// workspace paths. A bind that joins an open attempt inherits that
	// attempt's set.
	Invariants vfs.InvariantSet `json:"invariants,omitempty"`
	// Optional maintenance attribution; absent outside maintenance dispatches.
	CycleID      string   `json:"cycle_id,omitempty"`
	MandateClass string   `json:"mandate_class,omitempty"`
	Scope        []string `json:"scope,omitempty"`
	// op, verify, shell-prepare, shell-complete
	CallID     string   `json:"call_id,omitempty"`
	Resources  []string `json:"resources,omitempty"`
	Tool       string   `json:"tool,omitempty"`
	ToolCallID string   `json:"tool_call_id,omitempty"`
	Phase      string   `json:"phase,omitempty"`
	Action     string   `json:"action,omitempty"`
	Path       string   `json:"path,omitempty"`
	Content    string   `json:"content,omitempty"`
	// AuthorKey names the binding an op acts through, the author a judge reads
	// or verifies, the work consolidate or discard acts on, or the work an
	// assignment continues.
	AuthorKey string `json:"author_key,omitempty"`
	// AcceptFailing carries the user's explicit acceptance to consolidate work
	// a current verdict failed, for exactly the staged state AcceptedRevision
	// and AcceptedHash name: the one the failing-verdict refusal returned.
	AcceptFailing    bool   `json:"accept_failing,omitempty"`
	AcceptedRevision uint64 `json:"accepted_revision,omitempty"`
	AcceptedHash     string `json:"accepted_hash,omitempty"`
	Pass             bool   `json:"pass,omitempty"`
	Finding          string `json:"finding,omitempty"`
	// shell-prepare: the exact command the plan and any approval identify.
	Command    string `json:"command,omitempty"`
	Checkpoint string `json:"checkpoint,omitempty"`
	// release: the claim and the user's confirmation for an active one.
	ClaimKey  string `json:"key,omitempty"`
	Confirmed bool   `json:"confirmed,omitempty"`
}

// response is the JSON shape every mutation subcommand but adopt-gates returns.
type laneTermination struct {
	Cycle string     `json:"cycle"`
	Lanes []laneStop `json:"lanes"`
}

type response struct {
	StopCycle *laneTermination `json:"stop_cycle,omitempty"`
	Repair    *vfs.Repair      `json:"repair,omitempty"`
	OK        bool             `json:"ok"`
	Key       string           `json:"key,omitempty"`
	Content   string           `json:"content,omitempty"`
	// Revision is set only by operations on a staged view, where 0 is a real
	// revision, and by a failing-verdict refusal; every other answer omits it.
	Revision  *uint64 `json:"revision,omitempty"`
	DeltaHash string  `json:"delta_hash,omitempty"`
	// OutdatedPaths lists, on a gate read, the paths read under an earlier
	// state of the author's staged work.
	OutdatedPaths []string `json:"outdated_paths,omitempty"`
	Seq           int      `json:"seq,omitempty"`
	Error         string   `json:"error,omitempty"`
	// AttemptID and InvariantsVersion report what a bind belongs to: the
	// attempt the execution history admitted and the version of the invariant
	// set that governs it.
	AttemptID         string `json:"attempt_id,omitempty"`
	InvariantsVersion string `json:"invariants_version,omitempty"`
	// Shell carries the sandbox plan for one shell command: its decision and the
	// projection paths the coordinator resolved from the binding's own scope.
	Shell     *vfs.ShellPlan       `json:"shell,omitempty"`
	Claims    []vfs.OwnershipClaim `json:"claims,omitempty"`
	Collision *vfs.CollisionError  `json:"collision,omitempty"`
}

// gatesResponse answers adopt-gates; gates is always present.
type gatesResponse struct {
	OK    bool              `json:"ok"`
	Gates []vfs.AdoptedGate `json:"gates"`
}

// runVFSOperation serves every mutation command. Every decision is taken after
// the store, and with it the workspace lock, is open.
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
	if command == "native-inspections" && req.IPCVersion == IPCVersion {
		fs, openErr := vfs.Open(*workspace, *state)
		if openErr != nil {
			return openErr
		}
		defer func() { err = errors.Join(err, fs.Close()) }()
		return json.NewEncoder(stdout).Encode(map[string]any{"ok": true, "inspections": fs.NativeInspections()})
	}
	if err := validateVFSRequest(req); err != nil {
		return err
	}
	if req.CycleID != "" || req.MandateClass != "" {
		return errors.New("gc: ordinary transport cannot declare a maintenance cycle; use gc_authorize or gc_verdict")
	}
	if _, claim := claimCommandCapability[command]; claim {
		return runVFSClaimControl(*workspace, *state, command, req, stdout)
	}
	// A shell that runs natively holds no plan; only its Git mutation is judged.
	if command == "shell-guard" {
		if err := vfs.GuardShellGit(req.Command); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(response{OK: true})
	}
	// A bind for an identity outside the catalog is refused before any store is
	// opened or created; the store would reject it anyway.
	if err := validateBindRequest(command, req); err != nil {
		return err
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
	if req.Repair != nil {
		req.AcceptFailing = false
		req.AcceptedRevision = 0
		req.AcceptedHash = ""
	}
	if handled, err := admitRepair(stdout, sess.FS, &req.Repair, req.CallerAgent == vfs.OrchestratorInstance && req.CallerSession == req.SessionID); handled {
		return err
	}
	caller := callerOf(req)
	out, err := runVFSMutate(sess.FS, command, *state, req, caller)
	if err != nil {
		if handled, encodeErr := refuseForRecovery(stdout, sess.FS); handled {
			return encodeErr
		}
		return err
	}
	return json.NewEncoder(stdout).Encode(out)
}

// callerOf is the caller block of req.
func callerOf(req request) vfs.Caller {
	return vfs.Caller{Dispatch: req.CallerDispatch, AttemptID: req.CallerAttempt, SessionID: req.SessionID, HostSession: req.CallerSession, Agent: vfs.AgentID(req.CallerAgent), WorkUnitID: req.WorkUnitID}
}

func validateBindRequest(command string, req request) error {
	if command != "bind" {
		return nil
	}
	_, err := vfs.SpecialistRole(req.CallerAgent)
	if err != nil {
		return err
	}
	if err := vfs.RequireVFSCapability(req.CallerAgent, model.VFSCapabilityBind); err != nil {
		return err
	}
	if len(req.Scope) > 0 {
		return vfs.RequireVFSCapability(req.CallerAgent, model.VFSCapabilityWrite)
	}
	return nil
}

// claimCommandCapability maps each orchestrator claim command to the catalog
// grant that authorizes it.
var claimCommandCapability = map[string]model.VFSCapability{
	"claims":  model.VFSCapabilityClaimList,
	"assign":  model.VFSCapabilityClaimAssign,
	"release": model.VFSCapabilityClaimRelease,
}

func validVFSCommand(command string) bool { return slices.Contains(vfsCommands, command) }

func runVFSClaimControl(workspace, state, command string, req request, stdout io.Writer) (err error) {
	if command != "claims" && (req.CallerAgent != vfs.OrchestratorInstance || req.CallerSession != req.SessionID) {
		return errors.New("VFS claim mutation belongs to the root orchestrator; return to it for claim_assign or claim_release")
	}
	if err := vfs.RequireVFSCapability(vfs.OrchestratorInstance, claimCommandCapability[command]); err != nil {
		return err
	}
	fs, err := vfs.Open(workspace, state)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, fs.Close()) }()
	if handled, err := admitRepair(stdout, fs, &req.Repair, req.CallerAgent == vfs.OrchestratorInstance && req.CallerSession == req.SessionID); handled {
		return err
	}
	switch command {
	case "claims":
		return json.NewEncoder(stdout).Encode(response{OK: true, Claims: fs.OwnershipClaims(req.SessionID)})
	case "assign":
		return assignClaim(fs, req, stdout)
	default: // release
		if err := fs.ReleaseClaim(req.SessionID, vfs.AgentID(req.ClaimKey), req.Confirmed); err != nil {
			return err
		}
		return json.NewEncoder(stdout).Encode(response{OK: true})
	}
}

// assignClaim reserves a scope for its target, or, given an author key,
// hands that existing work to the target with a new scope.
func assignClaim(fs *vfs.FS, req request, stdout io.Writer) error {
	p, err := history.ReadProjection(fs.StateDirectory())
	if err != nil {
		return err
	}
	consumed, declared := p.Inputs[history.UnitKey(req.SessionID, req.WorkUnitID)]
	if !declared {
		return fmt.Errorf("claim assignment refused: inputs absent for root %s unit %s; call dispatch_inputs before claim_assign", req.SessionID, req.WorkUnitID)
	}
	// The target's identity is the harness's; it carries no attempt until the
	// execution history issues one when the target binds.
	identity := vfs.Identity{SessionID: req.SessionID, WorkUnitID: req.WorkUnitID, AgentID: vfs.AgentID(req.AgentID), Specialist: req.Specialist}
	for _, id := range consumed {
		identity.Invariants = append(identity.Invariants, vfs.EngramInvariant(id))
	}
	key := vfs.AgentID(req.AuthorKey)
	var assignErr error
	if key != "" {
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
	if req.SessionID == "" || req.CallerSession == "" || req.CallerAgent == "" {
		return errors.New("vfs: session_id, caller_session and caller_agent are required; run takt-ai setup sync to re-deploy the plugin")
	}
	return nil
}

// runVFSMutate dispatches one mutation for caller.
func runVFSMutate(fs *vfs.FS, command, state string, req request, caller vfs.Caller) (any, error) {
	switch command {
	case "bind":
		return mutateBind(fs, req, caller)
	case "adopt-gates":
		gates, err := fs.AdoptGates(caller)
		return gatesResponse{OK: err == nil, Gates: gates}, err
	case "op":
		return mutateOperation(fs, req, caller)
	case "verify":
		return response{OK: true}, fs.VerifyAs(caller, req.CallID, vfs.AgentID(req.AuthorKey), req.Pass, req.Finding)
	case "consolidate":
		return mutateConsolidate(fs, req, caller)
	case "discard":
		return response{OK: true}, fs.DiscardAs(caller, vfs.AgentID(req.AuthorKey))
	case "edit-guard":
		return response{OK: true}, fs.GuardNativeEdit(req.SessionID, req.Resources)
	case "native-inspection-start":
		return response{OK: true}, fs.StartNativeInspection(caller, req.CallID, req.Tool, req.Path)
	case "native-inspection-end":
		return response{OK: true}, fs.EndNativeInspection(caller, req.CallID)
	case "shell-continue":
		plan, err := fs.ContinueShell(caller, req.CallID, req.Phase)
		return response{OK: true, Shell: &plan}, err
	case "shell-release":
		return response{OK: true}, fs.ReleaseShell(caller, req.CallID)
	case "shell-prepare":
		// A policy denial returns OK true with Shell.Decision deny.
		plan, err := fs.PrepareShell(caller, req.CallID, req.Command, state, req.ToolCallID)
		return response{OK: true, Shell: &plan}, err
	case "shell-complete":
		// Success reports the resulting revision and delta hash even when the
		// command exited unsuccessfully.
		result, err := fs.CompleteShell(caller, req.CallID, state)
		return response{OK: true, Revision: &result.Revision, DeltaHash: result.DeltaHash}, err
	}
	return nil, fmt.Errorf("vfs: unsupported command %q", command)
}

func mutateBind(fs *vfs.FS, req request, caller vfs.Caller) (response, error) {
	bound, err := fs.BindAs(caller, req.Scope, req.Invariants)
	if err != nil {
		return response{}, err
	}
	// The caller learns its attempt and the pinned invariant set from the result,
	// which is the only place either is issued.
	out := response{OK: true, Key: string(bound.Key), AttemptID: bound.AttemptID, InvariantsVersion: bound.InvariantsVersion}
	// A bind that adopts work already staged under the key continues from its
	// revision; a fresh bind keeps the fixed contract without one.
	if bound.Staged {
		out.Revision, out.DeltaHash = &bound.Revision, bound.DeltaHash
	}
	return out, nil
}

// mutateOperation reads or changes staged work through the binding the caller
// holds. Success returns any read content, the resulting revision (including
// zero), the delta hash and, for a gate read, the outdated paths.
func mutateOperation(fs *vfs.FS, req request, caller vfs.Caller) (response, error) {
	action, err := parseAction(req.Action)
	if err != nil {
		return response{}, err
	}
	result, err := fs.Operate(caller, vfs.CallerOperation{CallID: req.CallID, Action: action, Path: req.Path,
		Content: []byte(req.Content), AuthorKey: vfs.AgentID(req.AuthorKey)})
	if err != nil {
		return response{}, err
	}
	return response{OK: true, Content: string(result.Content), Revision: &result.Revision, DeltaHash: result.DeltaHash, OutdatedPaths: result.OutdatedPaths}, nil
}

// mutateConsolidate consolidates for the orchestrator. A failing-verdict
// refusal answers with the staged state it refused, which a retry's
// acceptance must name.
func mutateConsolidate(fs *vfs.FS, req request, caller vfs.Caller) (response, error) {
	var accepted *vfs.StagedState
	if req.AcceptFailing {
		accepted = &vfs.StagedState{Revision: req.AcceptedRevision, DeltaHash: req.AcceptedHash}
	}
	seq, err := fs.ConsolidateAs(caller, vfs.AgentID(req.AuthorKey), req.Checkpoint, accepted)
	var failing *vfs.FailingVerdictError
	if errors.As(err, &failing) {
		return response{OK: false, Error: err.Error(), Revision: &failing.Revision, DeltaHash: failing.DeltaHash}, nil
	}
	if err != nil {
		return response{}, err
	}
	return response{OK: true, Seq: seq}, nil
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
	}
	return "", fmt.Errorf("vfs: unsupported action %q", action)
}

// admitRepair runs before a normal operation: it applies the manifest repair
// *approval names, clearing that consumed approval, or answers the repair still
// pending as the operation's refusal. handled reports that the command ends here.
// Specialist callers receive the same diagnostic, but cannot consume consent.
func admitRepair(stdout io.Writer, fs *vfs.FS, approval **vfs.RepairApproval, orchestrator bool) (handled bool, err error) {
	pending, err := fs.RecoveryRepair()
	if err != nil {
		return true, err
	}
	a := *approval
	if a == nil || a.Kind != "manifest" {
		return refuseForRepair(stdout, pending)
	}
	if pending == nil {
		return true, errors.New("manifest repair approval is obsolete; evaluate the normal operation again without it")
	}
	if !orchestrator {
		return true, errors.New("incomplete consolidation requires the root orchestrator to request approval for its exact repair; return the manifest diagnostic to it")
	}
	if err := fs.RepairRecovery(*a); err != nil {
		return true, err
	}
	*approval = nil
	return false, nil
}

// refuseForRecovery answers a failed operation with the repair it left
// pending, if any; handled reports that it did.
func refuseForRecovery(stdout io.Writer, fs *vfs.FS) (handled bool, err error) {
	pending, diagnose := fs.RecoveryRepair()
	if diagnose != nil {
		return false, nil
	}
	return refuseForRepair(stdout, pending)
}

// refuseForRepair answers pending, when there is one, as the refusal.
func refuseForRepair(stdout io.Writer, pending *vfs.Repair) (handled bool, err error) {
	if pending == nil {
		return false, nil
	}
	return true, json.NewEncoder(stdout).Encode(response{OK: false, Error: pending.Reason, Repair: pending})
}
