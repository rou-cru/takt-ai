package model

import "testing"

func TestCanonicalSubAgentCatalog(t *testing.T) {
	catalog := CanonicalSubAgentCatalog()
	if len(catalog) != 13 {
		t.Fatalf("catalog length = %d, want 13", len(catalog))
	}
	for _, subAgent := range catalog {
		if subAgent.Name == "" || subAgent.Persona != OfficialPersona {
			t.Errorf("invalid canonical sub-agent: %#v", subAgent)
		}
		if _, ok := subAgent.Assignments[AgentClaudeCode]; !ok {
			t.Errorf("%q has no Claude assignment", subAgent.Name)
		}
		if _, ok := subAgent.Assignments[AgentCodex]; !ok {
			t.Errorf("%q has no Codex assignment", subAgent.Name)
		}
	}
}

func TestTargetPresetsShareCanonicalSubAgents(t *testing.T) {
	claude := ClaudeDefaultPreset()
	codex := CodexDefaultPreset()
	for _, subAgent := range CanonicalSubAgents() {
		if _, ok := claude[subAgent]; !ok {
			t.Errorf("Claude preset missing %q", subAgent)
		}
		if _, ok := codex[subAgent]; !ok {
			t.Errorf("Codex preset missing %q", subAgent)
		}
	}
	if len(claude) != len(codex) || len(claude) != len(CanonicalSubAgents()) {
		t.Fatalf("preset sizes differ: Claude=%d Codex=%d catalog=%d", len(claude), len(codex), len(CanonicalSubAgents()))
	}
}

// TestSetupChoice_ClosedSet checks setup values.
func TestSetupChoice_ClosedSet(t *testing.T) {
	choices := []struct {
		name string
		got  SetupChoice
		want string
	}{
		{"SetupDefault", SetupDefault, ""},
		{"SetupCustom", SetupCustom, "custom"},
	}
	if len(choices) != 2 {
		t.Fatalf("expected 2 SetupChoice constants, got %d", len(choices))
	}
	for _, tc := range choices {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
			}
		})
	}
}

func TestSetupChoice_ZeroValue(t *testing.T) {
	var s SetupChoice
	// The zero value is SetupDefault.
	if s != SetupDefault {
		t.Errorf("zero-value SetupChoice = %q, want %q (SetupDefault)", s, SetupDefault)
	}
}

// TestCanonicalSubAgents_StableOrder verifies that CanonicalSubAgents
// returns names in the same, fixed order as CanonicalSubAgentCatalog, since
// callers (e.g. table rendering) depend on deterministic ordering.
func TestCanonicalSubAgents_StableOrder(t *testing.T) {
	want := []string{
		SubAgentTaktInit, SubAgentTaktAnalyst, SubAgentTaktPM, SubAgentTaktSpec,
		SubAgentTaktArchitect, SubAgentTaktProductDesigner, SubAgentTaktTPM,
		SubAgentTaktDev, SubAgentTaktVerify, SubAgentTaktJudgeA, SubAgentTaktJudgeB,
		SubAgentTaktFix, SubAgentDefault,
	}
	got := CanonicalSubAgents()
	if len(got) != len(want) {
		t.Fatalf("CanonicalSubAgents() len = %d, want %d", len(got), len(want))
	}
	for i, name := range want {
		if got[i] != name {
			t.Errorf("CanonicalSubAgents()[%d] = %q, want %q", i, got[i], name)
		}
	}
}

// TestCanonicalSubAgents_MatchesCatalogNames verifies that the names
// returned line up positionally with CanonicalSubAgentCatalog entries.
func TestCanonicalSubAgents_MatchesCatalogNames(t *testing.T) {
	names := CanonicalSubAgents()
	catalog := CanonicalSubAgentCatalog()
	if len(names) != len(catalog) {
		t.Fatalf("names len = %d, catalog len = %d", len(names), len(catalog))
	}
	for i, subAgent := range catalog {
		if names[i] != subAgent.Name {
			t.Errorf("names[%d] = %q, want %q", i, names[i], subAgent.Name)
		}
	}
}

// TestCanonicalSubAgentCatalog_NoDuplicateNames verifies that every
// canonical sub-agent name in the catalog is unique.
func TestCanonicalSubAgentCatalog_NoDuplicateNames(t *testing.T) {
	seen := make(map[string]bool)
	for _, subAgent := range CanonicalSubAgentCatalog() {
		if seen[subAgent.Name] {
			t.Errorf("duplicate canonical sub-agent name: %q", subAgent.Name)
		}
		seen[subAgent.Name] = true
	}
}
