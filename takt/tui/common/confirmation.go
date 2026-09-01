// Package common contains shared TUI state that is independent of a lifecycle flow.
package common

// Confirmation represents an explicit pending user decision.
type Confirmation struct {
	Prompt string
}

// AcceptConfirmation and CancelConfirmation are emitted by a flow after it maps
// its input to a confirmation decision.
type AcceptConfirmation struct{}
type CancelConfirmation struct{}

// Decision is the outcome of a confirmation message.
type Decision int

const (
	DecisionNone Decision = iota
	DecisionAccepted
	DecisionCanceled
)

// Update applies a confirmation decision without coupling this package to a
// particular screen or key binding.
func (confirmation Confirmation) Update(message any) Decision {
	switch message.(type) {
	case AcceptConfirmation:
		return DecisionAccepted
	case CancelConfirmation:
		return DecisionCanceled
	default:
		return DecisionNone
	}
}
