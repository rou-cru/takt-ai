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

// TestModelAssignment_FullIDUnaffectedByEffort verifies that FullID() is not
// changed by the presence of an Effort value.
func TestModelAssignment_FullIDUnaffectedByEffort(t *testing.T) {
	a := ModelAssignment{Model: "model-a", Effort: "high"}
	want := "model-a"
	if a.FullID() != want {
		t.Errorf("FullID() = %q, want %q", a.FullID(), want)
	}
}
