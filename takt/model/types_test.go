package model

import "testing"

func TestSetupChoice_ClosedSet(t *testing.T) {
	tests := []struct {
		name string
		got  SetupChoice
		want string
	}{
		{name: "default", got: SetupDefault, want: ""},
		{name: "custom", got: SetupCustom, want: "custom"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("SetupChoice = %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestSetupChoice_ZeroValue(t *testing.T) {
	var choice SetupChoice
	if choice != SetupDefault {
		t.Errorf("zero-value SetupChoice = %q, want %q", choice, SetupDefault)
	}
}
