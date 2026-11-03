package dispatch_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
)

// interlocutorTestAgent is a real catalog agent whose role resolves to
// model.RoleDirectInterlocutor (IR-1 eligible), like the fixture of
// interlocutor_test.go.
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
	if envelope.Result != "EarlyHandoff" || envelope.AdditionalContext != "context notes" {
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

// TestBuildHandoffEnvelopeEarlyHandoffReturnsActualStateWithoutManufacturedArtifact
// covers #15638 A22 and #15633 A17 against PIRS IR-22/IR-23: an EarlyHandoff
// carries exactly the four envelope fields, returns the work actually produced
// (partial or absent), and neither manufactures nor requires the switch's
// declared expected artifact.
func TestBuildHandoffEnvelopeEarlyHandoffReturnsActualStateWithoutManufacturedArtifact(t *testing.T) {
	const expectedArtifact = "artifact.md"
	partial := filepath.Join(t.TempDir(), "partial-draft.md")
	if err := os.WriteFile(partial, []byte("# Draft\n\nIncomplete section.\n"), 0o600); err != nil {
		t.Fatalf("write partial artifact: %v", err)
	}

	cases := []struct {
		name    string
		context string
		extra   []string
	}{
		{name: "partial", context: "The user closed early; the draft is returned in its partial state.", extra: []string{partial}},
		{name: "absent", context: "The user closed early; no artifact was produced.", extra: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHistory(t)
			memoryRoot := t.TempDir()
			if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, expectedArtifact); err != nil {
				t.Fatalf("Switch() error = %v", err)
			}

			outcome := dispatch.HandoffOutcome{Result: "EarlyHandoff", AdditionalContext: tc.context, ExtraArtifacts: tc.extra}
			envelope, err := dispatch.BuildHandoffEnvelope(h, "", memoryRoot, "root-1", "child-1", interlocutorTestAgent, outcome)
			if err != nil {
				t.Fatalf("BuildHandoffEnvelope() error = %v, want the early handoff accepted without its expected artifact", err)
			}

			raw, err := json.Marshal(envelope)
			if err != nil {
				t.Fatalf("marshal envelope: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatalf("decode envelope: %v", err)
			}
			for _, key := range []string{"result", "additional_context", "extra_artifacts", "memory"} {
				if _, ok := fields[key]; !ok {
					t.Errorf("envelope %s lacks field %q; all four fields are always present", raw, key)
				}
			}
			if len(fields) != 4 {
				t.Errorf("envelope %s has %d fields, want exactly four", raw, len(fields))
			}

			var got struct {
				Result            string   `json:"result"`
				AdditionalContext string   `json:"additional_context"`
				ExtraArtifacts    []string `json:"extra_artifacts"`
				Memory            struct {
					Entries []int64 `json:"entries"`
				} `json:"memory"`
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("decode envelope fields: %v", err)
			}
			if got.Result != "EarlyHandoff" {
				t.Errorf("result = %q, want EarlyHandoff", got.Result)
			}
			if got.AdditionalContext != tc.context {
				t.Errorf("additional_context = %q, want %q", got.AdditionalContext, tc.context)
			}
			if len(got.ExtraArtifacts) != len(tc.extra) {
				t.Fatalf("extra_artifacts = %v, want exactly the declared artifacts %v", got.ExtraArtifacts, tc.extra)
			}
			for i, ref := range got.ExtraArtifacts {
				if ref != tc.extra[i] {
					t.Errorf("extra_artifacts[%d] = %q, want %q", i, ref, tc.extra[i])
				}
				if filepath.Base(ref) == expectedArtifact {
					t.Errorf("extra_artifacts = %v manufactures the undelivered expected artifact %q", got.ExtraArtifacts, expectedArtifact)
				}
			}
			if len(got.Memory.Entries) != 0 {
				t.Errorf("memory = %v, want empty for an unseeded memory root", got.Memory)
			}
			if h.Project().Budgets("root-1").InterlocutorHolder != "" {
				t.Error("BuildHandoffEnvelope() did not return the interface to the convening agent")
			}
		})
	}
}

func TestBuildAbortEnvelopeAssemblesResultShape(t *testing.T) {
	h := newHistory(t)
	memoryRoot := t.TempDir()
	if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, "artifact.md"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}

	envelope, child, err := dispatch.BuildAbortEnvelope(h, "", memoryRoot, "root-1", "child-1", "drift detected", "harness")
	if err != nil {
		t.Fatalf("BuildAbortEnvelope() error = %v", err)
	}
	if envelope.Result != "Aborted" || envelope.AdditionalContext != "drift detected" || child != "child-1" {
		t.Errorf("BuildAbortEnvelope() = %+v, %q, want result=Aborted, the reason as additional context and child-1", envelope, child)
	}
	if envelope.Memory.Entries == nil || len(envelope.Memory.Entries) != 0 || envelope.Memory.NextAfter != 0 {
		t.Errorf("BuildAbortEnvelope() memory = %+v, want a complete empty index for an unseeded memory root", envelope.Memory)
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
	if _, _, err := dispatch.BuildAbortEnvelope(h, "", memoryRoot, "root-1", "child-1", "drift", "specialist"); err == nil {
		t.Fatal("BuildAbortEnvelope() with an invalid origin error = nil, want an error")
	}
}

// seedLentMemory writes the memory session index of root as the memory store
// persists it: each entry recorded by caller.
func seedLentMemory(t *testing.T, memoryRoot, root, caller string, ids []int64) {
	t.Helper()
	entries := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		entries = append(entries, map[string]any{"id": id, "author": interlocutorTestAgent, "caller": caller})
	}
	writeSessionIndex(t, memoryRoot, root, map[string]any{"session": root, "entries": entries})
}

func writeSessionIndex(t *testing.T, memoryRoot, root string, index any) {
	t.Helper()
	data, err := json.Marshal(index)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(memoryRoot, ".takt-ai", "memory", "sessions")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, root+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCurrentAgentFollowsTheSwitch(t *testing.T) {
	h := newHistory(t)
	if got := dispatch.CurrentAgent(h, "root-1"); got != "" {
		t.Fatalf("CurrentAgent() before any switch = %q, want empty", got)
	}
	if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, "artifact.md"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if got := dispatch.CurrentAgent(h, "root-1"); got != interlocutorTestAgent {
		t.Fatalf("CurrentAgent() after Switch = %q, want %q", got, interlocutorTestAgent)
	}
	if _, err := dispatch.BuildHandoffEnvelope(h, "", t.TempDir(), "root-1", "child-1", interlocutorTestAgent, dispatch.HandoffOutcome{Result: "EarlyHandoff"}); err != nil {
		t.Fatalf("BuildHandoffEnvelope() error = %v", err)
	}
	if got := dispatch.CurrentAgent(h, "root-1"); got != "" {
		t.Fatalf("CurrentAgent() after handoff = %q, want empty", got)
	}
}

// TestLentEntriesPagesOnlyTheLentSession covers IR-22: the index carries the
// entries the lent session recorded, a policy page at a time, and refuses a
// session that never held the interface.
func TestLentEntriesPagesOnlyTheLentSession(t *testing.T) {
	h := newHistory(t)
	memoryRoot := t.TempDir()
	page := loadPolicy(t).Interlocutor.MemoryPage
	ids := make([]int64, 0, page+2)
	for i := range page + 2 {
		ids = append(ids, int64(100+i))
	}
	seedLentMemory(t, memoryRoot, "root-1", "child-1", ids)

	if _, err := dispatch.LentEntries(h, memoryRoot, "root-1", "child-1", 0); err == nil {
		t.Fatal("LentEntries() for a session that never held the interface error = nil, want a refusal")
	}
	if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, "artifact.md"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}

	first, err := dispatch.LentEntries(h, memoryRoot, "root-1", "child-1", 0)
	if err != nil {
		t.Fatalf("LentEntries() error = %v", err)
	}
	if !slices.Equal(first.Entries, ids[:page]) || first.NextAfter != ids[page-1] {
		t.Fatalf("LentEntries() first page = %+v, want %d entries and next_after %d", first, page, ids[page-1])
	}
	rest, err := dispatch.LentEntries(h, memoryRoot, "root-1", "child-1", first.NextAfter)
	if err != nil {
		t.Fatalf("LentEntries(next_after) error = %v", err)
	}
	if !slices.Equal(rest.Entries, ids[page:]) || rest.NextAfter != 0 {
		t.Fatalf("LentEntries(next_after) = %+v, want the remaining %v and no cursor", rest, ids[page:])
	}
	if _, err := dispatch.LentEntries(h, memoryRoot, "root-1", "child-1", 7); err == nil {
		t.Fatal("LentEntries() from an entry outside the index error = nil, want a refusal")
	}
}

// TestEnvelopesSurfaceAnUnreadableMemoryIndex: an envelope never reports an
// empty index when the lent session's index cannot be read.
func TestEnvelopesSurfaceAnUnreadableMemoryIndex(t *testing.T) {
	memoryRoot := t.TempDir()
	writeSessionIndex(t, memoryRoot, "root-1", "not an index")

	h := newHistory(t)
	if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, "artifact.md"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if _, err := dispatch.BuildHandoffEnvelope(h, "", memoryRoot, "root-1", "child-1", interlocutorTestAgent, dispatch.HandoffOutcome{Result: "EarlyHandoff"}); err == nil {
		t.Fatal("BuildHandoffEnvelope() with an unreadable index error = nil, want an error")
	}

	h = newHistory(t)
	if err := dispatch.Switch(h, "", "root-1", "child-1", interlocutorTestAgent, "artifact.md"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if _, _, err := dispatch.BuildAbortEnvelope(h, "", memoryRoot, "root-1", "child-1", "drift detected", "harness"); err == nil {
		t.Fatal("BuildAbortEnvelope() with an unreadable index error = nil, want an error")
	}
}
