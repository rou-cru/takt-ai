package gc

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/vfs"
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
// rate is never logged or acted on without the counts behind it (PR-MNT-32).
type MandateRateDecision struct {
	Mandate        MandateClass `json:"mandate_class"`
	Reversions     int64        `json:"reversions"`
	RegisteredWork int64        `json:"registered_work"`
	Rate           float64      `json:"rate"`
	Demoted        bool         `json:"demoted"`
}

// MandateReversionRateInput is what EvaluateMandateReversionRate measures: the
// class and session, and the acting agent and work unit recorded on any
// CONTAIN effect.
type MandateReversionRateInput struct {
	Agent      string
	SessionID  string
	Mandate    MandateClass
	WorkUnitID string
}

// EvaluateMandateReversionRate counts distinct consolidated cycle/path pairs
// and their attributed re-edits from the journal. Reads, repeated edits and
// unrelated work cannot change the denominator. No registered work publishes no
// rate, preserving the last measured value on the bus.
func EvaluateMandateReversionRate(entries []vfs.JournalEntry, bus *obs.Bus, clock *obs.Clock, in MandateReversionRateInput) (MandateRateDecision, error) {
	type subject struct{ cycle, path string }
	registeredWork, revertedWork := map[subject]bool{}, map[subject]bool{}
	for _, e := range entries {
		if e.SessionID != in.SessionID || e.Outcome != "" || e.Path == "" {
			continue
		}
		if e.Operation == vfs.OpFlush && e.CycleID != "" && e.MandateClass == string(in.Mandate) {
			registeredWork[subject{e.CycleID, e.Path}] = true
		}
		if (e.Operation == vfs.OpCreate || e.Operation == vfs.OpPatch || e.Operation == vfs.OpDelete) && e.ReeditOfCycleID != "" && e.ReeditOfMandateClass == string(in.Mandate) {
			revertedWork[subject{e.ReeditOfCycleID, e.Path}] = true
		}
	}
	var reversions int64
	for key := range revertedWork {
		if registeredWork[key] {
			reversions++
		}
	}
	registered := int64(len(registeredWork))
	d := MandateRateDecision{Mandate: in.Mandate, Reversions: reversions, RegisteredWork: registered}
	if registered == 0 {
		return d, nil
	}
	p, err := LoadTriggerPolicy()
	if err != nil {
		return MandateRateDecision{}, err
	}
	threshold, minWork := p.Demotion.ReversionRate, p.Demotion.MinRegisteredWork
	d.Rate = float64(reversions) / float64(registered)
	signal := "cycle_reedit:" + string(in.Mandate)
	if err := obs.PublishProblemRateEvent(bus, clock, in.Agent, signal, reversions, registered, d.Rate, in.WorkUnitID); err != nil {
		return d, err
	}
	if registered >= minWork && d.Rate > threshold {
		d.Demoted = true
		// The record carries every input the decision read, so replaying the
		// same events under the same policy reproduces it.
		cond := fmt.Sprintf("gc mandate %s reversion rate %.2f (reversions=%d registered_work=%d) exceeds threshold %g (policy %s v%d, min_registered_work=%d)",
			in.Mandate, d.Rate, reversions, registered, threshold, TriggerPolicyRef, p.Version, minWork)
		if err := obs.PublishControlEffectEvent(bus, clock, obs.ActionContain, in.Agent, "gc.mandate-reversion-rate", cond, in.WorkUnitID); err != nil {
			return d, err
		}
	}
	return d, nil
}
