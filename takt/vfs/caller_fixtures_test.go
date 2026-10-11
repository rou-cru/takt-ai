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

package vfs_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// recordHistory appends one harness entry about session's unit attempt to the
// execution history under state, as the delegation's dispatch does.
func recordHistory(t *testing.T, state, session, unit, attempt string, kind history.Kind, outcome history.Outcome, agents ...string) {
	t.Helper()
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	agent := "dev"
	if len(agents) > 0 {
		agent = agents[0]
	}
	dispatch := session + "/" + unit + "/" + attempt
	if err := h.Append(history.Entry{Dispatch: dispatch, Agent: agent, Author: history.AuthorHarness, Kind: kind, SessionID: session, WorkUnitID: unit,
		AttemptID: attempt, NodeKind: history.NodeKindDelegated, Cause: history.CauseUncaptured, Outcome: outcome}); err != nil {
		t.Fatal(err)
	}
	if kind == history.KindAdmitted {
		if err := h.Append(history.Entry{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: session, WorkUnitID: unit, AttemptID: attempt, Dispatch: dispatch, Agent: agent, Caller: session, Cause: history.CauseNone}); err != nil {
			t.Fatal(err)
		}
		if err := h.ExecutionFact(h.Project().Executions[dispatch], history.KindPrepared, "", "", nil); err != nil {
			t.Fatal(err)
		}
		if _, err := h.AttachChild(dispatch, hostOf(agent, unit), agent); err != nil {
			t.Fatal(err)
		}
	}

}

// admit records the admission of unit's attempt in the execution history.
func admit(t *testing.T, state, session, unit, attempt string, agents ...string) {
	t.Helper()
	recordHistory(t, state, session, unit, attempt, history.KindAdmitted, "", agents...)
}

// settle records that unit's attempt terminated.
func settle(t *testing.T, state, session, unit, attempt string) {
	t.Helper()
	recordHistory(t, state, session, unit, attempt, history.KindTerminated, history.OutcomeCompleted)
}

// hostOf is the host session a delegation of agent on unit runs in.
func hostOf(agent, unit string) string { return "host/" + agent + "/" + unit }

// callerOf is the caller block of agent's delegation on session's unit.
func callerOf(session, agent, unit string) vfs.Caller {
	return vfs.Caller{SessionID: session, HostSession: hostOf(agent, unit), Agent: vfs.AgentID(agent), WorkUnitID: unit}
}

// orchestrator is the caller block of session's orchestrator.
func orchestrator(session string) vfs.Caller {
	return vfs.Caller{SessionID: session, HostSession: session, Agent: vfs.OrchestratorInstance}
}

// ensureAdmitted admits attempt 1 of unit unless a delegation of it is in flight.
func ensureAdmitted(t *testing.T, f *vfs.FS, state, session, unit, agent string) {
	t.Helper()
	if attempt, err := f.AdmittedAttempt(session, unit); err != nil {
		t.Fatal(err)
	} else if attempt == "" {
		admit(t, state, session, unit, history.FirstAttempt, agent)
	}
}

// delegate assigns scope to agent (specialist of the same name) on session's
// unit, admits the unit and binds it from its delegation's host session.
func delegate(t *testing.T, f *vfs.FS, state, session, agent, unit string, scope ...string) (vfs.Caller, vfs.AgentID) {
	t.Helper()
	if _, err := f.AssignScope(vfs.Identity{SessionID: session, WorkUnitID: unit, AgentID: vfs.AgentID(agent), Specialist: agent}, scope); err != nil {
		t.Fatal(err)
	}
	ensureAdmitted(t, f, state, session, unit, agent)
	c := callerOf(session, agent, unit)
	bound, err := f.BindAs(c, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, bound.Key
}

// judge assigns judge (specialist of the same name) on session's unit a gate
// over each author, admits the unit and adopts the gates.
func judge(t *testing.T, f *vfs.FS, state, session, agent, unit string, authors ...vfs.AgentID) (vfs.Caller, []vfs.AdoptedGate) {
	t.Helper()
	ensureAdmitted(t, f, state, session, unit, agent)
	for _, author := range authors {
		if _, err := f.AssignVerifier(vfs.Identity{SessionID: session, WorkUnitID: unit, AgentID: vfs.AgentID(agent), Specialist: agent}, author); err != nil {
			t.Fatal(err)
		}
	}
	c := callerOf(session, agent, unit)
	gates, err := f.AdoptGates(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, gate := range gates {
		if gate.Error != "" {
			t.Fatalf("gate over %s: %s", gate.AuthorKey, gate.Error)
		}
	}
	return c, gates
}

// write stages content at path through c's binding.
func write(t *testing.T, f *vfs.FS, c vfs.Caller, call, path, content string) vfs.OperationResult {
	t.Helper()
	r, err := f.Operate(c, vfs.CallerOperation{CallID: call, Action: vfs.OpCreate, Path: path, Content: []byte(content)})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// readAs reads path through c's binding, naming author when given.
func readAs(t *testing.T, f *vfs.FS, c vfs.Caller, call, path string, author vfs.AgentID) vfs.OperationResult {
	t.Helper()
	r, err := f.Operate(c, vfs.CallerOperation{CallID: call, Action: vfs.OpRead, Path: path, AuthorKey: author})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func replaceChild(t *testing.T, state, root, unit, attempt, agent, child string) {
	t.Helper()
	h, err := history.Open(state)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	}()
	d := h.Project().Executions[root+"/"+unit+"/"+attempt]
	if err := h.ExecutionFact(d, history.KindChild, child, "", nil); err != nil {
		t.Fatal(err)
	}
}
