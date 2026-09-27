// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package obs_test

import (
	"errors"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// ─── helpers ─────────────────────────────────────────────────────────────────

func newBus(t *testing.T) (*obs.Bus, *obs.Clock) {
	t.Helper()
	clock := obs.NewClock()
	bus := obs.NewBus("session-test", clock)
	return bus, clock
}

func newEnv(clock *obs.Clock, source obs.SourcePlane, agent string) obs.Envelope {
	env := obs.NewEnvelope(clock, source, agent)
	env.EventClass = obs.EventVFSDelta
	env.Correlations = obs.CorrelationIDs{SessionID: "session-test", WorkUnitID: "wu-1"}
	return env
}

// ─── Envelope normalization ──────────────────────────────────────────────────

func TestNewEnvelope_SchemaPinned(t *testing.T) {
	// Schema version must be pinned.
	clock := obs.NewClock()
	env := obs.NewEnvelope(clock, obs.PlaneVFS, "agent-a")
	if env.SchemaVersion != obs.SchemaVersion {
		t.Errorf("SchemaVersion = %q; want %q", env.SchemaVersion, obs.SchemaVersion)
	}
}

func TestNewEnvelope_TimestampsSet(t *testing.T) {
	// Session and wall timestamps must be set.
	before := time.Now()
	clock := obs.NewClock()
	env := obs.NewEnvelope(clock, obs.PlaneVFS, "agent-a")
	if env.WallTimestamp.Before(before) {
		t.Errorf("WallTimestamp %v is before test start %v", env.WallTimestamp, before)
	}
	// Session timestamp must be a non-negative elapsed duration.
	if env.SessionTimestamp < 0 {
		t.Errorf("SessionTimestamp is negative: %v", env.SessionTimestamp)
	}
}

func TestEnvelope_Validate_RequiresActingAgent(t *testing.T) {
	// Every event must attribute the acting agent.
	clock := obs.NewClock()
	env := obs.NewEnvelope(clock, obs.PlaneVFS, "")
	if err := env.Validate(); err == nil {
		t.Error("Validate() with empty ActingAgent = nil; want error")
	}
}

func TestEnvelope_Validate_ForbiddenAttributes(t *testing.T) {
	// The envelope must not carry file contents, prompts, or secrets.
	clock := obs.NewClock()
	forbidden := []string{"content", "file_content", "prompt", "response", "secret", "credential", "token", "password", "api_key"}
	for _, key := range forbidden {
		env := obs.NewEnvelope(clock, obs.PlaneVFS, "agent")
		env.EventClass = obs.EventVFSDelta
		env.Attributes = map[string]any{key: "some-value"}
		if err := env.Validate(); !errors.Is(err, obs.ErrContentForbidden) {
			t.Errorf("Validate() with attribute %q = %v; want ErrContentForbidden", key, err)
		}
	}
}

func TestEnvelope_Validate_SafeAttributes(t *testing.T) {
	// Safe attributes (path, hash, count) must pass validation.
	clock := obs.NewClock()
	env := obs.NewEnvelope(clock, obs.PlaneVFS, "agent")
	env.EventClass = obs.EventVFSDelta
	env.Attributes = map[string]any{
		"file_path":  "src/main.go",
		"after_hash": "abc123",
		"op_count":   42,
	}
	if err := env.Validate(); err != nil {
		t.Errorf("Validate() with safe attributes = %v; want nil", err)
	}
}

// ─── Internal Bus ────────────────────────────────────────────────────────────

func TestBus_Publish_AcceptsValidEnvelopes(t *testing.T) {
	// Publishing valid envelopes must succeed synchronously: the bus operates
	// locally with zero network egress.
	bus, clock := newBus(t)
	done := make(chan struct{})
	go func() {
		for i := range 5 {
			env := newEnv(clock, obs.PlaneVFS, "agent-a")
			env.Attributes = map[string]any{"seq": i}
			if err := bus.Publish(env); err != nil {
				t.Errorf("Publish: %v", err)
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("bus operations blocked — expected synchronous completion")
	}
}

func TestBus_Publish_RejectsInvalidEnvelope(t *testing.T) {
	// Bus must reject envelopes that fail governance validation.
	bus, clock := newBus(t)
	env := obs.NewEnvelope(clock, obs.PlaneVFS, "agent")
	env.EventClass = obs.EventVFSDelta
	env.Attributes = map[string]any{"prompt": "forbidden"}
	if err := bus.Publish(env); !errors.Is(err, obs.ErrContentForbidden) {
		t.Errorf("Publish forbidden envelope = %v; want ErrContentForbidden", err)
	}
}

// ─── Session Clock ───────────────────────────────────────────────────────────

func TestClock_NowIsNonDecreasing(t *testing.T) {
	clock := obs.NewClock()
	t1 := clock.Now()
	time.Sleep(1 * time.Millisecond)
	t2 := clock.Now()
	if t2 < t1 {
		t.Errorf("clock went backwards: %v < %v", t2, t1)
	}
}

func TestClock_ElapsedPositive(t *testing.T) {
	clock := obs.NewClock()
	time.Sleep(1 * time.Millisecond)
	if clock.Now() <= 0 {
		t.Error("Clock.Now() should be positive after sleep")
	}
}

// ─── Convenience helpers ──────────────────────────────────────────────────────

func TestPublishVFSEvent_Succeeds(t *testing.T) {
	// A VFS envelope must publish cleanly with its journal reference and no
	// duplicated content.
	bus, clock := newBus(t)
	if err := obs.PublishVFSEvent(bus, clock, "agent-a", "journal-ref-xyz", "wu-1", map[string]any{"operation": "create"}); err != nil {
		t.Fatalf("PublishVFSEvent: %v", err)
	}
}

func TestPublishCollisionEvent_Succeeds(t *testing.T) {
	// A collision envelope must publish cleanly without file content.
	bus, clock := newBus(t)
	if err := obs.PublishCollisionEvent(bus, clock, "agent-b", "agent-a", "agents/x.md", "wu-1"); err != nil {
		t.Fatalf("PublishCollisionEvent: %v", err)
	}
}

func TestPublishCycleReeditEvent_Succeeds(t *testing.T) {
	// A reedit envelope must publish cleanly and carry the cycle attribution
	// PR-MNT-31 requires, without any file content.
	bus, clock := newBus(t)
	if err := obs.PublishCycleReeditEvent(bus, clock, "agent-a", "cycle-1", "dead-code", "hash-xyz", "wu-1"); err != nil {
		t.Fatalf("PublishCycleReeditEvent: %v", err)
	}
}

// TestControlActionTaxonomyIsComplete checks PR-OBS-CTL-1's full ordering
// (OBSERVE, THROTTLE, CONTAIN, ROLLBACK, GATE, ESCALATE) validates as a
// ControlRecord, and that PublishControlEffectEvent produces the typed event
// PR-OBS-CTL-5 requires for every action beyond OBSERVE.
func TestControlActionTaxonomyIsComplete(t *testing.T) {
	for _, class := range []obs.ActionClass{obs.ActionObserve, obs.ActionThrottle, obs.ActionContain,
		obs.ActionRollback, obs.ActionGate, obs.ActionEscalate} {
		r := obs.ControlRecord{ActionClass: class, TriggeringCondition: "cond", PolicyRef: "policy.ref", ActingAgent: "agent-a"}
		if err := r.Validate(); err != nil {
			t.Errorf("ControlRecord{ActionClass: %s}.Validate() = %v, want nil", class, err)
		}
	}

	bus, clock := newBus(t)
	for _, class := range []obs.ActionClass{obs.ActionThrottle, obs.ActionContain, obs.ActionRollback,
		obs.ActionGate, obs.ActionEscalate} {
		if err := obs.PublishControlEffectEvent(bus, clock, class, "agent-a", "policy.ref", "cond", "wu-1"); err != nil {
			t.Errorf("PublishControlEffectEvent(%s) = %v, want nil", class, err)
		}
	}
	if err := obs.PublishControlEffectEvent(bus, clock, obs.ActionObserve, "agent-a", "policy.ref", "cond", "wu-1"); err == nil {
		t.Error("PublishControlEffectEvent(OBSERVE) = nil, want an error: OBSERVE has no effect event")
	}
}
