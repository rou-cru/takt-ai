package gc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

const coordinatorFile = "gc-coordinator.json"

// PlanUnit is one unit of a plan commitment; aliased from package dispatch.
type PlanUnit = dispatch.PlanUnit

// RecoveryDeclaration declares bounded recovery before uncertain work begins
// (PR-ORQ-13); GC records it verbatim from the declarer.
type RecoveryDeclaration = dispatch.RecoveryDeclaration

// AdmissionPolicy holds the dispatch ceiling and session budgets; GC reads it
// to admit work, never to change it.
type AdmissionPolicy = dispatch.AdmissionPolicy

// AdmissionPolicyRef names the admission rule on every record; aliased from
// package dispatch.
const AdmissionPolicyRef = dispatch.AdmissionPolicyRef

// Cycle persists the declaration independently of mutations, including no-op cycles.
type Cycle struct {
	Plan           Plan              `json:"plan"`
	Phase          string            `json:"phase"`
	Scope          []string          `json:"scope"`
	Sessions       map[string]string `json:"sessions"`
	AuthorKey      vfs.AgentID       `json:"author_key,omitempty"`
	VerifierKey    vfs.AgentID       `json:"verifier_key,omitempty"`
	Baseline       []CheckEvidence   `json:"baseline,omitempty"`
	Acceptance     []CheckEvidence   `json:"acceptance,omitempty"`
	Report         Report            `json:"report"`
	Investigations []Investigation   `json:"investigations,omitempty"`
	Reason         string            `json:"reason,omitempty"`
	Started        time.Time         `json:"started"`
}

// Coordinator is private workspace state, all access serialized by vfs.Open.
// In-flight ordinary-dispatch state is read from the history projection via
// package dispatch (PR-DAG-AUT-1); Units and Mutations are GC's own pace
// counters for deciding when to trigger the next cycle (PR-MNT-9/11).
type Coordinator struct {
	Version     int  `json:"version"`
	Units       int  `json:"units"`
	Mutations   int  `json:"mutations"`
	Cursor      int  `json:"cursor"`
	Deferrals   int  `json:"deferrals"`
	NextMandate int  `json:"next_mandate"`
	Requested   bool `json:"requested"`
	// Session is the root session the pace counters belong to; another root
	// session starts them over (see Bind).
	Session string  `json:"session,omitempty"`
	Cycle   *Cycle  `json:"cycle,omitempty"`
	History []Cycle `json:"history,omitempty"`
}

// Specialist identities used by the maintenance cycle. These are catalog
// instance IDs, centralized here so CLI and coordinator cannot drift.
const (
	CollectorSpecialistID = "simplify"
	VerifierSpecialistID  = "verify"
	// coordinatorFileMode keeps transient coordinator state private.
	coordinatorFileMode os.FileMode = 0o600
	// cycleIDBytes is the random byte length used for cycle identifiers.
	cycleIDBytes = 16
	// ProjectConfigPath is the reviewed project configuration a maintenance cycle
	// is dispatched under and judged against.
	ProjectConfigPath = ".takt/gc.json"
)

// LoadCoordinator reads the coordinator state, accepting a missing file as a
// fresh coordinator. A corrupt or future-version file is an error, never
// silently reset.
func LoadCoordinator(state string) (*Coordinator, error) {
	c := &Coordinator{Version: 1, Cursor: -1}
	b, e := os.ReadFile(filepath.Join(state, coordinatorFile))
	if errors.Is(e, os.ErrNotExist) {
		return c, nil
	}
	if e != nil {
		return nil, e
	}
	if e = json.Unmarshal(b, c); e != nil {
		return nil, e
	}
	if c.Version != 1 || c.NextMandate < 0 || c.NextMandate >= 5 {
		return nil, errors.New("gc: invalid coordinator state")
	}
	return c, nil
}

// SaveCoordinator atomically persists the coordinator under state.
func SaveCoordinator(state string, c *Coordinator) error {
	b, e := json.Marshal(c)
	if e != nil {
		return e
	}
	_, e = filemerge.WriteFileAtomic(filepath.Join(state, coordinatorFile), b, coordinatorFileMode)
	return e
}

// Bind makes session the one the pace counters measure. A cycle is declared
// over its own session's delta (PR-MNT-4), so work a previous root session left
// behind in this workspace never counts toward the next session's cadence.
func (c *Coordinator) Bind(session string) {
	if session == "" || session == c.Session {
		return
	}
	c.reset()
	c.Session = session
}

// Observe consumes each effective ordinary journal mutation exactly once. Only
// the bound session's mutations count; other sessions' entries are consumed
// without counting.
func (c *Coordinator) Observe(entries []vfs.JournalEntry) {
	for _, e := range entries {
		if e.Seq <= c.Cursor {
			continue
		}
		c.Cursor = e.Seq
		if e.CycleID != "" || e.Outcome != "" || e.BeforeHash == e.AfterHash || e.SessionID != "" && e.SessionID != c.Session {
			continue
		}
		if e.Operation == vfs.OpCreate || e.Operation == vfs.OpPatch || e.Operation == vfs.OpDelete {
			c.Mutations++
		}
	}
}

// Held reports whether a maintenance cycle is in flight. It never gates
// ordinary dispatch: a delegation ends the cycle instead (see Yield).
func (c *Coordinator) Held() bool { return c.Cycle != nil }

// Admit composes ordinary dispatch admission with the session pace counters.
// Maintenance never holds an admission: the caller ends a cycle in flight
// (Yield) before admitting. Concurrency, plan coverage and recovery budgets are
// package dispatch's concern, not GC's.
func (c *Coordinator) Admit(h *history.History, journalRef, event, session, agent, delegation string) error {
	p, e := dispatch.LoadAdmissionPolicy()
	if e != nil {
		return e
	}
	req := dispatch.AdmissionRequest{Event: event, Session: session, Agent: agent, Dispatch: delegation}
	if e := dispatch.Admit(h, p, journalRef, req); e != nil {
		return e
	}
	c.Units++
	return nil
}

var mandateRotation = []MandateClass{MandateDeadCode, MandateComplexity, MandateDuplication, MandateDocumentation, MandateAnalyzerIntegrity}

// Advance starts at most one cycle per trigger; the whole causal closure remains context.
func (c *Coordinator) Advance(ctx context.Context, fs *vfs.FS, h *history.History, entries []vfs.JournalEntry, session string, reach Reach) (Decision, BarrierVerdict, error) {
	c.Observe(entries)
	p, e := LoadTriggerPolicy()
	if e != nil {
		return Decision{}, BarrierVerdict{}, e
	}
	d := Decide(TriggerInput{UnitsDispatched: c.Units, Mutations: c.Mutations, UserRequested: c.Requested, CycleInFlight: c.Cycle != nil, Deferrals: c.Deferrals}, p)
	if c.Cycle != nil {
		return d, BarrierVerdict{}, nil
	}
	if d.Outcome == OutcomeSkip || d.Outcome == OutcomeAbort {
		c.reset()
		return d, BarrierVerdict{}, nil
	}
	if d.Outcome != OutcomeRun {
		return d, BarrierVerdict{}, nil
	}
	proj := h.Project()
	unfinished := unfinishedUnitIDs(proj)
	b := Barrier(BarrierInput{
		Decision: d, Deferrals: c.Deferrals,
		ActiveUnits: len(unfinished), ActiveUnitIDs: unfinished,
		PendingOrdinaryDeltas: fs.PendingOrdinaryDeltas(), PendingDeltaIdentities: fs.PendingOrdinaryDeltaIdentities(),
	})
	c.Deferrals = b.Deferrals
	if !b.Proceed {
		// Due but the workspace is not idle: legitimate, bounded by Deferrals
		// toward MaxDeferrals above. Nothing is held in the meantime.
		return d, b, nil
	}
	id := make([]byte, cycleIDBytes)
	if _, e = rand.Read(id); e != nil {
		// The barrier cleared but the cycle still didn't start: a
		// deterministic failure here must still count toward the abort valve
		// instead of resetting to 0 every tick (PR-MNT-6).
		c.Deferrals++
		return d, b, e
	}
	plan, e := Declare(ctx, entries, Request{SessionID: session, CycleID: hex.EncodeToString(id), Mandate: mandateRotation[c.NextMandate]}, reach)
	if e != nil {
		c.Deferrals++
		return d, b, e
	}
	c.Deferrals = 0
	c.Cycle = &Cycle{Plan: plan, Phase: "baseline", Scope: []string{}, Sessions: map[string]string{}, Started: time.Now().UTC()}
	return d, b, nil
}

// unfinishedUnitIDs names the units ActiveUnits counts: in flight, or planned
// and still awaiting their turn, since the orchestrator is not idle while its
// plan has work left. It lets a stalled barrier say which units block it.
func unfinishedUnitIDs(p history.Projection) []string {
	var out []string
	for id, u := range p.Units {
		if u.State == history.StateInFlight || u.State == history.StatePlanned {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}
func (c *Coordinator) reset() {
	c.Units = 0
	c.Mutations = 0
	c.Deferrals = 0
	c.Requested = false
}

// Close retains closure evidence; rotation advances only for an actual cycle.
func (c *Coordinator) Close(reason string) {
	if c.Cycle == nil {
		return
	}
	c.Cycle.Phase = "closed"
	c.Cycle.Reason = reason
	c.History = append(c.History, *c.Cycle)
	c.Cycle = nil
	c.NextMandate = (c.NextMandate + 1) % len(mandateRotation)
	c.reset()
}

// Attach records the API-created child before prompting it. Identities are never model arguments.
func (c *Coordinator) Attach(role, session string) error {
	if c.Cycle == nil || session == "" {
		return errors.New("gc: no cycle or child session")
	}
	if role != "collector" && role != "verifier" {
		return errors.New("gc: invalid child role")
	}
	for r, s := range c.Cycle.Sessions {
		if s == session && r != role {
			return errors.New("gc: independent child sessions required")
		}
	}
	if old := c.Cycle.Sessions[role]; old != "" && old != session {
		return errors.New("gc: child already attached; reconcile instead of redispatch")
	}
	c.Cycle.Sessions[role] = session
	return nil
}

// Require errors unless session is the persisted participant for role in the
// active cycle, so only attached children can drive cycle phases.
func (c *Coordinator) Require(session, role string) error {
	if c.Cycle == nil || c.Cycle.Sessions[role] == "" || c.Cycle.Sessions[role] != session {
		return errors.New("gc: caller is not the persisted cycle participant")
	}
	return nil
}

// AuthorizedScope bounds mutation paths, never truncates causal context.
func AuthorizedScope(plan Plan, findings []Finding, investigations []Investigation) ([]string, error) {
	p, e := LoadTriggerPolicy()
	if e != nil {
		return nil, e
	}
	out := []string{}
	seen := make(map[string]struct{}, p.Limits.Files)
	n := 0
	for _, f := range findings {
		if !AuthorizedFinding(f, investigations) {
			continue
		}
		if !slices.Contains(plan.Closure, f.Path) {
			return nil, fmt.Errorf("gc: finding outside causal closure: %s", f.Path)
		}
		if n >= p.Limits.Findings {
			break
		}
		if _, ok := seen[f.Path]; !ok {
			if len(seen) >= p.Limits.Files {
				break
			}
			seen[f.Path] = struct{}{}
			out = append(out, f.Path)
		}
		n++
	}
	slices.Sort(out)
	return out, nil
}
