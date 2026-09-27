// Package testutil provides shared test helpers for setup and CLI test suites.
package testutil

import "github.com/rou-cru/takt-ai/takt/setup"

// TestPlanRequest builds an OpenCode setup.PlanRequest. It is shared between
// takt/setup and cmd/takt-ai test suites to avoid duplicating the same builder
// in two packages.
func TestPlanRequest() setup.PlanRequest {
	return setup.PlanRequest{
		OpenCode: setup.OpenCodePlanOptions{
			GlobalPrompt: "Use the explicit OpenCode test prompt.",
		},
	}
}
