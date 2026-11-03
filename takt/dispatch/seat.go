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

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/model"
)

// UnprovenModelCause prefixes the refusal of an autonomous reassignment to a
// model this session has not seen complete work; the user may approve it.
const UnprovenModelCause = "seat/unproven-model"

// Seat is what a delegation declares about the seat it fills (PR-CRW-13):
// the model it runs, the instance's configured model, and, when it restores
// an unavailable seat, the reason and whether a substitute fills it.
type Seat struct {
	Agent         string
	Model         string
	Configured    string
	Reason        string
	Substitute    bool
	UserConfirmed bool
}

// Restores reports whether the delegation restores the seat: another model
// than the configured one, or a substitute.
func (s Seat) Restores() bool {
	return s.Substitute || (s.Model != "" && s.Model != s.Configured)
}

// ValidateSeat refuses a restoration PR-CRW-13 does not admit, before any
// admission is spent: the orchestrator seat, a restoration without a reason
// or model, a substitute before any reassignment of that seat, and, without
// the user's approval, a model this session has not seen complete work.
func ValidateSeat(h *history.History, root string, s Seat) error {
	if !s.Restores() {
		return nil
	}
	role, e := interlocutorRole(s.Agent)
	if e != nil {
		return e
	}
	if role == model.RoleOrchestrator {
		return errors.New("dispatch: the orchestrator seat is never restored by delegation; crew execution stops and the user decides its recovery")
	}
	if strings.TrimSpace(s.Reason) == "" || s.Model == "" {
		return fmt.Errorf("dispatch: restoring the %s seat names the model it runs and a seat_reason stating why the instance cannot run; retry the subagent call with both", s.Agent)
	}
	p := h.Project()
	if s.Substitute && !reassignmentFailed(h.Entries(), root, s.Agent) {
		return fmt.Errorf("dispatch: a substitute fills the %s seat only after a model reassignment of that seat is recorded and still fails; reassign its model first", s.Agent)
	}
	if !s.UserConfirmed && !proven(p, root, s.Model) {
		return fmt.Errorf("%s: model %s has completed no work in this session; with the user present, their approval admits it", UnprovenModelCause, s.Model)
	}
	return nil
}

// reassignmentFailed reports whether a model reassignment of agent's seat in
// root is recorded and the attempt it ran ended failed or interrupted: the
// recorded events that show no reassignment works (PR-CRW-13).
func reassignmentFailed(entries []history.Entry, root, agent string) bool {
	type attempt struct{ unit, id string }
	reassigned := map[attempt]bool{}
	for _, e := range entries {
		if e.SessionID != root {
			continue
		}
		switch {
		case e.Kind == history.KindSeatRestored && e.Agent == agent && !e.Substitute:
			reassigned[attempt{e.WorkUnitID, e.AttemptID}] = true
		case e.Kind == history.KindTerminated && (e.Outcome == history.OutcomeFailed || e.Outcome == history.OutcomeInterrupted) && reassigned[attempt{e.WorkUnitID, e.AttemptID}]:
			return true
		}
	}
	return false
}

// proven reports whether model completed a unit of root.
func proven(p history.Projection, root, modelID string) bool {
	for _, d := range p.Executions {
		if d.Root != root || d.Model != modelID {
			continue
		}
		if u, ok := p.Unit(root, d.Unit); ok && u.AttemptID == d.Attempt && u.Outcome == history.OutcomeCompleted {
			return true
		}
	}
	return false
}

// SeatEntry is the execution-history record of an admitted restoration.
func SeatEntry(root, unit, attempt string, s Seat) history.Entry {
	return history.Entry{
		Author: history.AuthorOrchestrator, Kind: history.KindSeatRestored,
		SessionID: root, WorkUnitID: unit, AttemptID: attempt,
		Agent: s.Agent, Model: s.Model, Reason: s.Reason, Substitute: s.Substitute, Cause: history.CauseNone,
	}
}

// SubstituteIdentity returns the installed PERSONA and SOUL texts of agent's
// instance: what a substitute's composed prompt leaves out.
func SubstituteIdentity(agent string) ([]string, error) {
	pack, e := catalog.LoadPackages()
	if e != nil {
		return nil, e
	}
	for _, def := range pack.Agents {
		if !slices.Contains(def.Instances, agent) {
			continue
		}
		var texts []string
		for _, name := range []string{def.Context.Persona, def.Context.Soul} {
			if name == "" {
				continue
			}
			raw, e := fs.ReadFile(catalog.AssetFS(), path.Join("agents", def.ID, name))
			if e != nil {
				return nil, e
			}
			resolved, e := ResolvePolicyPlaceholders(raw)
			if e != nil {
				return nil, e
			}
			texts = append(texts, strings.TrimSpace(string(resolved)))
		}
		return texts, nil
	}
	return nil, fmt.Errorf("dispatch: %q is not a Takt crew member", agent)
}
