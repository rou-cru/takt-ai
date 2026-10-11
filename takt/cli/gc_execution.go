package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/history"
)

func attachGCLane(h *history.History, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if c.Cycle == nil || (r.Event != "" && r.Event != c.Cycle.Plan.CycleID) || r.Child == "" || (r.Role != "collector" && r.Role != "verifier") {
		return nil, errors.New("gc: no active cycle or valid child role; retry gc_request from the root orchestrator")
	}
	if r.Owner == "" {
		return nil, errors.New("gc: a lane names the harness instance that attaches it; run takt-ai setup sync to re-deploy the plugin")
	}
	err := h.AttachMaintenance(c.Cycle.Plan.SessionID, c.Cycle.Plan.CycleID, r.Role, r.Child, r.Owner)
	c.ObserveExecutions(h)
	return c, err
}
func claimGCPrompt(h *history.History, c *gc.Coordinator, r coordinationRequest) (any, error) {
	if c.Cycle == nil || (r.Event != "" && r.Event != c.Cycle.Plan.CycleID) || (r.Phase != "" && r.Phase != c.Cycle.Phase) {
		return map[string]any{"allowed": false}, nil
	}
	d, ok := h.Project().ExecutionForChild(r.Child, "")
	if !ok || d.Terminated || d.Cycle != c.Cycle.Plan.CycleID {
		return nil, fmt.Errorf("gc: child %s is not attached to current cycle", r.Child)
	}
	// Only the instance that attached the lane prompts it, unless its lease lapsed.
	if p := h.Project(); d.Owner != r.Owner && !p.Orphaned(d, time.Now()) {
		return map[string]any{"allowed": false}, nil
	}
	phase := c.Cycle.Phase
	if d.Prompted == phase {
		return map[string]any{"allowed": false}, nil
	}
	err := h.ExecutionFact(d, history.KindPrompt, "", phase, nil)
	return map[string]any{"allowed": err == nil}, err
}
