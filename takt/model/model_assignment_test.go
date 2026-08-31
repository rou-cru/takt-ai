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
