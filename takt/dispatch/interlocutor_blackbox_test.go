package dispatch_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/dispatch"
	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
)

// interlocutorTestAgent is a real catalog agent whose role resolves to
// model.RoleDirectInterlocutor (IR-1 eligible), like the fixture of
// interlocutor_test.go.
const interlocutorTestAgent = "pm"

// unusedCheck fails the way an unreachable memory service would, for paths that must not consult it.
func unusedCheck(context.Context, []int64, memory.Rule) (memory.DescribeResult, error) {
	return memory.DescribeResult{}, errors.New("memory was consulted")
}

// switchTo lends the interface of root-1 to child.
func switchTo(h *history.History, child string) error {
	return dispatch.Switch(h, "", "root-1", child, interlocutorTestAgent, func() error { return nil })
}

func TestBuildHandoffEnvelopeAssemblesResultShape(t *testing.T) {
	h := newHistory(t)
	if err := switchTo(h, "child-1"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	// An early close with no result needs no entry, so memory is not consulted.
	outcome := dispatch.HandoffOutcome{Result: "EarlyHandoff", AdditionalContext: "context notes", ExtraArtifacts: []string{"extra.md"}}
	envelope, err := dispatch.BuildHandoffEnvelope(context.Background(), h, "", unusedCheck, "root-1", "child-1", outcome)
	if err != nil {
		t.Fatalf("BuildHandoffEnvelope() error = %v", err)
	}
	if envelope.Result != "EarlyHandoff" || envelope.AdditionalContext != "context notes" || envelope.Memory == nil || len(envelope.Memory) != 0 {
		t.Errorf("BuildHandoffEnvelope() = %+v, want result=EarlyHandoff, the additional context and an empty memory", envelope)
	}
	if h.Project().Budgets("root-1").InterlocutorHolder != "" {
		t.Error("BuildHandoffEnvelope() did not clear the interlocutor holder")
	}
}

func TestHandoffRefusesAMalformedEnvelope(t *testing.T) {
	h := newHistory(t)
	if err := switchTo(h, "child-1"); err != nil {
		t.Fatal(err)
	}
	before := len(h.Entries())
	for _, tc := range []struct {
		name    string
		outcome dispatch.HandoffOutcome
		wants   []string
	}{
		{"unknown result", dispatch.HandoffOutcome{Result: "Done"}, []string{`"Done"`, "Standard, EarlyHandoff, TechFault, Outraged"}},
		{"non-numeric entry reference", dispatch.HandoffOutcome{Result: "EarlyHandoff", ExtraArtifacts: []string{"#abc"}}, []string{`"#abc"`}},
		{"zero entry reference", dispatch.HandoffOutcome{Result: "EarlyHandoff", ExtraArtifacts: []string{"#0"}}, []string{`"#0"`}},
		{"empty reference", dispatch.HandoffOutcome{Result: "EarlyHandoff", ExtraArtifacts: []string{""}}, []string{`""`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := dispatch.ValidateHandoff(context.Background(), h, unusedCheck, "root-1", "child-1", tc.outcome)
			if err == nil {
				t.Fatal("ValidateHandoff() error = nil, want a refusal")
			}
			for _, want := range append(tc.wants, "dispatch_handoff") {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("refusal %q lacks %q", err, want)
				}
			}
			if _, berr := dispatch.BuildHandoffEnvelope(context.Background(), h, "", unusedCheck, "root-1", "child-1", tc.outcome); berr == nil || berr.Error() != err.Error() {
				t.Errorf("BuildHandoffEnvelope() = %v, want %v", berr, err)
			}
			if len(h.Entries()) != before || h.Project().Budgets("root-1").InterlocutorHolder != "child-1" {
				t.Error("a malformed envelope recorded a transition")
			}
		})
	}
}

func TestHandoffAcceptsResolvableExtraArtifacts(t *testing.T) {
	h := newHistory(t)
	if err := switchTo(h, "child-1"); err != nil {
		t.Fatal(err)
	}
	outcome := dispatch.HandoffOutcome{Result: "EarlyHandoff", ExtraArtifacts: []string{"#7", "notes/result.md"}}
	if err := dispatch.ValidateHandoff(context.Background(), h, unusedCheck, "root-1", "child-1", outcome); err != nil {
		t.Errorf("ValidateHandoff() error = %v, want nil", err)
	}
}

func TestBuildHandoffEnvelopePropagatesHandoffDenial(t *testing.T) {
	h := newHistory(t)
	if err := switchTo(h, "child-1"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if _, err := dispatch.BuildHandoffEnvelope(context.Background(), h, "", unusedCheck, "root-1", "someone-else", dispatch.HandoffOutcome{Result: "EarlyHandoff"}); err == nil {
		t.Fatal("BuildHandoffEnvelope() from a non-holder session error = nil, want an error")
	}
	if h.Project().Budgets("root-1").InterlocutorHolder != "child-1" {
		t.Error("a denied handoff released the interface")
	}
}

// TestBuildHandoffEnvelopeEarlyHandoffReturnsActualStateWithoutRequiredDocument
// covers #15638 A22 and #15633 A17 against PIRS IR-22/IR-23: an EarlyHandoff
// carries exactly the four envelope fields, returns the work actually produced
// (partial or absent) without requiring any document.
func TestBuildHandoffEnvelopeEarlyHandoffReturnsActualStateWithoutRequiredDocument(t *testing.T) {
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
			if err := switchTo(h, "child-1"); err != nil {
				t.Fatalf("Switch() error = %v", err)
			}

			outcome := dispatch.HandoffOutcome{Result: "EarlyHandoff", AdditionalContext: tc.context, ExtraArtifacts: tc.extra}
			envelope, err := dispatch.BuildHandoffEnvelope(context.Background(), h, "", unusedCheck, "root-1", "child-1", outcome)
			if err != nil {
				t.Fatalf("BuildHandoffEnvelope() error = %v, want the early handoff accepted", err)
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
				Memory            []int64  `json:"memory"`
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
			}
			if len(got.Memory) != 0 {
				t.Errorf("memory = %v, want empty for an early close with no result", got.Memory)
			}
			if h.Project().Budgets("root-1").InterlocutorHolder != "" {
				t.Error("BuildHandoffEnvelope() did not return the interface to the convening agent")
			}
		})
	}
}

// TestBuildAbortEnvelopeAssemblesResultShape covers that a synthesized abort leaves memory empty.
func TestBuildAbortEnvelopeAssemblesResultShape(t *testing.T) {
	h := newHistory(t)
	if err := switchTo(h, "child-1"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	envelope, child, err := dispatch.BuildAbortEnvelope(h, "", "root-1", "child-1", "drift detected", "harness")
	if err != nil {
		t.Fatalf("BuildAbortEnvelope() error = %v", err)
	}
	if envelope.Result != "Aborted" || envelope.AdditionalContext != "drift detected" || child != "child-1" {
		t.Errorf("BuildAbortEnvelope() = %+v, %q, want result=Aborted, the reason as additional context and child-1", envelope, child)
	}
	if envelope.Memory == nil || len(envelope.Memory) != 0 {
		t.Errorf("BuildAbortEnvelope() memory = %v, want empty", envelope.Memory)
	}
	if h.Project().Budgets("root-1").InterlocutorHolder != "" {
		t.Error("BuildAbortEnvelope() did not clear the interlocutor holder")
	}
}

func TestBuildAbortEnvelopePropagatesAbortDenial(t *testing.T) {
	h := newHistory(t)
	if err := switchTo(h, "child-1"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if _, _, err := dispatch.BuildAbortEnvelope(h, "", "root-1", "child-1", "drift", "specialist"); err == nil {
		t.Fatal("BuildAbortEnvelope() with an invalid origin error = nil, want an error")
	}
}

func TestCurrentAgentFollowsTheSwitch(t *testing.T) {
	h := newHistory(t)
	if got := dispatch.CurrentAgent(h, "root-1"); got != "" {
		t.Fatalf("CurrentAgent() before any switch = %q, want empty", got)
	}
	if err := switchTo(h, "child-1"); err != nil {
		t.Fatalf("Switch() error = %v", err)
	}
	if got := dispatch.CurrentAgent(h, "root-1"); got != interlocutorTestAgent {
		t.Fatalf("CurrentAgent() after Switch = %q, want %q", got, interlocutorTestAgent)
	}
	if _, err := dispatch.BuildHandoffEnvelope(context.Background(), h, "", unusedCheck, "root-1", "child-1", dispatch.HandoffOutcome{Result: "EarlyHandoff"}); err != nil {
		t.Fatalf("BuildHandoffEnvelope() error = %v", err)
	}
	if got := dispatch.CurrentAgent(h, "root-1"); got != "" {
		t.Fatalf("CurrentAgent() after handoff = %q, want empty", got)
	}
}
