package ui

import (
	"cmp"
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"

	progressbar "charm.land/bubbles/v2/progress"

	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

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
	style := theme.StatusInfo
	switch state {
	case StateSuccess:
		style = theme.StatusSuccess
	case StatePartial, StateWarning:
		style = theme.StatusWarning
	case StateFailed:
		style = theme.StatusDanger
	}
	return style.Render(string(state)+":") + " " + theme.Label.Render(message)
}

// Progress is a running operation's phases: the ones already finished, the
// current one with its real counts, and the file it is on.
type Progress struct {
	Done      []string
	Message   string
	Current   string
	Completed int
	Total     int
	Frame     int
}

// maxVisibleDone bounds the finished phases shown, so the current phase stays
// on screen at the smallest supported size.
const maxVisibleDone = 6

// Busy shows the finished phases, the current one with its marker and, when
// its total is known, a real progress bar and the file it is on. operation
// names the work until the first phase is reported.
func Busy(operation string, marker string, progress Progress) string {
	var body strings.Builder
	done := progress.Done
	if hidden := len(done) - maxVisibleDone; hidden > 0 {
		body.WriteString(theme.Caption.Render(fmt.Sprintf(TextEarlierPhasesFmt, hidden)) + "\n")
		done = done[hidden:]
	}
	for _, phase := range done {
		body.WriteString(theme.SuccessText.Render(theme.Icon.Done) + " " + theme.Secondary.Render(phase) + "\n")
	}
	current := cmp.Or(progress.Message, operation)
	body.WriteString(theme.Focus.Render(marker) + " " + theme.Label.Render(current))
	indent := strings.Repeat(" ", lipgloss.Width(marker)+1)
	if bar := progressBar(progress); bar != "" {
		body.WriteString("\n" + indent + bar)
	}
	if progress.Current != "" {
		body.WriteString("\n" + indent + theme.Caption.Render(progress.Current))
	}
	return body.String()
}

// BusyFooter offers cancellation while an operation runs; once requested it
// stays visible but unavailable, saying the current phase must finish.
func BusyFooter(cancelRequested bool) string {
	action := FooterAction{Label: TextActionCancel}
	if cancelRequested {
		action.Unavailable = TextCancelRequested
	}
	return FooterActions([]FooterAction{action}, 0, true)
}

// progressBarWidth fits the bar and its percentage inside the narrowest
// panel content width (60 columns minus margins, border and padding).
const progressBarWidth = 40

// progressBar renders real progress only: without a known total the spinner
// marker carries activity, so no bar pretends to measure anything.
func progressBar(progress Progress) string {
	if progress.Total <= 0 {
		return ""
	}
	bar := progressbar.New(
		progressbar.WithWidth(progressBarWidth),
		progressbar.WithColors(theme.FocusRing),
		progressbar.WithFillCharacters('█', '░'),
	)
	bar.EmptyColor = theme.BorderSubtle
	bar.PercentageStyle = theme.Caption
	return bar.ViewAs(float64(min(progress.Completed, progress.Total)) / float64(progress.Total))
}
