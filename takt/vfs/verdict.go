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

package vfs

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/rou-cru/takt-ai/takt/model"
)

// InvariantSet is the reference the verification gate judges a staged delta
// against, in the precedence order PR-VFS-CSL-3 fixes (user goal and
// directives, the work unit's contract, the framework's planning artifacts).
// An entry is either an Engram reference (see EngramInvariant), which pins
// itself because an entry is immutable and a revision is another entry, or a
// workspace-relative path pinned by its content on disk.
type InvariantSet []string

// engramInvariantPrefix marks an invariant that is an Engram entry.
const engramInvariantPrefix = "engram:"

// EngramInvariant is the invariant that names the Engram entry id.
func EngramInvariant(id int64) string {
	return engramInvariantPrefix + strconv.FormatInt(id, 10)
}

// invariantsVersionLocked is the set's version: it changes whenever a declared
// invariant is added, removed, reordered or, for a path, edited, which is what
// makes evidence stop governing under PR-VFS-CSL-7.
func (f *FS) invariantsVersionLocked(set InvariantSet) (string, error) {
	var pinned []string
	for _, invariant := range set {
		if strings.HasPrefix(invariant, engramInvariantPrefix) {
			id, perr := strconv.ParseInt(strings.TrimPrefix(invariant, engramInvariantPrefix), 10, 64)
			if perr != nil || id <= 0 {
				return "", fmt.Errorf("%w: invariant %q is not a valid Engram reference", ErrIdentity, invariant)
			}
			pinned = append(pinned, EngramInvariant(id))
			continue
		}
		base, err := f.physical(invariant)
		if err != nil {
			return "", fmt.Errorf("%w: invariant document %q: %w", ErrIdentity, invariant, err)
		}
		pinned = append(pinned, invariant+"\x00"+hashOf(base.Content, base.Present))
	}
	return hashOf([]byte(strings.Join(pinned, "\n")), true), nil
}

// Verify attaches a verification gate to an exact revision and delta hash.
func (f *FS) Verify(verifier, author AgentID, callID string, expected uint64, deltaHash string, pass bool, finding string) (err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err = f.readyLocked(); err != nil {
		return err
	}
	return f.verifyLocked(verifier, author, callID, expected, deltaHash, pass, finding)
}

// verifyLocked attaches the verdict of gate verifier over author at the stated
// revision and delta hash, bound to the identity of the work it evaluates.
func (f *FS) verifyLocked(verifier, author AgentID, callID string, expected uint64, deltaHash string, pass bool, finding string) (err error) {
	start := len(f.journal)
	defer func() {
		if err != nil && len(f.journal) == start {
			f.appendJournalLocked(JournalEntry{Agent: verifier, Operation: OperationType("verdict"), Outcome: "denied"})
		}
		// The verdict is about the author's staged work: it carries that revision.
		f.correlateLocked(start, verifier, author, callID)
		f.finishLocked(&err)
	}()
	v, a, err := f.admitVerifierLocked(verifier, author, callID)
	if err != nil {
		return err
	}
	d := f.staged[author]
	if err = validVerdict(d, expected, f.deltaHashLocked(author), deltaHash, pass, finding); err != nil {
		return err
	}
	// The verdict names the invariant set it was judged against and the version
	// of that set as the sources read now, not as they read at bind time
	// (PR-VFS-CSL-7).
	version, err := f.invariantsVersionLocked(a.Invariants)
	if err != nil {
		return err
	}
	// The call is spent only by a verdict that lands: a refused one may be
	// attached again under the same call once its cause is gone.
	if err = f.consumeCallLocked(v.SessionID, callID); err != nil {
		return err
	}
	if d.verdicts == nil {
		d.verdicts = make(map[AgentID]*VerificationVerdict)
	}
	// One verdict per judge: its latest replaces its earlier one, whichever
	// delegation of it attached that.
	d.verdicts[v.AgentID] = &VerificationVerdict{Pass: pass, Finding: finding, VerifierID: v.AgentID, VerifierRole: v.Role, Revision: expected, DeltaHash: deltaHash, Invariants: a.Invariants, InvariantsHash: version, WorkUnitID: a.WorkUnitID, AttemptID: a.AttemptID}
	// Findings may contain sensitive source excerpts: retain in private state,
	// never export them in the content-free journal.
	outcome := "rejected"
	if pass {
		outcome = "passed"
	}
	f.appendJournalLocked(JournalEntry{Agent: verifier, Operation: OperationType("verdict"), Outcome: outcome, AfterHash: deltaHash})
	// A failing ordinary gate retains the delta for correction. A GC gate's
	// coordinator owns the full-cycle discard after this verdict is recorded.
	return nil
}

// validVerdict refuses a verdict that does not describe the author's staged
// work as it is now, naming the call that attaches a valid one.
func validVerdict(d *agentDelta, expected uint64, actualHash, deltaHash string, pass bool, finding string) error {
	switch {
	case d == nil:
		return fmt.Errorf("%w: the author has no staged work to judge; verify once it stages work", ErrInvalidVerdict)
	case d.revision != expected:
		return fmt.Errorf("%w: the verdict names revision %d but the staged work is at revision %d; re-verify at revision %d", ErrInvalidVerdict, expected, d.revision, d.revision)
	case actualHash != deltaHash:
		return fmt.Errorf("%w: the verdict names a delta hash the staged work no longer has; re-verify with the current hash", ErrInvalidVerdict)
	case !pass && strings.TrimSpace(finding) == "":
		return fmt.Errorf("%w: a failing verdict states its finding; attach it again with the finding", ErrInvalidVerdict)
	}
	return nil
}

// admitVerifierLocked resolves both bindings: the verifier must be the current
// gate entitled to judge the author, and never the author itself. The caller
// spends callID once the verdict is valid.
func (f *FS) admitVerifierLocked(verifier, author AgentID, callID string) (v, a Identity, err error) {
	v, vok := f.bindings[verifier]
	a, aok := f.bindings[author]
	if !vok || !aok || callID == "" {
		return v, a, fmt.Errorf("%w: a verdict names a bound verifier, a bound author and a call", ErrIdentity)
	}
	// The grant decides first: a caller without verify is told so, whatever
	// binding it names.
	if err = RequireVFSCapability(v.Specialist, model.VFSCapabilityVerify); err != nil {
		return v, a, err
	}
	if !v.verifies(author, a) {
		return v, a, fmt.Errorf("%w: verifier %q is not assigned to judge author %q", ErrIdentity, verifier, author)
	}
	if v.Superseded {
		return v, a, fmt.Errorf("%w: a later delegation of %q replaced gate %q over author %q; only that delegation attaches its verdict", ErrIdentity, v.AgentID, verifier, author)
	}
	if v.AgentID == a.AgentID {
		return v, a, ErrSelfVerification
	}
	return v, a, nil
}

// verdictDescribesLocked reports whether v was attached to the work key's
// binding holds now: the same work unit, attempt and revision. A verdict on
// another attempt, or one recorded without its unit and attempt, stays
// retained and inspectable but neither passes nor refuses.
func (f *FS) verdictDescribesLocked(key AgentID, v *VerificationVerdict) bool {
	binding, ok := f.bindings[key]
	d := f.staged[key]
	return ok && d != nil && v.WorkUnitID == binding.WorkUnitID && v.AttemptID == binding.AttemptID && v.Revision == d.revision
}

// currentVerdictsLocked returns key's verdicts that still describe its staged
// work: the same delta and the same invariant set as when they were judged,
// and the same unit, attempt and revision. A verdict whose invariants can no
// longer be read, or whose subject is another attempt or revision, cannot be
// shown to be current.
func (f *FS) currentVerdictsLocked(key AgentID) []VerdictSummary {
	d := f.staged[key]
	if d == nil {
		return nil
	}
	hash := f.deltaHashLocked(key)
	var current []VerdictSummary
	for _, verifier := range slices.Sorted(maps.Keys(d.verdicts)) {
		v := d.verdicts[verifier]
		if v.DeltaHash != hash {
			continue
		}
		if version, err := f.invariantsVersionLocked(v.Invariants); err != nil || version != v.InvariantsHash {
			continue
		}
		if !f.verdictDescribesLocked(key, v) {
			continue
		}
		current = append(current, VerdictSummary{VerifierKey: verifier, Pass: v.Pass, Finding: v.Finding})
	}
	return current
}

// consolidationAcceptedLocked lets ordinary work through unless a current
// verdict failed and the user has not accepted consolidating it anyway.
func (f *FS) consolidationAcceptedLocked(key AgentID, acceptFailing bool) error {
	if acceptFailing {
		return nil
	}
	return f.failingVerdictsLocked(key)
}

// failingVerdictsLocked refuses with every current failing finding, so the
// orchestrator and the user see each judge's report.
func (f *FS) failingVerdictsLocked(key AgentID) error {
	var findings []string
	for _, v := range f.currentVerdictsLocked(key) {
		if !v.Pass {
			findings = append(findings, fmt.Sprintf("%s: %s", v.VerifierKey, v.Finding))
		}
	}
	if len(findings) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrFailingVerdict, strings.Join(findings, "; "))
}
