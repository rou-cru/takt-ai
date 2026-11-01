// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package obs_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// Exercise both public acquisition boundaries: invalid events cannot become
// durable telemetry even when a producer bypasses Bus.Publish.
func TestEveryEnvelopeClassRejectsUnknownAndNestedAttributesBeforePersistence(t *testing.T) {
	store := openStore(t, t.TempDir())
	clock := obs.NewClock()
	bus := obs.NewBus("contract", clock)
	bus.AttachStore(store)
	classes := []struct {
		class obs.EventClass
		key   string
	}{
		{obs.EventVFSDelta, "operation"}, {obs.EventCollision, "path"},
		{obs.EventDispatch, "decision"}, {obs.EventUnitLifecycle, "transition"},
		{obs.EventToolActivity, "tool"}, {obs.EventModelUsage, "model"},
		{obs.EventGCFindingUnapplied, "target"}, {obs.EventProblemRate, "signal"},
		{obs.EventCycleReedit, "cycle_id"}, {obs.EventControlThrottle, "policy_ref"},
		{obs.EventControlContain, "policy_ref"}, {obs.EventControlRollback, "policy_ref"},
		{obs.EventControlGate, "policy_ref"}, {obs.EventControlEscalate, "policy_ref"},
	}
	for _, tt := range classes {
		t.Run(string(tt.class), func(t *testing.T) {
			e := obs.NewEnvelope(clock, obs.PlaneHarness, "actor")
			e.EventClass = tt.class
			e.Correlations.SessionID = "contract"
			for _, attrs := range []map[string]any{
				{"unexpected": "synthetic"},
				{tt.key: map[string]any{"prompt": "synthetic"}},
				{tt.key: []string{"synthetic"}},
				{tt.key: nil},
			} {
				e.Attributes = attrs
				if err := bus.Publish(e); err == nil {
					t.Errorf("bus accepted %+v", attrs)
				}
				if _, err := store.AppendEvent(e); err == nil {
					t.Errorf("store accepted %+v", attrs)
				}
			}
			e.Attributes = map[string]any{tt.key: "synthetic-reference"}
			if err := bus.Publish(e); err != nil {
				t.Fatal(err)
			}
		})
	}
	events, err := store.Events("contract", "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != len(classes) {
		t.Fatalf("persisted %d events, want only %d valid events", len(events), len(classes))
	}
}

func TestEnvelopeRejectsUnknownClassSourceAndVersion(t *testing.T) {
	store := openStore(t, t.TempDir())
	clock := obs.NewClock()
	bus := obs.NewBus("invalid-contract", clock)
	bus.AttachStore(store)
	for _, tc := range []struct {
		name   string
		change func(*obs.Envelope)
	}{
		{"unknown-class", func(e *obs.Envelope) { e.EventClass = "unregistered" }},
		{"empty-class", func(e *obs.Envelope) { e.EventClass = "" }},
		{"unknown-source", func(e *obs.Envelope) { e.Source = "unregistered" }},
		{"empty-source", func(e *obs.Envelope) { e.Source = "" }},
		{"unknown-version", func(e *obs.Envelope) { e.SchemaVersion = "future" }},
		{"empty-version", func(e *obs.Envelope) { e.SchemaVersion = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(clock, obs.PlaneHarness, "actor")
			e.Correlations.SessionID = "invalid-contract"
			tc.change(&e)
			if err := bus.Publish(e); err == nil {
				t.Fatal("bus accepted invalid contract")
			}
			if _, err := store.AppendEvent(e); err == nil {
				t.Fatal("store accepted invalid contract")
			}
		})
	}
	events, err := store.Events("invalid-contract", "", 0, 0, 0)
	if err != nil || len(events) != 0 {
		t.Fatalf("invalid events persisted: %v, %v", events, err)
	}
}
