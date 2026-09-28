package obs_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
)

func TestBusAttachStorePersistsPublishedEvents(t *testing.T) {
	store, err := obs.OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	clock := obs.NewClock()
	bus := obs.NewBus("session-1", clock)
	bus.AttachStore(store)

	env := obs.NewEnvelope(clock, obs.PlaneVFS, "agent-a")
	env.EventClass = obs.EventVFSDelta
	env.Correlations = obs.CorrelationIDs{SessionID: "session-1", WorkUnitID: "wu-1"}
	if err := bus.Publish(env); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	events, err := store.Events("session-1", obs.EventVFSDelta, 0, 0, 0)
	if err != nil {
		t.Fatalf("store.Events() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("store.Events() = %d, want 1 persisted event", len(events))
	}
}

func TestBusDegradeMarksUnavailable(t *testing.T) {
	// Degrade has no exported reader, so this only exercises that it does not
	// panic and that publishing still works afterward (fallback posture).
	bus := obs.NewBus("session-1", obs.NewClock())
	bus.Degrade("control plane unreachable")

	env := obs.NewEnvelope(obs.NewClock(), obs.PlaneVFS, "agent-a")
	env.EventClass = obs.EventVFSDelta
	env.Correlations = obs.CorrelationIDs{SessionID: "session-1"}
	if err := bus.Publish(env); err != nil {
		t.Fatalf("Publish() after Degrade() error = %v, want nil (no store attached, still valid)", err)
	}
}
