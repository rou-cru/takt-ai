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

package dispatch_test

import (
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
)

// interlocutorAgent is a real catalog agent whose role resolves to
// model.RoleDirectInterlocutor (IR-1 eligible).
const interlocutorAgent = "pm"

// ineligibleAgent is a real catalog agent whose role does not resolve to
// model.RoleDirectInterlocutor.
const ineligibleAgent = "analyst"

func TestSwitchDeniesIneligibleRole(t *testing.T) {
	h := newHistory(t)
	if dispatch.Switch(h, "", "root-1", "child-1", ineligibleAgent, "artifact.md") == nil {
		t.Fatal("expected switch to a non-interlocutor role to be denied")
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "" {
		t.Fatalf("expected no holder to be registered, got %q", holder)
	}
}

func TestSwitchSucceedsAndRecordsHolder(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	budgets := h.Project().Budgets("root-1")
	if budgets.InterlocutorHolder != "child-1" {
		t.Fatalf("expected holder child-1, got %q", budgets.InterlocutorHolder)
	}
	if budgets.InterlocutorAgent != interlocutorAgent {
		t.Fatalf("expected agent %q, got %q", interlocutorAgent, budgets.InterlocutorAgent)
	}
	if budgets.InterlocutorArtifact != "artifact.md" {
		t.Fatalf("expected artifact artifact.md, got %q", budgets.InterlocutorArtifact)
	}
}

func TestSwitchRefusesChaining(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected first switch to succeed, got %v", e)
	}
	if dispatch.Switch(h, "", "root-1", "child-2", interlocutorAgent, "other.md") == nil {
		t.Fatal("expected a second switch while a holder is active to be denied")
	}
	budgets := h.Project().Budgets("root-1")
	if budgets.InterlocutorHolder != "child-1" {
		t.Fatalf("expected the first holder to remain child-1, got %q", budgets.InterlocutorHolder)
	}
}

func TestHandoffDeniesNonHolderCaller(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if dispatch.Handoff(h, "", "root-1", "someone-else", "Standard") == nil {
		t.Fatal("expected handoff from a non-holder session to be denied")
	}
}

func TestHandoffSucceedsForCurrentHolder(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if e := dispatch.Handoff(h, "", "root-1", "child-1", "Standard"); e != nil {
		t.Fatalf("expected handoff by the current holder to succeed, got %v", e)
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "" {
		t.Fatalf("expected holder to be cleared, got %q", holder)
	}
}

func TestAbortDeniesInvalidOrigin(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if dispatch.Abort(h, "", "root-1", "child-1", "drift", "specialist") == nil {
		t.Fatal("expected abort with an invalid origin to be denied")
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "child-1" {
		t.Fatalf("expected the holder to remain child-1, got %q", holder)
	}
}

func TestAbortSucceedsFromHarnessOrigin(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if e := dispatch.Abort(h, "", "root-1", "child-1", "drift", "harness"); e != nil {
		t.Fatalf("expected abort from the harness to succeed, got %v", e)
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "" {
		t.Fatalf("expected holder to be cleared, got %q", holder)
	}
}

func TestExpectedArtifactReflectsActiveSwitch(t *testing.T) {
	h := newHistory(t)
	if got := dispatch.ExpectedArtifact(h, "root-1"); got != "" {
		t.Fatalf("expected no expected artifact before any switch, got %q", got)
	}
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if got := dispatch.ExpectedArtifact(h, "root-1"); got != "artifact.md" {
		t.Fatalf("expected artifact.md, got %q", got)
	}
}

func TestAbortWithoutHolderIsRefusedAndRecorded(t *testing.T) {
	h := newHistory(t)
	e := dispatch.Abort(h, "", "root-1", "child-1", "drift", "harness")
	if e == nil || !strings.Contains(e.Error(), "no interlocutor switch is active") {
		t.Fatalf("expected a no-holder refusal, got %v", e)
	}
	entries := h.Entries()
	last := entries[len(entries)-1]
	if last.Kind != history.KindDenied || last.Cause != history.CauseInterlocutorHolder {
		t.Fatalf("expected a recorded holder denial, got %+v", last)
	}
}

func TestAbortOfNonHolderChildIsRefused(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	e := dispatch.Abort(h, "", "root-1", "other", "drift", "harness")
	if e == nil || !strings.Contains(e.Error(), "not the current interlocutor holder") {
		t.Fatalf("expected a mismatch refusal, got %v", e)
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "child-1" {
		t.Fatalf("expected the holder to remain child-1, got %q", holder)
	}
}

func TestAbortWithEmptyChildAbortsTheCurrentHolder(t *testing.T) {
	h := newHistory(t)
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if e := dispatch.Abort(h, "", "root-1", "", "drift", "harness"); e != nil {
		t.Fatalf("expected the empty-child abort to succeed, got %v", e)
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "" {
		t.Fatalf("expected holder to be cleared, got %q", holder)
	}
}

func TestAbortWithEmptyChildAndNoHolderIsRefused(t *testing.T) {
	h := newHistory(t)
	e := dispatch.Abort(h, "", "root-1", "", "drift", "harness")
	if e == nil || !strings.Contains(e.Error(), "no interlocutor switch is active") {
		t.Fatalf("expected a no-holder refusal, got %v", e)
	}
}

func TestCurrentHolderReadsTheHolderSession(t *testing.T) {
	h := newHistory(t)
	if got := dispatch.CurrentHolder(h, "root-1"); got != "" {
		t.Fatalf("expected no holder before any switch, got %q", got)
	}
	if e := dispatch.Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if got := dispatch.CurrentHolder(h, "root-1"); got != "child-1" {
		t.Fatalf("expected holder child-1, got %q", got)
	}
}

func TestHeldRootFindsOnlyTheMatchingRoot(t *testing.T) {
	h := newHistory(t)
	for _, pair := range [][2]string{{"root-1", "child-1"}, {"root-2", "child-2"}} {
		if e := dispatch.Switch(h, "", pair[0], pair[1], interlocutorAgent, "artifact.md"); e != nil {
			t.Fatalf("switch: %v", e)
		}
	}
	if root, ok := h.Project().HeldRoot("child-2"); !ok || root != "root-2" {
		t.Fatalf("expected root-2, got %q %v", root, ok)
	}
	if _, ok := h.Project().HeldRoot("stranger"); ok {
		t.Fatal("a non-holder must hold no root")
	}
	if _, ok := h.Project().HeldRoot(""); ok {
		t.Fatal("an empty session must hold no root")
	}
	if e := dispatch.Abort(h, "", "root-2", "child-2", "gone", "harness"); e != nil {
		t.Fatalf("abort: %v", e)
	}
	if _, ok := h.Project().HeldRoot("child-2"); ok {
		t.Fatal("an aborted holder must hold no root")
	}
	if root, ok := h.Project().HeldRoot("child-1"); !ok || root != "root-1" {
		t.Fatalf("the other root's holder must remain, got %q %v", root, ok)
	}
}
