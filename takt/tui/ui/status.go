package ui

import (
	"fmt"
	"strings"

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

// Progress describes the latest operation phase and its committed artifact list.
type Progress struct {
	Message   string
	Current   string
	Completed int
	Total     int
	Applied   []string
	Frame     int
}

// Busy shows a running operation with a step marker and cancellation path.
func Busy(operation string, cancelRequested bool, marker string, updates ...Progress) string {
	body := theme.Title.Render(marker) + " " + Status(StatePending, operation+TextInProgressSuffix)
	progress := Progress{}
	if len(updates) > 0 {
		progress = updates[0]
	}
	if progress.Message != "" {
		body += "\n\n" + theme.Label.Render(progress.Message)
	}
	body += "\n\n" + progressBar(progress)
	if progress.Current != "" {
		body += "\n" + theme.Caption.Render(progress.Current)
	}
	if len(progress.Applied) > 0 {
		body += "\n\n" + theme.Title.Render("Applied")
		const maxVisibleApplied = 6
		start := max(0, len(progress.Applied)-maxVisibleApplied)
		if start > 0 {
			body += "\n  … and " + theme.Label.Render(fmt.Sprint(start)) + " earlier"
		}
		for _, path := range progress.Applied[start:] {
			body += "\n  ✓ " + theme.Label.Render(path)
		}
	}
	if cancelRequested {
		return body + "\n\n" + theme.Label.Foreground(theme.WarningFg).Render(TextCancelRequested)
	}
	return body + "\n\n" + theme.Caption.Render(TextCancelHint)
}

const progressTrackWidth = 16
const progressBlockWidth = 4

func progressBar(progress Progress) string {
	filled := 0
	if progress.Total > 0 {
		filled = min(progressTrackWidth, progress.Completed*progressTrackWidth/progress.Total)
	} else if theme.Animation() {
		position := progress.Frame % (progressTrackWidth - progressBlockWidth + 1)
		return "[" + strings.Repeat("░", position) + strings.Repeat("█", progressBlockWidth) + strings.Repeat("░", progressTrackWidth-position-progressBlockWidth) + "]"
	} else {
		filled = progressBlockWidth
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", progressTrackWidth-filled) + "]"
}
