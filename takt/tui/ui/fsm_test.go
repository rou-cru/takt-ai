package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

type fsmState int

const (
	stateA fsmState = iota
	stateB
)

type fsmModel struct{ visited int }

func TestTableApplyRunsTheMatchingRule(t *testing.T) {
	table := Table[fsmState, fsmModel]{
		{From: stateA, Event: "go"}: func(m *fsmModel) (fsmState, tea.Cmd) {
			m.visited++
			return stateB, nil
		},
	}
	m := fsmModel{}
	next, _, ok := table.Apply(&m, stateA, "go")
	if !ok || next != stateB || m.visited != 1 {
		t.Fatalf("Apply() = (%v, ok=%v), model = %+v", next, ok, m)
	}
}

func TestTableApplyReportsNoRule(t *testing.T) {
	table := Table[fsmState, fsmModel]{}
	m := fsmModel{}
	next, cmd, ok := table.Apply(&m, stateA, "go")
	if ok || next != stateA || cmd != nil {
		t.Fatalf("Apply() = (%v, cmd=%v, ok=%v), want unchanged state and ok=false", next, cmd, ok)
	}
}
