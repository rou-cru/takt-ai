package gc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

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
	// Since is the history position after which admissions count toward the
	// next cycle.
	Since           int `json:"dispatch_after"`
	observedJournal int
	observedHistory int
	Version         int  `json:"version"`
	Units           int  `json:"units"`
	Mutations       int  `json:"mutations"`
	Cursor          int  `json:"cursor"`
	Deferrals       int  `json:"deferrals"`
	NextMandate     int  `json:"next_mandate"`
	Requested       bool `json:"requested"`
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
	// cycleIDBytes is the random byte length used for cycle identifiers.
	cycleIDBytes = 16
	// ProjectConfigPath is the reviewed project configuration a maintenance cycle
	// is dispatched under and judged against.
	ProjectConfigPath = ".takt/gc.json"
)

// LoadCoordinator reads the core state under its workspace lock. Pace counts,
// lane sessions, the cycle's keys and its authorized scope are projections of
// history and the core bindings, never persisted mirrors.
func LoadCoordinator(fs *vfs.FS) (*Coordinator, error) {
	c := &Coordinator{Version: 1, Cursor: -1, Since: -1, observedJournal: -1, observedHistory: -1}
	if raw := fs.Maintenance(); len(raw) > 0 {
		if err := json.Unmarshal(raw, c); err != nil {
			return nil, err
		}
	}
	if c.Version != 1 {
		return nil, errors.New("gc: invalid coordinator state")
	}
	c.NextMandate = len(c.History) % len(mandateRotation)
	if c.Cycle != nil {
		c.Cycle.Sessions = map[string]string{}
		c.Cycle.AuthorKey, c.Cycle.VerifierKey = fs.CycleKeys(c.Cycle.Plan.CycleID)
		c.Cycle.Scope = []string{}
		for _, claim := range fs.OwnershipClaims(c.Cycle.Plan.SessionID) {
			if c.Cycle.AuthorKey != "" && claim.Key == c.Cycle.AuthorKey {
				c.Cycle.Scope = claim.Scope
			}
		}
	}
	return c, nil
}

// SaveCoordinator persists only the facts the coordinator owns; every
// projection LoadCoordinator rebuilds is blanked.
func SaveCoordinator(fs *vfs.FS, c *Coordinator) error {
	stored := *c
	stored.Units, stored.Mutations, stored.NextMandate = 0, 0, 0
	if c.Cycle != nil {
		cycle := withoutProjections(*c.Cycle)
		stored.Cycle = &cycle
	}
	stored.History = make([]Cycle, len(c.History))
	for i, closed := range c.History {
		stored.History[i] = withoutProjections(closed)
	}
	raw, err := json.Marshal(&stored)
	if err != nil {
		return err
	}
	return fs.SetMaintenance(raw)
}

// withoutProjections blanks what a cycle record only mirrors from history and
// the core bindings: its lane sessions, keys and scope.
func withoutProjections(cycle Cycle) Cycle {
	cycle.Sessions, cycle.Scope, cycle.AuthorKey, cycle.VerifierKey = map[string]string{}, []string{}, "", ""
	return cycle
}

// Execution sessions come exclusively from history, including GC lanes.
func (c *Coordinator) ObserveExecutions(h *history.History) {
	c.Units = 0
	c.observedHistory = len(h.Entries()) - 1
	for _, e := range h.Entries() {
		if e.Seq > c.Since && e.Kind == history.KindAdmitted && e.SessionID == c.Session {
			c.Units++
		}
	}
	if c.Cycle == nil {
		return
	}
	c.Cycle.Sessions = map[string]string{}
	for _, d := range h.Project().Executions {
		if !d.Settled && d.Cycle == c.Cycle.Plan.CycleID {
			c.Cycle.Sessions[d.Role] = d.Child
		}
	}
}

// Bind makes session the one the pace counters measure. A cycle is declared
// over its own session's delta (PR-MNT-4), so work a previous root session left
// behind in this workspace never counts toward the next session's cadence.
func (c *Coordinator) Bind(session string) {
	if session == "" || session == c.Session {
		return
	}
	c.reset()
	c.Cursor, c.Since = -1, -1
	c.Session = session
}

// Observe consumes each effective ordinary journal mutation exactly once. Only
// the bound session's mutations count; other sessions' entries are consumed
// without counting.
func (c *Coordinator) Observe(entries []vfs.JournalEntry) {
	c.Mutations = 0
	c.observedJournal = c.Cursor
	for _, e := range entries {
		c.observedJournal = max(c.observedJournal, e.Seq)
		if e.Seq <= c.Cursor || e.CycleID != "" || e.Outcome != "" || e.BeforeHash == e.AfterHash || e.SessionID != "" && e.SessionID != c.Session {
			continue
		}
		if e.Operation == vfs.OpCreate || e.Operation == vfs.OpPatch || e.Operation == vfs.OpDelete {
			c.Mutations++
		}
	}
}

// Held reports whether a maintenance cycle is in flight. It never gates
// ordinary dispatch: an admitted delegation ends the cycle instead.
func (c *Coordinator) Held() bool { return c.Cycle != nil }

// Admit composes ordinary dispatch admission with the session pace counters.
// Maintenance never holds an admission: the caller ends a cycle in flight
// first. Concurrency, plan coverage and recovery budgets belong to package
// dispatch, not GC.
func (c *Coordinator) Admit(h *history.History, journalRef, event, session, agent, delegation string) error {
	c.Bind(session)
	p, e := dispatch.LoadAdmissionPolicy()
	if e != nil {
		return e
	}
	req := dispatch.AdmissionRequest{Event: event, Session: session, Agent: agent, Dispatch: delegation}
	if e := dispatch.Admit(h, p, journalRef, req); e != nil {
		return e
	}
	c.ObserveExecutions(h)
	return nil
}

var mandateRotation = []MandateClass{MandateDeadCode, MandateComplexity, MandateDuplication, MandateDocumentation, MandateAnalyzerIntegrity}

// Advance starts at most one cycle per trigger; the whole causal closure remains context.
func (c *Coordinator) Advance(ctx context.Context, fs *vfs.FS, h *history.History, entries []vfs.JournalEntry, session string, reach Reach) (Decision, BarrierVerdict, error) {
	c.Observe(entries)
	c.ObserveExecutions(h)
	p, e := LoadTriggerPolicy()
	if e != nil {
		return Decision{}, BarrierVerdict{}, e
	}
	d := Decide(TriggerInput{UnitsDispatched: c.Units, Mutations: c.Mutations, UserRequested: c.Requested, CycleInFlight: c.Cycle != nil, Deferrals: c.Deferrals}, p)
	if c.Cycle != nil {
		return d, BarrierVerdict{}, nil
	}
	if d.Outcome == OutcomeSkip || d.Outcome == OutcomeAbort {
		c.Cursor, c.Since = c.observedJournal, c.observedHistory
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
	for _, u := range p.Units {
		if u.State == history.StateInFlight || u.State == history.StatePlanned {
			out = append(out, u.WorkUnitID)
		}
	}
	// Two roots may name a unit alike; the barrier names each once.
	slices.Sort(out)
	return slices.Compact(out)
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
	c.NextMandate = len(c.History) % len(mandateRotation)
	c.Cursor, c.Since = c.observedJournal, c.observedHistory
	c.reset()
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
