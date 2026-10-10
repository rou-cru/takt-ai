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

package gc

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

// refutationFileMode keeps persisted refutation evidence private.
const refutationFileMode os.FileMode = 0o600

// EvidenceClass names what the analysis could not see.
type EvidenceClass string

// Evidence classes are the closed set of blind spots a refutation may cite.
const (
	// EvidenceDynamicDispatch cites a call site the analyzer could not resolve statically.
	EvidenceDynamicDispatch EvidenceClass = "dynamic-dispatch"
	// EvidenceReflection cites a reflective access hiding the actual target.
	EvidenceReflection EvidenceClass = "reflection"
	// EvidenceConfigWiring cites wiring decided by configuration, not code.
	EvidenceConfigWiring EvidenceClass = "config-wiring"
	// EvidenceFrameworkEntry cites a live entry the framework invokes directly.
	EvidenceFrameworkEntry EvidenceClass = "framework-entrypoint"
	// EvidenceSerialization cites a type read or written through serialization.
	EvidenceSerialization EvidenceClass = "serialization"
	// EvidenceReExport cites a re-exported symbol whose use is outside the analyzed set.
	EvidenceReExport EvidenceClass = "re-export"
	// EvidenceValueReference cites a value use the analyzer treated as a reference.
	EvidenceValueReference EvidenceClass = "value-reference"
	// EvidencePublicContract cites a published contract consumers rely on.
	EvidencePublicContract EvidenceClass = "public-contract"
)

var evidenceClasses = []EvidenceClass{
	EvidenceDynamicDispatch, EvidenceReflection, EvidenceConfigWiring, EvidenceFrameworkEntry,
	EvidenceSerialization, EvidenceReExport, EvidenceValueReference, EvidencePublicContract,
}

// Refutation errors reject invalid refutation records.
var (
	// ErrUnknownFinding rejects a refutation of an ID the analyzer never produced
	// for the cycle: a record that matches nothing would hide a typo.
	ErrUnknownFinding = errors.New("gc: refutation names a finding the cycle did not produce")
	// ErrNoEvidence rejects a refutation that does not say what the analysis missed.
	ErrNoEvidence = errors.New("gc: refutation needs an evidence class and the evidence found")
)

// Refutation is the outcome of investigating one finding.
type Refutation struct {
	FindingID string        `json:"finding_id"`
	SessionID string        `json:"session_id"`
	CycleID   string        `json:"cycle_id"`
	Class     EvidenceClass `json:"class"`
	// Evidence is where the specialist saw the use the analysis missed.
	Evidence string `json:"evidence"`
	// Instance is the refuting specialist instance.
	Instance string `json:"instance"`
	// At is injected by the caller so the rules stay clock-free.
	At time.Time `json:"at"`
}

// Refute records r against plan's cycle and returns the cycle's refutations.
func Refute(plan Plan, report Report, existing []Refutation, r Refutation) ([]Refutation, error) {
	if !slices.Contains(evidenceClasses, r.Class) || strings.TrimSpace(r.Evidence) == "" || r.Instance == "" || r.At.IsZero() {
		return nil, ErrNoEvidence
	}
	if !slices.ContainsFunc(report.Findings, func(f Finding) bool { return f.ID == r.FindingID }) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownFinding, r.FindingID)
	}
	if slices.ContainsFunc(existing, func(e Refutation) bool { return e.FindingID == r.FindingID }) {
		return existing, nil
	}
	r.SessionID, r.CycleID = plan.SessionID, plan.CycleID
	return append(slices.Clone(existing), r), nil
}

func refuted(refs []Refutation) map[string]bool {
	set := make(map[string]bool, len(refs))
	for _, r := range refs {
		set[r.FindingID] = true
	}
	return set
}

// Actionable returns findings judged dead and not refuted.
func Actionable(findings []Finding, refs []Refutation) []Finding {
	gone := refuted(refs)
	out := []Finding{}
	for _, f := range findings {
		if f.Status == StatusDead && !gone[f.ID] {
			out = append(out, f)
		}
	}
	return out
}

// refutationsFile lives in the private state directory beside the VFS store: like
// verdicts and findings it never enters the content-free journal.
const refutationsFile = "gc-refutations.json"

// LoadRefutations returns the refutations attached to a cycle. A missing file is
// an empty record, not an error.
func LoadRefutations(state, cycleID string) ([]Refutation, error) {
	all, err := readRefutations(state)
	return all[cycleID], err
}

// SaveRefutations replaces the cycle's refutations. Callers serialize writers
// (the CLI holds the workspace lock); the rename keeps a crash from leaving a
// torn file.
func SaveRefutations(state, cycleID string, refs []Refutation) error {
	all, err := readRefutations(state)
	if err != nil {
		return err
	}
	all[cycleID] = refs
	data, err := json.Marshal(all)
	if err != nil {
		return err
	}
	_, err = filemerge.WriteFileAtomic(filepath.Join(state, refutationsFile), data, refutationFileMode)
	return err
}

func readRefutations(state string) (map[string][]Refutation, error) {
	all := map[string][]Refutation{}
	data, err := os.ReadFile(filepath.Join(state, refutationsFile))
	if errors.Is(err, os.ErrNotExist) {
		return all, nil
	}
	if err != nil {
		return all, err
	}
	return all, json.Unmarshal(data, &all)
}
