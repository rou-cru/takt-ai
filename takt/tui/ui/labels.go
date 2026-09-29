package ui

import (
	"github.com/rou-cru/takt-ai/takt/model"
)

// OpenCodeLabel is the harness display name; prose keeps the raw identifier.
const OpenCodeLabel = "OpenCode"

// TargetLabel returns the display name for a plan target, which is the harness
// for its own plan and a bare identifier such as "skills" for the others.
func TargetLabel(id string) string {
	if id == model.AgentOpenCode {
		return OpenCodeLabel
	}
	return id
}
