package history_test

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/history"
)

// TestDeliveredIsWhatTheProducerDeliveredWithoutABlocker: a consumer receives
// only the entries a producer delivered without a blocker mark, and nothing
// from a producer whose latest delivery is a blocker or that delivered nothing.
func TestDeliveredIsWhatTheProducerDeliveredWithoutABlocker(t *testing.T) {
	h, err := history.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	deliveries := map[string][]struct {
		ids     []int64
		blocker bool
	}{
		"unblocked": {{[]int64{101}, true}, {[]int64{202}, false}},
		"result":    {{[]int64{303}, false}, {[]int64{304}, false}},
		"blocked":   {{[]int64{404}, false}, {[]int64{405}, true}},
		"silent":    nil,
	}
	for unit, events := range deliveries {
		if err := h.Append(history.Entry{Author: history.AuthorHarness, Kind: history.KindExecution, SessionID: "r", WorkUnitID: unit, AttemptID: "1", Dispatch: unit, Agent: "dev", Caller: "r", Cause: history.CauseNone}); err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if err := h.Deliver(h.Project().Executions[unit], e.ids, e.blocker); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := h.Deliver(history.Execution{Dispatch: "absent"}, []int64{1}, false); err == nil {
		t.Fatal("a delivery for an execution that does not run was recorded")
	}
	p := h.Project()
	for unit, want := range map[string][]int64{"unblocked": {202}, "result": {303, 304}, "blocked": nil, "silent": nil} {
		got, ok := p.Delivered("r", unit)
		if ok != (want != nil) || !slices.Equal(got, want) {
			t.Errorf("Delivered(%s) = %v, %v; want %v", unit, got, ok, want)
		}
	}
}
