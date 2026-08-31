package model

import "testing"

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
