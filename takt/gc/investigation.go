package gc

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// Investigation records both successful and unsuccessful attempts to refute.
// A candidate is not mutation authority until explicit evidence survives review.
type Investigation struct {
	FindingID string `json:"finding_id"`
	CycleID   string `json:"cycle_id"`
	SessionID string `json:"session_id"`
	Instance  string `json:"instance"`
	// Outcome is refuted, confirmed or proposal.
	Outcome  string    `json:"outcome"`
	Evidence string    `json:"evidence"`
	At       time.Time `json:"at"`
}

// Investigate validates r against the report, stamps it with the cycle
// identity and appends it to prior; conflicting evidence for the same
// finding is refused.
func Investigate(plan Plan, report Report, prior []Investigation, r Investigation) ([]Investigation, error) {
	if err := validateInvestigation(report, r); err != nil {
		return nil, err
	}
	r.CycleID = plan.CycleID
	r.SessionID = plan.SessionID
	if err := checkPrior(prior, r); err != nil {
		if errors.Is(err, errDuplicateInvestigation) {
			return prior, nil
		}
		return nil, err
	}
	return append(append([]Investigation{}, prior...), r), nil
}

var errDuplicateInvestigation = errors.New("gc: duplicate investigation")

func validateInvestigation(report Report, r Investigation) error {
	if strings.TrimSpace(r.Evidence) == "" || r.Instance == "" || r.At.IsZero() {
		return ErrNoEvidence
	}
	if r.Outcome != "refuted" && r.Outcome != "confirmed" && r.Outcome != "proposal" {
		return errors.New("gc: invalid investigation outcome")
	}
	for _, f := range report.Findings {
		if f.ID == r.FindingID {
			return nil
		}
	}
	return ErrUnknownFinding
}

func checkPrior(prior []Investigation, r Investigation) error {
	for _, p := range prior {
		if p.FindingID != r.FindingID {
			continue
		}
		if p.Outcome == r.Outcome && p.Evidence == r.Evidence {
			return errDuplicateInvestigation
		}
		return errors.New("gc: conflicting investigation; retained evidence cannot be replaced")
	}
	return nil
}

// AuthorizedFinding reports whether a confirmed investigation with evidence
// stands behind f and the finding itself is eligible for mutation. This is
// the gate between analysis and source edits.
func AuthorizedFinding(f Finding, rs []Investigation) bool {
	if !eligibleFinding(f) {
		return false
	}
	for _, r := range rs {
		if r.FindingID == f.ID && r.Outcome == "confirmed" && strings.TrimSpace(r.Evidence) != "" {
			return true
		}
	}
	return false
}

func eligibleFinding(f Finding) bool {
	if f.ProposalOnly || f.Introduced || f.Exported || f.Mandate == MandateAnalyzerIntegrity || f.Status == StatusPending || mandateDemoted(f.Mandate) {
		return false
	}
	return f.Tool != "" && f.ToolVersion != "" && f.Rule != "" && f.Evidence != ""
}

// ─── PR-MNT-32: demotion by reversion rate ──────────────────────────────────
//
// A mandate class that keeps having its consolidated work reverted (PR-MNT-31
// attributes those reversions back to the cycle and mandate class that made
// them) loses its authority to mutate on its own and drops to proposal-only.
// demotedMandates is the in-memory record of that loss of autonomy: a cycle
// checks it via eligibleFinding, and EvaluateMandateReversionRate is what
// sets it once the rate crosses reversionRateThreshold. It is process-local
// and reset each run, same as the rest of this package's in-memory state
// (the durable record of the decision is the control-effect event published
// on the bus, not this map).
var (
	demotedMu       sync.Mutex
	demotedMandates = map[MandateClass]bool{}
)

func mandateDemoted(m MandateClass) bool {
	demotedMu.Lock()
	defer demotedMu.Unlock()
	return demotedMandates[m]
}

func demoteMandate(m MandateClass) {
	demotedMu.Lock()
	defer demotedMu.Unlock()
	demotedMandates[m] = true
}

// reversionRateThreshold is the fraction of registered work (the PR-MNT-9/
// PR-MNT-11 denominator, never wall time) that a mandate class's attributed
// reversions may reach before PR-MNT-32 demotes it. Set at 1/4: below that,
// a handful of reverted edits reads as normal review friction; at or above
// it, a quarter of the work this class is responsible for is coming back,
// which is the class's changes failing to stick rather than noise.
const reversionRateThreshold = 0.25

// minRegisteredWorkForRateDecision keeps a demotion decision from firing off
// a denominator too small for a rate to mean anything (one reedit out of two
// units of work is a 50% rate that is pure noise, not a pattern).
const minRegisteredWorkForRateDecision = 8

// MandateRateDecision is the reversion-rate evaluation for one mandate class
// over one range of registered work, kept alongside its raw inputs so the
// rate is never logged or acted on without the counts behind it (PR-MNT-11).
type MandateRateDecision struct {
	Mandate        MandateClass `json:"mandate_class"`
	Reversions     int64        `json:"reversions"`
	RegisteredWork int64        `json:"registered_work"`
	Rate           float64      `json:"rate"`
	Demoted        bool         `json:"demoted"`
}

// EvaluateMandateReversionRate measures how often mandate's consolidated work
// gets reverted (PR-MNT-7): cycle-reedit events (PR-MNT-31) over registered
// session events in (afterID, beforeID], never elapsed time (PR-MNT-9/11).
// Above reversionRateThreshold it demotes mandate to proposal-only (PR-MNT-32)
// and publishes the CONTAIN effect PR-OBS-CTL-5 requires.
func EvaluateMandateReversionRate(store *obs.Store, bus *obs.Bus, clock *obs.Clock, agent, sessionID string, mandate MandateClass, afterID, beforeID int64, workUnitID string) (MandateRateDecision, error) {
	reedits, err := store.Events(sessionID, obs.EventCycleReedit, afterID, beforeID, 0)
	if err != nil {
		return MandateRateDecision{}, err
	}
	var reversions int64
	for _, e := range reedits {
		if mc, _ := e.Envelope.Attributes["mandate_class"].(string); mc == string(mandate) {
			reversions++
		}
	}
	registered, err := store.CountEvents(sessionID, "", afterID, beforeID)
	if err != nil {
		return MandateRateDecision{}, err
	}
	d := MandateRateDecision{Mandate: mandate, Reversions: reversions, RegisteredWork: registered}
	if registered > 0 {
		d.Rate = float64(reversions) / float64(registered)
	}
	if registered >= minRegisteredWorkForRateDecision && d.Rate > reversionRateThreshold {
		demoteMandate(mandate)
		d.Demoted = true
		cond := fmt.Sprintf("gc mandate %s reversion rate %.2f (reversions=%d registered_work=%d) exceeds threshold %.2f",
			mandate, d.Rate, reversions, registered, reversionRateThreshold)
		if err := obs.PublishControlEffectEvent(bus, clock, obs.ActionContain, agent, "gc.mandate-reversion-rate", cond, workUnitID); err != nil {
			return d, err
		}
	}
	return d, nil
}
