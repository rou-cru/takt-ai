package ui

import "github.com/rou-cru/takt-ai/takt/tui/theme"

// State names a result in words.
type State string

// Shared result and operation states.
const (
	// StateSuccess marks a completed operation.
	StateSuccess State = "Success"
	// StatePartial marks a cancellation with changes already applied.
	StatePartial State = "Partial"
	// StateFailed marks a failed operation.
	StateFailed State = "Failed"
	// StateWarning marks a condition needing attention.
	StateWarning State = "Warning"
	// StatePending marks an operation still running.
	StatePending State = "Pending"
)

// Status prefixes a message with its state.
func Status(state State, message string) string {
	color := theme.TextPrimary
	switch state {
	case StateSuccess:
		color = theme.SuccessFg
	case StatePartial, StateWarning:
		color = theme.WarningFg
	case StateFailed:
		color = theme.DangerFg
	}
	// theme.Title carries the weight, so mono emits no styling at all.
	return theme.Title.Foreground(color).Render(string(state)+":") + " " + theme.Label.Render(message)
}

// indeterminateBar is the fixed 20-column indeterminate progress bar.
const indeterminateBar = "[████░░░░░░░░░░░░░░]"

// Busy shows a running operation with a step marker and cancellation path.
func Busy(operation string, cancelRequested bool, marker string) string {
	body := theme.Title.Render(marker) + " " + Status(StatePending, operation+TextInProgressSuffix)
	body += "\n\n" + indeterminateBar
	if cancelRequested {
		return body + "\n\n" + theme.Label.Foreground(theme.WarningFg).Render(TextCancelRequested)
	}
	return body + "\n\n" + theme.Caption.Render(TextCancelHint)
}
