package setup_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestDriftLabel(t *testing.T) {
	tests := []struct {
		reason          string
		wantLabel       string
		wantSeverity    string
		wantExplanation string
	}{
		{"missing", "missing/corrupt", "danger", "expected on disk but not found"},
		{"takt-additions", "pre-existing", "warning", "still contains settings Takt added"},
		{"user-edited", "modified", "warning", "content no longer matches what Takt deployed"},
		{"something-unknown", "pre-existing", "warning", "unmanaged file Takt would otherwise overwrite"},
		{"", "pre-existing", "warning", "unmanaged file Takt would otherwise overwrite"},
	}
	for _, tc := range tests {
		t.Run(tc.reason, func(t *testing.T) {
			label, severity, explanation := setup.DriftLabel(tc.reason)
			if label != tc.wantLabel || severity != tc.wantSeverity || explanation != tc.wantExplanation {
				t.Errorf("DriftLabel(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tc.reason, label, severity, explanation, tc.wantLabel, tc.wantSeverity, tc.wantExplanation)
			}
		})
	}
}
