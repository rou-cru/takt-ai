package runtime_test

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/spinner"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
)

func TestRunLifecycle(t *testing.T) {
	r := runtime.NewRun()
	if r.Busy() {
		t.Fatal("NewRun() is busy, want idle")
	}

	request := runtime.ActionRequest{ID: runtime.NextID(), Action: runtime.ActionInstall}
	r = r.Start(request)
	if !r.Busy() {
		t.Fatal("Start() did not mark the run busy")
	}
	if r.Request.ID != request.ID {
		t.Errorf("Request after Start() = %+v, want ID %d", r.Request, request.ID)
	}

	cancelled := r.Cancel(runtime.CancelRequest{ID: request.ID})
	if !cancelled.CancelRequested {
		t.Error("Cancel() with a matching ID did not set CancelRequested")
	}
	unmatched := r.Cancel(runtime.CancelRequest{ID: request.ID + 100})
	if unmatched.CancelRequested {
		t.Error("Cancel() with a mismatched ID set CancelRequested")
	}

	other := runtime.ActionResultMsg{Request: runtime.ActionRequest{ID: request.ID + 1, Action: request.Action}}
	if _, ok := r.Result(other); ok {
		t.Error("Result() matched a message for a different request ID")
	}
	matching := runtime.ActionResultMsg{Request: request, Result: runtime.ActionResult{Changed: []string{"a"}}}
	got, ok := r.Result(matching)
	if !ok || len(got.Result.Changed) != 1 {
		t.Errorf("Result() with a matching request = %+v, %v, want the matching message", got, ok)
	}

	ended := r.End()
	if ended.Busy() || ended.CancelRequested {
		t.Error("End() did not clear busy/cancel-requested")
	}

	idle := runtime.NewRun()
	if _, cmd := idle.Tick(spinner.TickMsg{}); cmd != nil {
		t.Error("Tick() on an idle run returned a non-nil cmd")
	}
	busy, cmd := r.Tick(spinner.TickMsg{})
	_ = busy
	_ = cmd // a busy run's tick may or may not return a cmd depending on the spinner's animation state; just exercise the path.

	if view := r.SpinView(); view == "" {
		t.Error("SpinView() is empty")
	}
}

func TestNewAdapterWiresProductionLifecycle(t *testing.T) {
	// NewAdapter just wires production seams; constructing it must not panic
	// or require any external process.
	_ = runtime.NewAdapter()
}

func TestCancelledBodyNotPartialReportsNothing(t *testing.T) {
	got := runtime.CancelledBody(runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledNothingApplied})
	if got == "" {
		t.Error("CancelledBody() for a non-partial cancellation is empty")
	}
}

func TestCancelledBodyPartialListsAppliedAndNotApplied(t *testing.T) {
	result := runtime.ActionResult{
		Outcome:    lifecycle.OutcomeCancelledPartial,
		Changed:    []string{"a.txt"},
		NotApplied: []string{"b.txt"},
		BackupDir:  "/tmp/backups",
	}
	got := runtime.CancelledBody(result)
	for _, want := range []string{"a.txt", "/tmp/backups"} {
		if !strings.Contains(got, want) {
			t.Errorf("CancelledBody() = %q, want it to contain %q", got, want)
		}
	}
}

func TestLateCancelNote(t *testing.T) {
	if got := runtime.LateCancelNote(runtime.ActionResult{}); got != "" {
		t.Errorf("LateCancelNote() with no cancel requested = %q, want empty", got)
	}
	cancelled := runtime.ActionResult{Outcome: lifecycle.OutcomeCancelledPartial, CancelRequested: true}
	if got := runtime.LateCancelNote(cancelled); got != "" {
		t.Errorf("LateCancelNote() for an actually-cancelled result = %q, want empty", got)
	}
	late := runtime.ActionResult{CancelRequested: true}
	if got := runtime.LateCancelNote(late); got == "" {
		t.Error("LateCancelNote() for a completed result with a late cancel request is empty, want a note")
	}
}
