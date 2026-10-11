package history

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"
)

const (
	KindInputs         Kind = "inputs-declared"
	KindExecution      Kind = "execution-prepared"
	KindChild          Kind = "execution-child"
	KindDelivery       Kind = "execution-delivery"
	KindLaneStopped    Kind = "maintenance-lane-stopped"
	KindLane           Kind = "maintenance-lane"
	KindPrompt         Kind = "maintenance-prompt"
	KindPrepared       Kind = "execution-ready"
	KindExecutionEnded Kind = "execution-ended"
	// KindLease renews a harness instance's claim to the executions it owns.
	KindLease Kind = "harness-lease"
)

// LeaseTTL is how long a harness instance's last lease renewal keeps its
// executions owned: three renewal intervals of the plugin (30 s each), so one
// missed renewal never hands live work to another instance.
const LeaseTTL = 90 * time.Second

// Execution is the current linkage of one exact host dispatch. A unit retry
// creates a new record; no callback may move its predecessor to that attempt.
type Execution struct {
	Dispatch   string  `json:"dispatch"`
	Root       string  `json:"root"`
	Unit       string  `json:"unit"`
	Attempt    string  `json:"attempt"`
	Agent      string  `json:"agent"`
	Caller     string  `json:"caller"`
	Child      string  `json:"child,omitempty"`
	Background bool    `json:"background,omitempty"`
	AdmittedAt int64   `json:"admitted_at"`
	Inputs     []int64 `json:"inputs"`
	Delivery   []int64 `json:"delivery,omitempty"`
	Cycle      string  `json:"cycle,omitempty"`
	Role       string  `json:"role,omitempty"`
	Prompted   string  `json:"prompted,omitempty"`
	Ready      bool    `json:"ready,omitempty"`
	Terminated bool    `json:"terminated,omitempty"`
	Settled    bool    `json:"settled,omitempty"`
	// Owner is the harness instance that prepared or attached this execution;
	// empty for one recorded before owners were.
	Owner string `json:"owner,omitempty"`
}

func executionKind(k Kind) bool {
	switch k {
	case KindInputs, KindExecution, KindChild, KindDelivery, KindLane, KindPrompt, KindExecutionEnded, KindPrepared, KindLease, KindLaneStopped:
		return true
	}
	return false
}
func (e Entry) validateExecution() error {
	if e.SessionID == "" && e.Kind != KindLease {
		return errors.New("history: execution root required")
	}
	if e.Kind == KindLease {
		if e.Author != AuthorHarness || e.Owner == "" || e.CreatedAt <= 0 {
			return errors.New("history: a lease names its harness instance and renewal time")
		}
		return nil
	}
	if e.Kind == KindInputs {
		if e.Author != AuthorOrchestrator || e.WorkUnitID == "" || (len(e.ResultIDs) > 0) == e.None {
			return errors.New("history: declare either consumed result_ids or none: true, exactly one")
		}
		return nil
	}
	if e.Author != AuthorHarness || e.Dispatch == "" || e.AttemptID == "" || e.WorkUnitID == "" {
		return errors.New("history: exact dispatch and attempt required")
	}
	return nil
}
func (p *Projection) foldExecution(e Entry) {
	if e.Kind == KindLease {
		p.Leases[e.Owner] = max(p.Leases[e.Owner], e.CreatedAt)
		return
	}
	if e.Kind == KindInputs {
		p.Inputs[UnitKey(e.SessionID, e.WorkUnitID)] = slices.Clone(e.ResultIDs)
		return
	}
	d := p.Executions[e.Dispatch]
	switch e.Kind {
	case KindExecution, KindLane:
		if d.Dispatch != "" {
			return
		}
		d = Execution{Dispatch: e.Dispatch, Root: e.SessionID, Unit: e.WorkUnitID, Attempt: e.AttemptID, Agent: e.Agent, Caller: e.Caller, Child: e.Child, Background: e.Background, AdmittedAt: e.CreatedAt, Inputs: append([]int64{}, p.Inputs[UnitKey(e.SessionID, e.WorkUnitID)]...), Cycle: e.CycleID, Role: e.Role, Owner: e.Owner}
	case KindPrepared:
		d.Ready = true
	case KindChild:
		d.Child = e.Child
	case KindDelivery:
		for _, id := range e.ResultIDs {
			if !slices.Contains(d.Delivery, id) {
				d.Delivery = append(d.Delivery, id)
			}
		}
	case KindPrompt:
		d.Prompted = e.Phase
	case KindLaneStopped:
		d.Terminated = true
	case KindExecutionEnded:
		d.Settled = true
	}
	p.Executions[e.Dispatch] = d
}

// Orphaned reports whether d's owner has stopped renewing its lease by now, so
// another harness instance may reconcile it. An execution recorded without an
// owner has none to wait for.
func (p Projection) Orphaned(d Execution, now time.Time) bool {
	renewed, ok := p.Leases[d.Owner]
	return d.Owner == "" || !ok || now.Sub(time.UnixMilli(renewed)) > LeaseTTL
}

// ExecutionForChild never uses a host title as authority. Titles only propose
// the initial association, recorded by AttachChild under the workspace lock.
func (p Projection) ExecutionForChild(child, agent string) (Execution, bool) {
	if child == "" {
		return Execution{}, false
	}
	for _, d := range p.Executions {
		if !d.Settled && d.Child == child && (agent == "" || d.Agent == agent) {
			return d, true
		}
	}
	return Execution{}, false
}
func (h *History) ExecutionFact(d Execution, kind Kind, child, phase string, ids []int64) error {
	current, ok := h.Project().Executions[d.Dispatch]
	if !ok || current.Root != d.Root || current.Unit != d.Unit || current.Attempt != d.Attempt || current.Settled {
		return fmt.Errorf("dispatch: %s attempt %s no longer runs; resume the current delegation", d.Dispatch, d.Attempt)
	}
	return h.Append(Entry{Author: AuthorHarness, Kind: kind, SessionID: d.Root, WorkUnitID: d.Unit, AttemptID: d.Attempt, Dispatch: d.Dispatch, Child: child, Phase: phase, ResultIDs: ids, Cause: CauseNone})
}
func (h *History) AttachChild(dispatch, child, agent string) (Execution, error) {
	p := h.Project()
	d, ok := p.Executions[dispatch]
	if !ok || d.Settled || d.Agent != agent || child == "" {
		return Execution{}, fmt.Errorf("dispatch: %s has no active %s delegation; retry the same subagent call", dispatch, agent)
	}
	if !d.Ready && d.Cycle == "" {
		return Execution{}, fmt.Errorf("dispatch: %s preparation incomplete; retry the same subagent call before launching", dispatch)
	}
	if d.Child != "" {
		if d.Child == child {
			return d, nil
		}
		return Execution{}, errors.New("dispatch: another child already holds this dispatch")
	}
	for _, old := range p.Executions {
		if old.Child == child && (old.Dispatch != dispatch && (!old.Settled || old.Cycle != "")) {
			return Execution{}, errors.New("dispatch: child already executes another dispatch; resume its exact delegation")
		}
	}
	if err := h.ExecutionFact(d, KindChild, child, "", nil); err != nil {
		return Execution{}, err
	}
	d.Child = child
	return d, nil
}

// NextAttempt is issued from execution history for ordinary and maintenance
// work alike. A denied ordinary admission does not spend an attempt.
func (p Projection) NextAttempt(root, unit string) string {
	if u, known := p.Unit(root, unit); known {
		if u.State != StateSettled {
			return FirstAttempt
		}
		n, err := strconv.Atoi(u.AttemptID)
		if err != nil {
			return FirstAttempt
		}
		return strconv.Itoa(n + 1)
	}
	n := 0
	for _, d := range p.Executions {
		if d.Root == root && d.Unit == unit {
			a, _ := strconv.Atoi(d.Attempt)
			n = max(n, a)
		}
	}
	return strconv.Itoa(n + 1)
}

// LaneUnit is the work unit and catalog agent of a maintenance cycle's lane
// for role: the collector runs as the cycle itself, the verifier as its own unit.
func LaneUnit(cycle, role string) (unit, agent string) {
	if role == "verifier" {
		return cycle + "/verify", "verify"
	}
	return cycle, "simplify"
}

// AttachMaintenance records a native lane under the cycle the core authorized.
func (h *History) AttachMaintenance(root, cycle, role, child, owner string) error {
	if cycle == "" || root == "" || child == "" || (role != "collector" && role != "verifier") {
		return errors.New("gc: no active cycle or valid child role")
	}
	for _, d := range h.Project().Executions {
		if d.Child == child && (d.Cycle != cycle || d.Role != role) && (!d.Settled || d.Cycle != "") {
			return errors.New("gc: child already belongs to another dispatch; use the current lane session")
		}
		if d.Settled || d.Cycle != cycle {
			continue
		}
		if d.Role == role {
			if d.Child == child {
				return nil
			}
			return errors.New("gc: child already attached; resume the existing session")
		}
		if d.Child == child {
			return errors.New("gc: independent child sessions required")
		}
	}
	unit, agent := LaneUnit(cycle, role)
	return h.Append(Entry{Author: AuthorHarness, Kind: KindLane, SessionID: root, WorkUnitID: unit, AttemptID: h.Project().NextAttempt(root, unit), Dispatch: cycle + "/" + role, Agent: agent, Caller: root, Child: child, CycleID: cycle, Role: role, Owner: owner, CreatedAt: time.Now().UnixMilli(), Cause: CauseNone})
}
