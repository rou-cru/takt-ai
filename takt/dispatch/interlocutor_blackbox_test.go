package dispatch_test

import (
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
)

// interlocutorTestAgent is a real catalog agent whose role resolves to
// model.RoleDirectInterlocutor (IR-1 eligible), matching the white-box
// interlocutor_test.go fixture.
const interlocutorTestAgent = "pm"

func TestBuildHandoffEnvelopeAssemblesResultShape(t *testing.T) {
	h := newHistory(t)
	memoryRoot := t.TempDir()
	if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, "artifact.md"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}

	// result != "Standard" and no resultIDs skips memory validation entirely,
	// so this exercises BuildHandoffEnvelope's envelope assembly without
	// needing a seeded memory session index.
	outcome := dispatch.HandoffOutcome{Result: "EarlyHandoff", AdditionalContext: "context notes", ExtraArtifacts: []string{"extra.md"}}
	envelope, err := dispatch.BuildHandoffEnvelope(h, "", memoryRoot, "root-1", "child-1", interlocutorTestAgent, outcome)
	if err != nil {
		t.Fatalf("BuildHandoffEnvelope() error = %v", err)
	}
	if envelope["result"] != "EarlyHandoff" || envelope["additional_context"] != "context notes" {
		t.Errorf("BuildHandoffEnvelope() = %+v, want result=EarlyHandoff and the additional context", envelope)
	}
	if h.Project().Budgets("root-1").InterlocutorHolder != "" {
		t.Error("BuildHandoffEnvelope() did not clear the interlocutor holder")
	}
}

func TestBuildHandoffEnvelopePropagatesHandoffDenial(t *testing.T) {
	h := newHistory(t)
	memoryRoot := t.TempDir()
	if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, "artifact.md"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if _, err := dispatch.BuildHandoffEnvelope(h, "", memoryRoot, "root-1", "someone-else", interlocutorTestAgent, dispatch.HandoffOutcome{Result: "EarlyHandoff"}); err == nil {
		t.Fatal("BuildHandoffEnvelope() from a non-holder session error = nil, want an error")
	}
}

func TestBuildAbortEnvelopeAssemblesResultShape(t *testing.T) {
	h := newHistory(t)
	memoryRoot := t.TempDir()
	if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, "artifact.md"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}

	envelope, err := dispatch.BuildAbortEnvelope(h, "", memoryRoot, "root-1", "child-1", "drift detected", "harness")
	if err != nil {
		t.Fatalf("BuildAbortEnvelope() error = %v", err)
	}
	if envelope["result"] != "Aborted" || envelope["additional_context"] != "drift detected" {
		t.Errorf("BuildAbortEnvelope() = %+v, want result=Aborted and the reason as additional context", envelope)
	}
	if ids, ok := envelope["memory"].([]int64); !ok || len(ids) != 0 {
		t.Errorf("BuildAbortEnvelope() memory = %v, want an empty slice for an unseeded memory root", envelope["memory"])
	}
	if h.Project().Budgets("root-1").InterlocutorHolder != "" {
		t.Error("BuildAbortEnvelope() did not clear the interlocutor holder")
	}
}

func TestBuildAbortEnvelopePropagatesAbortDenial(t *testing.T) {
	h := newHistory(t)
	memoryRoot := t.TempDir()
	if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, "artifact.md"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if _, err := dispatch.BuildAbortEnvelope(h, "", memoryRoot, "root-1", "child-1", "drift", "specialist"); err == nil {
		t.Fatal("BuildAbortEnvelope() with an invalid origin error = nil, want an error")
	}
}
