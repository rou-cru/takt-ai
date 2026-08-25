package model

import "testing"

// TestModelAssignment_EffortZeroValue verifies that a ModelAssignment constructed
// without setting Effort has an empty string as its zero value.
func TestModelAssignment_EffortZeroValue(t *testing.T) {
	a := ModelAssignment{Model: "model-a"}
	if a.Effort != "" {
		t.Errorf("ModelAssignment.Effort zero value = %q, want %q", a.Effort, "")
	}
}

// TestModelAssignment_ZeroValue verifies that a zero-value ModelAssignment
// has empty Model and Effort fields.
func TestModelAssignment_ZeroValue(t *testing.T) {
	var a ModelAssignment
	if a.Model != "" || a.Effort != "" {
		t.Errorf("zero-value ModelAssignment = %#v, want empty Model and Effort", a)
	}
}

// TestModelAssignment_Equality verifies that ModelAssignment supports value
// equality via ==, which callers rely on (e.g. selection_test.go, preset
// comparisons).
func TestModelAssignment_Equality(t *testing.T) {
	a := ModelAssignment{Model: "model-a", Effort: "high"}
	b := ModelAssignment{Model: "model-a", Effort: "high"}
	c := ModelAssignment{Model: "model-a", Effort: "low"}
	if a != b {
		t.Errorf("%#v != %#v, want equal", a, b)
	}
	if a == c {
		t.Errorf("%#v == %#v, want different", a, c)
	}
}

func TestResolveSubAgentAssignment_Precedence(t *testing.T) {
	tests := []struct {
		name      string
		subAgent  string
		overrides map[string]ModelAssignment
		preset    map[string]ModelAssignment
		want      ModelAssignment
	}{
		{
			name:     "specialist override wins",
			subAgent: "specialist",
			overrides: map[string]ModelAssignment{
				"specialist": {Model: "override"},
				"default":    {Model: "explicit-default"},
			},
			preset: map[string]ModelAssignment{
				"specialist": {Model: "preset-specialist"},
				"default":    {Model: "preset-default"},
			},
			want: ModelAssignment{Model: "override"},
		},
		{
			name:     "specialist preset beats default override",
			subAgent: "specialist",
			overrides: map[string]ModelAssignment{
				"default": {Model: "explicit-default"},
			},
			preset: map[string]ModelAssignment{
				"specialist": {Model: "preset-specialist"},
				"default":    {Model: "preset-default"},
			},
			want: ModelAssignment{Model: "preset-specialist"},
		},
		{
			name:     "default override handles unknown specialist",
			subAgent: "unknown",
			overrides: map[string]ModelAssignment{
				"default": {Model: "explicit-default"},
			},
			preset: map[string]ModelAssignment{
				"default": {Model: "preset-default"},
			},
			want: ModelAssignment{Model: "explicit-default"},
		},
		{
			name:     "preset default handles unknown specialist",
			subAgent: "unknown",
			preset: map[string]ModelAssignment{
				"default": {Model: "preset-default"},
			},
			want: ModelAssignment{Model: "preset-default"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveSubAgentAssignment("agent", tc.subAgent, "default", tc.overrides, tc.preset)
			if err != nil {
				t.Fatalf("ResolveSubAgentAssignment() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("ResolveSubAgentAssignment() = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestResolveSubAgentAssignment_RequiresModel(t *testing.T) {
	_, err := ResolveSubAgentAssignment("codex", "specialist", "default", map[string]ModelAssignment{
		"specialist": {},
	}, nil)
	if got, want := err.Error(), `codex sub-agent "specialist" has no model assignment`; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}
