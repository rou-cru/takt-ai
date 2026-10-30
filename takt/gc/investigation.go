package gc

import (
	"errors"
	"fmt"
	"slices"
	"strings"
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
// stands behind f and the finding itself is eligible for mutation; a finding
// of a demoted mandate class never is (PR-MNT-32). This is the gate between
// analysis and source edits.
func AuthorizedFinding(f Finding, rs []Investigation, demoted []MandateClass) bool {
	if !eligibleFinding(f, demoted) {
		return false
	}
	for _, r := range rs {
		if r.FindingID == f.ID && r.Outcome == "confirmed" && strings.TrimSpace(r.Evidence) != "" {
			return true
		}
	}
	return false
}

func eligibleFinding(f Finding, demoted []MandateClass) bool {
	if f.ProposalOnly || f.Introduced || f.Exported || f.Mandate == MandateAnalyzerIntegrity || f.Status == StatusPending || slices.Contains(demoted, f.Mandate) {
		return false
	}
	return f.Tool != "" && f.ToolVersion != "" && f.Rule != "" && f.Evidence != ""
}

// ─── PR-MNT-32: demotion by reversion rate ──────────────────────────────────
//
// A mandate class that keeps having its consolidated work reverted (PR-MNT-31
// attributes those reversions back to the cycle and mandate class that made
// them) loses its authority to mutate on its own and drops to proposal-only.
// EvaluateMandateReversionRate decides it; the coordinator records it
// durably (Coordinator.Demote), so every later authorization reads it.

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

// MandateReversionRateInput is what EvaluateMandateReversionRate measures: the
// class and session, the registered range (afterID, beforeID], and the acting
// agent and work unit recorded on any CONTAIN effect.
type MandateReversionRateInput struct {
	Agent      string
	SessionID  string
	Mandate    MandateClass
	AfterID    int64
	BeforeID   int64
	WorkUnitID string
}

// EvaluateMandateReversionRate measures how often mandate's consolidated work
// is reverted (PR-MNT-7): cycle-reedit events (PR-MNT-31) over registered
// session events in (afterID, beforeID], never elapsed time (PR-MNT-9/11).
// Above the policy's demotion reversion rate it demotes the mandate to proposal-only
// (PR-MNT-32) and publishes the CONTAIN effect PR-OBS-CTL-5 requires; the
// caller records the demotion in the coordinator.
func EvaluateMandateReversionRate(store *obs.Store, bus *obs.Bus, clock *obs.Clock, in MandateReversionRateInput) (MandateRateDecision, error) {
	reedits, err := store.Events(in.SessionID, obs.EventCycleReedit, in.AfterID, in.BeforeID, 0)
	if err != nil {
		return MandateRateDecision{}, err
	}
	var reversions int64
	for _, e := range reedits {
		if mc, _ := e.Envelope.Attributes["mandate_class"].(string); mc == string(in.Mandate) {
			reversions++
		}
	}
	registered, err := store.CountEvents(in.SessionID, "", in.AfterID, in.BeforeID)
	if err != nil {
		return MandateRateDecision{}, err
	}
	p, err := LoadTriggerPolicy()
	if err != nil {
		return MandateRateDecision{}, err
	}
	threshold, minWork := p.Demotion.ReversionRate, p.Demotion.MinRegisteredWork
	d := MandateRateDecision{Mandate: in.Mandate, Reversions: reversions, RegisteredWork: registered}
	if registered > 0 {
		d.Rate = float64(reversions) / float64(registered)
	}
	if registered >= minWork && d.Rate > threshold {
		d.Demoted = true
		// The record carries every input the decision read, so replaying the
		// same events under the same policy reproduces it.
		cond := fmt.Sprintf("gc mandate %s reversion rate %.2f (reversions=%d registered_work=%d range=(%d,%d]) exceeds threshold %.2f (policy %s v%d, min_registered_work=%d)",
			in.Mandate, d.Rate, reversions, registered, in.AfterID, in.BeforeID, threshold, TriggerPolicyRef, p.Version, minWork)
		if err := obs.PublishControlEffectEvent(bus, clock, obs.ActionContain, in.Agent, "gc.mandate-reversion-rate", cond, in.WorkUnitID); err != nil {
			return d, err
		}
	}
	return d, nil
}
