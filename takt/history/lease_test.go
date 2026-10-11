package history_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/history"
)

// TestOrphanedFollowsTheOwnersLease: an execution stays with its owner while
// the owner renews its lease, and is orphaned once the lease lapses or when it
// never had an owner.
func TestOrphanedFollowsTheOwnersLease(t *testing.T) {
	h, err := history.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	renewed := time.Now()
	for _, e := range []history.Entry{
		{Author: history.AuthorHarness, Kind: history.KindLease, Owner: "live", CreatedAt: renewed.UnixMilli(), Cause: history.CauseNone},
		{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: "r", WorkUnitID: "a", AttemptID: "1", Dispatch: "owned", Agent: "dev", Caller: "r", Owner: "live", Cause: history.CauseNone},
		{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: "r", WorkUnitID: "b", AttemptID: "1", Dispatch: "ownerless", Agent: "dev", Caller: "r", Cause: history.CauseNone},
		{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: "r", WorkUnitID: "c", AttemptID: "1", Dispatch: "unknown-owner", Agent: "dev", Caller: "r", Owner: "gone", Cause: history.CauseNone},
	} {
		if err := h.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.Append(history.Entry{Author: history.AuthorHarness, Kind: history.KindLease, Cause: history.CauseNone}); err == nil {
		t.Fatal("a lease without its instance was recorded")
	}
	p := h.Project()
	for dispatch, want := range map[string]bool{"owned": false, "ownerless": true, "unknown-owner": true} {
		if got := p.Orphaned(p.Executions[dispatch], renewed); got != want {
			t.Errorf("Orphaned(%s) while the lease is fresh = %v; want %v", dispatch, got, want)
		}
	}
	if !p.Orphaned(p.Executions["owned"], renewed.Add(history.LeaseTTL+time.Second)) {
		t.Error("Orphaned(owned) after the lease lapsed = false; want true")
	}
}
