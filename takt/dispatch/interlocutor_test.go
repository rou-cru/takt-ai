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

package dispatch

import "testing"

// interlocutorAgent is a real catalog agent whose role resolves to
// model.RoleDirectInterlocutor (IR-1 eligible).
const interlocutorAgent = "pm"

// ineligibleAgent is a real catalog agent whose role does not resolve to
// model.RoleDirectInterlocutor.
const ineligibleAgent = "analyst"

func TestSwitchDeniesIneligibleRole(t *testing.T) {
	h := openHistory(t)
	if Switch(h, "", "root-1", "child-1", ineligibleAgent, "artifact.md") == nil {
		t.Fatal("expected switch to a non-interlocutor role to be denied")
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "" {
		t.Fatalf("expected no holder to be registered, got %q", holder)
	}
}

func TestSwitchSucceedsAndRecordsHolder(t *testing.T) {
	h := openHistory(t)
	if e := Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
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
	h := openHistory(t)
	if e := Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected first switch to succeed, got %v", e)
	}
	if Switch(h, "", "root-1", "child-2", interlocutorAgent, "other.md") == nil {
		t.Fatal("expected a second switch while a holder is active to be denied")
	}
	budgets := h.Project().Budgets("root-1")
	if budgets.InterlocutorHolder != "child-1" {
		t.Fatalf("expected the first holder to remain child-1, got %q", budgets.InterlocutorHolder)
	}
}

func TestHandoffDeniesNonHolderCaller(t *testing.T) {
	h := openHistory(t)
	if e := Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if Handoff(h, "", "root-1", "someone-else", "Standard") == nil {
		t.Fatal("expected handoff from a non-holder session to be denied")
	}
}

func TestHandoffSucceedsForCurrentHolder(t *testing.T) {
	h := openHistory(t)
	if e := Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if e := Handoff(h, "", "root-1", "child-1", "Standard"); e != nil {
		t.Fatalf("expected handoff by the current holder to succeed, got %v", e)
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "" {
		t.Fatalf("expected holder to be cleared, got %q", holder)
	}
}

func TestAbortDeniesInvalidOrigin(t *testing.T) {
	h := openHistory(t)
	if e := Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if Abort(h, "", "root-1", "child-1", "drift", "specialist") == nil {
		t.Fatal("expected abort with an invalid origin to be denied")
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "child-1" {
		t.Fatalf("expected the holder to remain child-1, got %q", holder)
	}
}

func TestAbortSucceedsFromHarnessOrigin(t *testing.T) {
	h := openHistory(t)
	if e := Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if e := Abort(h, "", "root-1", "child-1", "drift", "harness"); e != nil {
		t.Fatalf("expected abort from the harness to succeed, got %v", e)
	}
	if holder := h.Project().Budgets("root-1").InterlocutorHolder; holder != "" {
		t.Fatalf("expected holder to be cleared, got %q", holder)
	}
}

func TestExpectedArtifactReflectsActiveSwitch(t *testing.T) {
	h := openHistory(t)
	if got := ExpectedArtifact(h, "root-1"); got != "" {
		t.Fatalf("expected no expected artifact before any switch, got %q", got)
	}
	if e := Switch(h, "", "root-1", "child-1", interlocutorAgent, "artifact.md"); e != nil {
		t.Fatalf("expected switch to succeed, got %v", e)
	}
	if got := ExpectedArtifact(h, "root-1"); got != "artifact.md" {
		t.Fatalf("expected artifact.md, got %q", got)
	}
}
