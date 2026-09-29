package ui_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

func TestTargetLabel(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"opencode agent maps to display name", model.AgentOpenCode, ui.OpenCodeLabel},
		{"other identifiers echo back unchanged", "skills", "skills"},
		{"empty id echoes back unchanged", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ui.TargetLabel(tc.id); got != tc.want {
				t.Errorf("TargetLabel(%q) = %q, want %q", tc.id, got, tc.want)
			}
		})
	}
}
