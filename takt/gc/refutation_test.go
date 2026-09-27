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

package gc_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/gc"
)

var (
	refPlan   = gc.Plan{Request: gc.Request{SessionID: "s", CycleID: "c1", Mandate: gc.MandateDeadCode}}
	refReport = gc.Report{Findings: []gc.Finding{
		{ID: "dead-code:a.go#Used", Status: gc.StatusDead},
		{ID: "dead-code:a.go#Gone", Status: gc.StatusDead},
		{ID: "dead-code:b.go#New", Status: gc.StatusPending},
	}}
	refAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
)

func refutation(id string) gc.Refutation {
	return gc.Refutation{FindingID: id, Class: gc.EvidenceValueReference, Evidence: "a.go:10 passes Used as a handler", Instance: "simplifier-1", At: refAt}
}

func TestRefuteRecordsAgainstPlanCycle(t *testing.T) {
	r := refutation("dead-code:a.go#Used")
	r.CycleID, r.SessionID = "other", "other"
	refs, err := gc.Refute(refPlan, refReport, nil, r)
	if err != nil || len(refs) != 1 {
		t.Fatalf("refs = %v, err = %v", refs, err)
	}
	if refs[0].CycleID != "c1" || refs[0].SessionID != "s" || !refs[0].At.Equal(refAt) || refs[0].Instance != "simplifier-1" {
		t.Fatalf("record = %+v", refs[0])
	}
}

func TestRefuteRejectsWithoutEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*gc.Refutation){
		"no evidence text": func(r *gc.Refutation) { r.Evidence = "  " },
		"unknown class":    func(r *gc.Refutation) { r.Class = "hunch" },
		"no class":         func(r *gc.Refutation) { r.Class = "" },
		"no instance":      func(r *gc.Refutation) { r.Instance = "" },
		"no timestamp":     func(r *gc.Refutation) { r.At = time.Time{} },
	} {
		r := refutation("dead-code:a.go#Used")
		mutate(&r)
		if _, err := gc.Refute(refPlan, refReport, nil, r); !errors.Is(err, gc.ErrNoEvidence) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestRefuteRejectsUnknownFinding(t *testing.T) {
	if _, err := gc.Refute(refPlan, refReport, nil, refutation("dead-code:a.go#Typo")); !errors.Is(err, gc.ErrUnknownFinding) {
		t.Fatalf("err = %v", err)
	}
}

func TestRefuteTwiceIsIdempotent(t *testing.T) {
	first, err := gc.Refute(refPlan, refReport, nil, refutation("dead-code:a.go#Used"))
	if err != nil {
		t.Fatal(err)
	}
	again := refutation("dead-code:a.go#Used")
	again.Evidence, again.At = "different words", refAt.Add(time.Hour)
	second, err := gc.Refute(refPlan, refReport, first, again)
	if err != nil || len(second) != 1 || second[0] != first[0] {
		t.Fatalf("second = %+v, err = %v", second, err)
	}
}

func TestRefutedFindingLeavesActionable(t *testing.T) {
	refs, err := gc.Refute(refPlan, refReport, nil, refutation("dead-code:a.go#Used"))
	if err != nil {
		t.Fatal(err)
	}
	got := gc.Actionable(refReport.Findings, refs)
	if len(got) != 1 || got[0].ID != "dead-code:a.go#Gone" {
		t.Fatalf("actionable = %+v", got)
	}
	// The finding is discarded from the actionable set, not from the report: the record keeps both.
	if len(refReport.Findings) != 3 {
		t.Fatal("report mutated")
	}
}

func TestRefutationsPersistPerCycleInPrivateState(t *testing.T) {
	state := t.TempDir()
	if refs, err := gc.LoadRefutations(state, "c1"); err != nil || len(refs) != 0 {
		t.Fatalf("empty state: %v, %v", refs, err)
	}
	refs, err := gc.Refute(refPlan, refReport, nil, refutation("dead-code:a.go#Used"))
	if err != nil {
		t.Fatal(err)
	}
	if err = gc.SaveRefutations(state, "c1", refs); err != nil {
		t.Fatal(err)
	}
	if err = gc.SaveRefutations(state, "c2", nil); err != nil {
		t.Fatal(err)
	}
	got, err := gc.LoadRefutations(state, "c1")
	if err != nil || len(got) != 1 || got[0] != refs[0] {
		t.Fatalf("reloaded = %+v, err = %v", got, err)
	}
	if other, _ := gc.LoadRefutations(state, "c2"); len(other) != 0 {
		t.Fatalf("cycle c2 sees %v", other)
	}
	info, err := os.Stat(filepath.Join(state, "gc-refutations.json"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("file: %v, %v", info, err)
	}
}

func TestLoadRefutationsRejectsCorruptFile(t *testing.T) {
	state := t.TempDir()
	if err := os.WriteFile(filepath.Join(state, "gc-refutations.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := gc.LoadRefutations(state, "c1"); err == nil {
		t.Fatal("corrupt store read as empty")
	}
}
