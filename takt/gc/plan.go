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
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/vfs"
)

// MandateClass is one family of work in the collector's mandate.
type MandateClass string

// Mandate classes are the closed set of work families a cycle can declare.
const (
	// MandateDeadCode removes code the analysis proves unreachable.
	MandateDeadCode MandateClass = "dead-code"
	// MandateComplexity reduces hotspots the complexity analysis flags.
	MandateComplexity MandateClass = "complexity"
	// MandateDuplication merges duplicated blocks the duplication analysis flags.
	MandateDuplication MandateClass = "duplication"
	// MandateDocumentation repairs documentation the analysis finds missing or stale.
	MandateDocumentation MandateClass = "documentation"
	// MandateAnalyzerIntegrity repairs the analyzers themselves.
	MandateAnalyzerIntegrity MandateClass = "analyzer-integrity"
)

// Reachability labels how far the closure could be computed.
const (
	// ReachCodegraph means the closure was computed through the code graph.
	ReachCodegraph = "codegraph"
	// ReachJournalOnly means the closure fell back to the session delta alone.
	ReachJournalOnly = "journal-only"
)

// Reach answers what lies outward from a file: it returns the files reachable
// from path through the code graph. A nil Reach means no code graph is
// available, so the plan is usable without one.
type Reach func(ctx context.Context, path string) ([]string, error)

// Request is what the caller declares; the harness owns cycle identity and
// which mandate class runs.
type Request struct {
	SessionID string       `json:"session_id"`
	CycleID   string       `json:"cycle_id"`
	Mandate   MandateClass `json:"mandate_class"`
}

// Change is one path of the session's delta.
type Change struct {
	Path string `json:"path"`
	// Introduced marks a path the session created: with no consumer yet it is
	// pending, not dead (PR-MNT-26).
	Introduced bool `json:"introduced,omitempty"`
	Deleted    bool `json:"deleted,omitempty"`
}

// Plan is the cycle declaration recorded before the cycle starts.
type Plan struct {
	Request
	Delta []Change `json:"delta"`
	// Closure is the delta plus everything reachable from it, sorted. Tests are
	// not filtered here: excluding them is the dead-code analysis's job (PR-MNT-25).
	Closure []string `json:"closure"`
	// Reachability is ReachCodegraph, or ReachJournalOnly when the closure fell
	// back to the delta alone. Gap says why: a recorded gap, never silent (PR-MNT-24).
	Reachability string `json:"reachability"`
	Gap          string `json:"gap,omitempty"`
}

// SessionDelta returns the paths the session changed and did not roll back.
func SessionDelta(entries []vfs.JournalEntry, sessionID string) []Change {
	flushed, staged := map[string]Change{}, map[string]Change{}
	for _, e := range entries {
		if !sessionEntry(e, sessionID) {
			continue
		}
		applyJournalEntry(e, staged, flushed)
	}
	var delta []Change
	for _, c := range mergeAll(flushed, staged) {
		if !c.Introduced || !c.Deleted {
			delta = append(delta, c)
		}
	}
	slices.SortFunc(delta, func(a, b Change) int { return strings.Compare(a.Path, b.Path) })
	return delta
}

func sessionEntry(e vfs.JournalEntry, sessionID string) bool {
	return e.Path != "" && e.Outcome == "" && e.CycleID == "" && (e.SessionID == "" || e.SessionID == sessionID)
}

func applyJournalEntry(e vfs.JournalEntry, staged, flushed map[string]Change) {
	switch e.Operation {
	case vfs.OpCreate, vfs.OpPatch, vfs.OpDelete:
		staged[e.Path] = merge(staged[e.Path], Change{Path: e.Path, Introduced: e.Operation == vfs.OpCreate && e.BeforeHash == "", Deleted: e.Operation == vfs.OpDelete})
	case vfs.OpRollback:
		delete(staged, e.Path)
	case vfs.OpFlush:
		delete(staged, e.Path)
		flushed[e.Path] = merge(flushed[e.Path], Change{Path: e.Path, Introduced: e.BeforeHash == "" && e.AfterHash != "", Deleted: e.AfterHash == ""})
	}
}

// merge folds a later change into an earlier one: the path stays introduced once
// introduced, and its liveness is the latest operation's.
func merge(prev, next Change) Change {
	next.Introduced = next.Introduced || prev.Introduced
	return next
}

func mergeAll(flushed, staged map[string]Change) map[string]Change {
	all := make(map[string]Change, len(flushed)+len(staged))
	for p, c := range flushed {
		all[p] = c
	}
	for p, c := range staged {
		all[p] = merge(all[p], c)
	}
	return all
}

// Declare builds the cycle declaration.
func Declare(ctx context.Context, entries []vfs.JournalEntry, req Request, reach Reach) (Plan, error) {
	if req.SessionID == "" || req.CycleID == "" {
		return Plan{}, errors.New("gc: session and cycle identifiers are required")
	}
	switch req.Mandate {
	case MandateDeadCode, MandateComplexity, MandateDuplication, MandateDocumentation, MandateAnalyzerIntegrity:
	default:
		return Plan{}, fmt.Errorf("gc: unknown mandate class %q: a cycle declares exactly one", req.Mandate)
	}
	p := Plan{Request: req, Delta: SessionDelta(entries, req.SessionID), Reachability: ReachJournalOnly, Gap: "no code graph available"}
	var reached []string
	if reach != nil {
		var err error
		if reached, err = dependents(ctx, reach, p.Delta); err == nil {
			p.Reachability, p.Gap = ReachCodegraph, ""
		} else {
			p.Gap = err.Error()
		}
	}
	set := map[string]struct{}{}
	for _, c := range p.Delta {
		set[c.Path] = struct{}{}
	}
	for _, path := range reached {
		set[path] = struct{}{}
	}
	p.Closure = make([]string, 0, len(set))
	for path := range set {
		p.Closure = append(p.Closure, path)
	}
	slices.Sort(p.Closure)
	return p, nil
}

// dependents unions what is reachable from every delta path, failing whole on the
// first error so a partial closure is never mistaken for a complete one.
func dependents(ctx context.Context, reach Reach, delta []Change) ([]string, error) {
	var all []string
	for _, c := range delta {
		deps, err := reach(ctx, c.Path)
		if err != nil {
			return nil, err
		}
		all = append(all, deps...)
	}
	return all, nil
}
