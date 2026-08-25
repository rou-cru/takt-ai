package model

import "testing"

// validOrchestratorProjection returns a projection with every field populated.
func validOrchestratorProjection() OrchestratorProjection {
	return OrchestratorProjection{
		Platform:   "claude-code",
		Delegate:   "delegate",
		Wait:       "wait",
		Close:      "close",
		Question:   "question",
		Background: "background",
		Models:     "models",
		Effort:     "effort",
		Isolation:  "isolation",
		SkillRoot:  "skill-root",
	}
}

// TestOrchestratorProjectionValidate_AllFieldsSet verifies that a fully
// populated projection validates without error.
func TestOrchestratorProjectionValidate_AllFieldsSet(t *testing.T) {
	p := validOrchestratorProjection()
	if err := p.Validate(); err != nil {
		t.Errorf("Validate() = %v, want nil", err)
	}
}

// TestOrchestratorProjectionValidate_ZeroValue verifies that the zero value
// (all fields empty) is invalid.
func TestOrchestratorProjectionValidate_ZeroValue(t *testing.T) {
	var p OrchestratorProjection
	if err := p.Validate(); err == nil {
		t.Error("Validate() = nil, want error for zero-value projection")
	}
}

// TestOrchestratorProjectionValidate_MissingField verifies that Validate
// reports an error when any single field is left blank, one at a time.
func TestOrchestratorProjectionValidate_MissingField(t *testing.T) {
	tests := []struct {
		name  string
		blank func(p *OrchestratorProjection)
	}{
		{"Platform", func(p *OrchestratorProjection) { p.Platform = "" }},
		{"Delegate", func(p *OrchestratorProjection) { p.Delegate = "" }},
		{"Wait", func(p *OrchestratorProjection) { p.Wait = "" }},
		{"Close", func(p *OrchestratorProjection) { p.Close = "" }},
		{"Question", func(p *OrchestratorProjection) { p.Question = "" }},
		{"Background", func(p *OrchestratorProjection) { p.Background = "" }},
		{"Models", func(p *OrchestratorProjection) { p.Models = "" }},
		{"Effort", func(p *OrchestratorProjection) { p.Effort = "" }},
		{"Isolation", func(p *OrchestratorProjection) { p.Isolation = "" }},
		{"SkillRoot", func(p *OrchestratorProjection) { p.SkillRoot = "" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := validOrchestratorProjection()
			tc.blank(&p)
			if err := p.Validate(); err == nil {
				t.Errorf("Validate() = nil, want error when %s is blank", tc.name)
			}
		})
	}
}

// TestOrchestratorProjectionValidate_ErrorMessage verifies the error message
// text so regressions to the message are caught.
func TestOrchestratorProjectionValidate_ErrorMessage(t *testing.T) {
	var p OrchestratorProjection
	err := p.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want error")
	}
	want := "invalid orchestrator projection"
	if err.Error() != want {
		t.Errorf("Validate() error = %q, want %q", err.Error(), want)
	}
}
