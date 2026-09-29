package runtime

import (
	"github.com/rou-cru/takt-ai/takt/lifecycle"
)

// Cancelled reports a cancellation that stopped the action before completion.
func (result ActionResult) Cancelled() bool {
	return result.Outcome == lifecycle.OutcomeCancelledPartial || result.Outcome == lifecycle.OutcomeCancelledNothingApplied
}

// Partial reports a cancellation that left changes applied so callers warn honestly.
func (result ActionResult) Partial() bool {
	return result.Outcome == lifecycle.OutcomeCancelledPartial
}
