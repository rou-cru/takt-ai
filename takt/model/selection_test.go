package model

import (
	"testing"
)

// TestSelectionSetupDefault verifies the default setup choice.
func TestSelectionSetupDefault(t *testing.T) {
	s := Selection{}
	if s.Setup != SetupDefault {
		t.Errorf("Selection.Setup zero value = %q, want %q (SetupDefault)", s.Setup, SetupDefault)
	}
}

func TestSelectionSetupCustom(t *testing.T) {
	s := Selection{Setup: SetupCustom}
	if s.Setup != SetupCustom {
		t.Errorf("Selection.Setup = %q, want %q", s.Setup, SetupCustom)
	}
}

func TestSelectionModelOverrides(t *testing.T) {
	s := Selection{ModelOverrides: map[string]ModelAssignment{
		SubAgentTaktJudgeA: {Model: "custom-model", Effort: "high"},
	}}
	if s.ModelOverrides[SubAgentTaktJudgeA].Model != "custom-model" {
		t.Fatal("model override was not retained")
	}
	o := SyncOverrides{ModelOverrides: s.ModelOverrides}
	if o.ModelOverrides == nil {
		t.Fatal("sync model overrides should be retained")
	}
}

// TestSelectionNonDetectedAgents verifies undetected target tracking.
func TestSelectionNonDetectedAgents(t *testing.T) {
	s := Selection{}
	if s.NonDetectedAgents != nil {
		t.Errorf("Selection.NonDetectedAgents zero value = %v, want nil", s.NonDetectedAgents)
	}

	s.NonDetectedAgents = []AgentID{AgentCodex}
	if len(s.NonDetectedAgents) != 1 || s.NonDetectedAgents[0] != AgentCodex {
		t.Errorf("Selection.NonDetectedAgents = %v, want [codex]", s.NonDetectedAgents)
	}
}

func TestSelectionHasNonDetectedAgent(t *testing.T) {
	s := Selection{NonDetectedAgents: []AgentID{AgentCodex}}

	if !s.IsNonDetected(AgentCodex) {
		t.Error("IsNonDetected(AgentCodex) = false, want true")
	}
	if s.IsNonDetected(AgentOpenCode) {
		t.Error("IsNonDetected(AgentOpenCode) = true, want false")
	}
	if s.IsNonDetected("") {
		t.Error("IsNonDetected(\"\") = true, want false")
	}
}

// TestSelectionPreservedSubagents verifies preserved sub-agent names.
func TestSelectionPreservedSubagents(t *testing.T) {
	s := Selection{}
	if s.PreservedSubagents != nil {
		t.Errorf("Selection.PreservedSubagents zero value = %v, want nil", s.PreservedSubagents)
	}

	s.PreservedSubagents = []string{"takt-dev"}
	if len(s.PreservedSubagents) != 1 || s.PreservedSubagents[0] != "takt-dev" {
		t.Errorf("Selection.PreservedSubagents = %v, want [takt-dev]", s.PreservedSubagents)
	}
}
