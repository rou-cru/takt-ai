package runtime

import (
	tea "charm.land/bubbletea/v2"
	"github.com/rou-cru/takt-ai/takt/tui/ui"
)

// Run is one screen's action state: the pending request, its spinner, and the cancel flag.
type Run struct {
	// Request is the in-flight action; screens read it back after End, e.g.
	// to name the affected targets in the result view.
	Request ActionRequest
	// CancelRequested marks a cancellation asked while the action runs.
	CancelRequested bool
	spin            ui.Spinner
	progress        ui.Progress
	frames          int
	busy            bool
}

// NewRun returns a Run ready to start an action.
func NewRun() Run { return Run{spin: ui.NewSpinner()} }

// Runner is a flow with an in-flight action.
type Runner interface {
	// Run exposes the flow's action state.
	Run() Run
}

// Start marks request as running until its result arrives.
func (r Run) Start(request ActionRequest) Run {
	r.Request = request
	r.busy = true
	r.CancelRequested = false
	r.progress = ui.Progress{}
	r.frames = 0
	return r
}

// Cancel records a cancellation for the running request; repeated presses add nothing.
func (r Run) Cancel(msg CancelRequest) Run {
	if r.busy && msg.ID == r.Request.ID {
		r.CancelRequested = true
	}
	return r
}

// Tick advances the spinner while an action runs; otherwise it is a no-op.
func (r Run) Tick(msg tea.Msg) (Run, tea.Cmd) {
	if !r.busy {
		return r, nil
	}
	var cmd tea.Cmd
	r.spin, cmd = r.spin.Update(msg)
	r.frames++
	r.progress.Frame = r.frames
	return r, cmd
}

// AcceptProgress applies one progress event only when it belongs to this run.
func (r Run) AcceptProgress(msg ActionProgressMsg) (Run, bool) {
	if !r.busy || msg.Request.ID != r.Request.ID || msg.Request.Action != r.Request.Action {
		return r, false
	}
	event := msg.Progress
	r.progress.Message = event.Message
	if event.Stage == "applied" {
		r.progress.Completed = event.Completed
		r.progress.Total = event.Total
	} else {
		r.progress.Completed = 0
		r.progress.Total = 0
	}
	if event.Stage == "applied" && event.Path != "" {
		if len(r.progress.Applied) == 0 || r.progress.Applied[len(r.progress.Applied)-1] != event.Path {
			r.progress.Applied = append(r.progress.Applied, event.Path)
		}
		r.progress.Current = ""
	} else if event.Path != "" {
		r.progress.Current = event.Path
	} else {
		r.progress.Current = ""
	}
	return r, true
}

// Result reports msg when it answers the running request.
func (r Run) Result(msg ActionResultMsg) (ActionResultMsg, bool) {
	if !r.busy || msg.Request.ID != r.Request.ID || msg.Request.Action != r.Request.Action {
		return ActionResultMsg{}, false
	}
	return msg, true
}

// End clears the running flags once the result was consumed.
func (r Run) End() Run {
	r.busy = false
	r.CancelRequested = false
	return r
}

// Busy reports that an action is running.
func (r Run) Busy() bool { return r.busy }

// SpinView renders the current spinner frame.
func (r Run) SpinView() string { return r.spin.View() }

// ProgressView returns the current progress snapshot for a busy screen.
func (r Run) ProgressView() ui.Progress { return r.progress }
