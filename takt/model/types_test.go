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
