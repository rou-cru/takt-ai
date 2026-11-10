package main_test

import (
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/rou-cru/takt-ai/takt/history"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// testOwnerInstance names the harness instance the delivery scenarios act as.
const testOwnerInstance = "delivery-instance"

// startMemory gives the session a memory directory under /tmp, short enough for a socket path, with a
// store, and a service the binary launches itself through `memory start`.
func (s *session) startMemory() {
	s.t.Helper()
	dir, err := os.MkdirTemp("/tmp", "tkt")
	if err != nil {
		s.t.Fatal(err)
	}
	s.t.Cleanup(func() {
		testutil.StopMemoryOwner(dir)
		_ = os.RemoveAll(dir)
	})
	s.env = append(s.env, memory.EnvDir+"="+dir)
	s.memoryDir = dir
	if res := s.run("memory", "start", "--init"); res.code != 0 {
		s.t.Fatalf("memory start --init exited %d: %s", res.code, res.stderr)
	}
}

// record binds host as a crew session of the root and records one observation as it, optionally
// superseding an earlier entry, returning the new entry's ID.
func (s *session) record(host, title string, supersedes int64) int64 {
	s.t.Helper()
	ctx := context.Background()
	admin, err := memory.DialAdmin(ctx, s.memoryDir)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	if err := admin.Bind(ctx, memory.BindArgs{HostSession: host, Agent: "pm", ClaimedRoot: rootSession, Workspace: "ws"}); err != nil {
		s.t.Fatal(err)
	}
	args := map[string]any{"nature": "observation", "scope": "workspace", "title": title, "content": "body of " + title, "evidence": "verified in the scenario"}
	if supersedes != 0 {
		args["relates_to"] = map[string]any{"id": supersedes, "relation": memory.RelationSupersedes}
	}
	sock := filepath.Join(s.memoryDir, memory.SocketFile)
	crew, err := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1"}, nil).Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint: "http://memory" + memory.PathCrew, DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "unix", sock)
		}}},
	}, nil)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = crew.Close() }()
	res, err := crew.CallTool(ctx, &mcp.CallToolParams{Name: memory.ToolRecord, Arguments: args, Meta: mcp.Meta{memory.MetaHostSession: host}})
	if err != nil || res.IsError {
		s.t.Fatalf("record %q as %s: %v %+v", title, host, err, res)
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(res.Content[0].(*mcp.TextContent).Text, "Recorded #"), 10, 64)
	if err != nil {
		s.t.Fatal(err)
	}
	return id
}

// requireNamed fails unless the refusal's text contains every want.
func requireNamed(t *testing.T, res result, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(res.stderr, want) {
			t.Errorf("refusal %q does not name %q", res.stderr, want)
		}
	}
}

func idRef(id int64) string { return "#" + strconv.FormatInt(id, 10) }

// deliveryTable covers PR-HAR-24 for a delegated execution: it accepts the specialist's own
// current entries, and refuses a superseded draft, a sibling session's entry,
// and a completed result without IDs, each naming the continuation.
func deliveryTable(t *testing.T, bin string) {
	s := newSession(t, bin)
	const child = "pm-child"
	input := s.record("analyst-child", "declared input", 0)
	s.must(request{Action: "inputs", Session: rootSession, Event: "brief", ResultIDs: []int64{input}})
	s.must(request{Action: "prepare", Owner: testOwnerInstance, Session: rootSession, Event: "brief", Dispatch: "call", Caller: rootSession, Agent: "pm", Child: child})
	draft := s.record(child, "draft", 0)
	final := s.record(child, "final", draft)
	sibling := s.record("pm-sibling", "sibling result", 0)
	deliver := func(ids ...int64) request {
		return request{Action: "deliver", Session: rootSession, Child: child, Agent: "pm", ResultIDs: ids}
	}
	for _, tc := range []struct {
		name string
		ids  []int64
		want string
	}{
		{"superseded draft", []int64{draft}, "current version is " + idRef(final)},
		{"sibling session's entry", []int64{sibling}, "written by another session"},
		{"completed result without IDs", nil, "at least one Takt memory entry ID"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireNamed(t, s.rejected(deliver(tc.ids...)), tc.want, "takt_memory_record", "deliver_result")
		})
	}
	var delivered struct {
		ResultIDs []int64 `json:"result_ids"`
	}
	s.must(deliver(final)).decode(t, &delivered)
	if !slices.Equal(delivered.ResultIDs, []int64{final}) || !slices.Equal(s.projection().Executions["call"].Delivery, []int64{final}) {
		t.Fatalf("delivery of the specialist's own current entry: answered %v, recorded %v", delivered.ResultIDs, s.projection().Executions["call"].Delivery)
	}
}

// topLevelDelivery covers the top-level path: every ID was written by the calling host session and is
// current.
func topLevelDelivery(t *testing.T, bin string) {
	s := newSession(t, bin)
	const top = "top-session"
	before := s.record(top, "recorded long ago", 0)
	draft := s.record(top, "draft", 0)
	final := s.record(top, "final", draft)
	sibling := s.record("other-session", "someone else's", 0)
	validate := func(ids ...int64) request {
		return request{Action: "validate_results", Session: top, ResultIDs: ids}
	}
	s.must(validate(final, before))
	requireNamed(t, s.rejected(validate(draft)), "current version is "+idRef(final), "takt_memory_record")
	requireNamed(t, s.rejected(validate(sibling)), "written by another session")
	requireNamed(t, s.rejected(validate()), "at least one Takt memory entry ID")
	requireNamed(t, s.rejected(validate(final+99)), idRef(final+99)+" does not exist")
}

// inputsAreCurrent covers PR-HAR-37 and the pm rule: a superseded input is refused naming its current
// version, and the pm session that wrote a consumed entry cannot be resumed to consume it.
func inputsAreCurrent(t *testing.T, bin string) {
	s := newSession(t, bin)
	draft := s.record("writer", "draft", 0)
	final := s.record("writer", "final", draft)
	requireNamed(t, s.rejected(request{Action: "inputs", Session: rootSession, Event: "brief", ResultIDs: []int64{draft}}), "current version is "+idRef(final))
	s.must(request{Action: "inputs", Session: rootSession, Event: "brief", ResultIDs: []int64{final}})

	story := s.record("pm-stories", "user story", 0)
	s.must(request{Action: "inputs", Session: rootSession, Event: "business-docs", ResultIDs: []int64{story}})
	prepare := request{Action: "prepare", Owner: testOwnerInstance, Session: rootSession, Event: "business-docs", Dispatch: "resume-call", Caller: rootSession, Agent: "pm", Child: "pm-stories"}
	requireNamed(t, s.rejected(prepare), "fresh pm unit")
	if p := s.projection(); len(p.Executions) != 0 || p.InFlight() != 0 {
		t.Fatal("a refused resumption admitted an execution")
	}
	prepare.Dispatch, prepare.Child = "fresh-call", ""
	s.must(prepare)
	// Only the session that recorded the entry is barred: another pm session consumes it.
	s.must(request{Action: "inputs", Session: rootSession, Event: "other-docs", ResultIDs: []int64{story}})
	other := request{Action: "prepare", Owner: testOwnerInstance, Session: rootSession, Event: "other-docs", Dispatch: "other-call", Caller: rootSession, Agent: "pm", Child: "pm-docs"}
	s.must(other)
}

// failedSwitchBind: a switch whose lent session cannot be bound to the convening root fails
// and records nothing, and validate_switch refuses an absent or non-current requirements list.
func failedSwitchBind(t *testing.T, bin string) {
	s := newSession(t, bin)
	// A switch without an interlocutor specialist is refused before the lent session is bound,
	// and the harness records the denial.
	s.refused(request{Action: "switch", Session: rootSession, Child: "lent", Requirements: []int64{}})
	if holder := s.projection().Budgets(rootSession).InterlocutorHolder; holder != "" {
		t.Fatalf("a switch that failed to bind lent the interface to %q", holder)
	}
	unavailable := newSession(t, bin)
	unavailable.env = append(unavailable.env, memory.EnvDir+"="+filepath.Join(t.TempDir(), "absent"))
	requireNamed(t, unavailable.rejected(request{Action: "switch", Session: rootSession, Child: "lent", Agent: "pm", Requirements: []int64{}}), "Takt memory service is unreachable")
	requireNamed(t, unavailable.rejected(request{Action: "switch", Session: rootSession, Child: "lent", Agent: "pm", Requirements: []int64{}}), "call dispatch_switch again")

	draft := s.record("writer", "draft", 0)
	final := s.record("writer", "final", draft)
	s.must(request{Action: "validate_switch", Session: rootSession, Agent: "pm", Requirements: []int64{}})
	s.must(request{Action: "validate_switch", Session: rootSession, Agent: "pm", Requirements: []int64{final}})
	s.refused(request{Action: "validate_switch", Session: rootSession, Agent: "pm"})
	requireNamed(t, s.refused(request{Action: "validate_switch", Session: rootSession, Agent: "pm", Requirements: []int64{draft}}), "current version is "+idRef(final))
	s.refused(request{Action: "validate_switch", Session: rootSession, Agent: "analyst", Requirements: []int64{}})
	s.rejected(request{Action: "switch", Session: rootSession, Child: "lent", Agent: "pm", Requirements: []int64{draft}})
	if s.count(history.KindInterlocutorSwitched) != 0 {
		t.Fatal("a refused switch was recorded")
	}
}

// handoffNeedsRecordedResults covers PR-HAR-24 for a lent session: a completed handoff delivers the
// entries the holder recorded, the envelope carries exactly those IDs, an early close
// needs none, and validate_handoff refuses what the handoff refuses without recording anything.
func handoffNeedsRecordedResults(t *testing.T, bin string) {
	s := newSession(t, bin)
	const child = "child-1"
	required := s.record("analyst-child", "requirement", 0)
	s.must(request{Action: "switch", Session: rootSession, Child: child, Agent: "pm", Requirements: []int64{required}})
	if holder := s.projection().Budgets(rootSession).InterlocutorHolder; holder != child {
		t.Fatalf("the switch recorded holder %q, want %q", holder, child)
	}
	draft := s.record(child, "draft", 0)
	final := s.record(child, "final", draft)
	sibling := s.record("sibling-child", "sibling result", 0)
	handoff := func(result string, ids ...int64) request {
		return request{Action: "handoff", Session: rootSession, Child: child, Agent: "pm", Result: result, ResultIDs: ids}
	}
	for _, tc := range []struct {
		name string
		req  request
		want string
	}{
		{"completed result without IDs", handoff("Standard"), "at least one Takt memory entry ID"},
		{"superseded draft", handoff("Standard", draft), "current version is " + idRef(final)},
		{"sibling session's entry", handoff("Standard", sibling), "written by another session"},
		{"IDs declared by an early close", handoff("EarlyHandoff", sibling), "written by another session"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requireNamed(t, s.rejected(tc.req), tc.want, "takt_memory_record", "dispatch_handoff")
			validate := tc.req
			validate.Action = "validate_handoff"
			requireNamed(t, s.rejected(validate), tc.want)
		})
	}
	if holder := s.projection().Budgets(rootSession).InterlocutorHolder; holder != child {
		t.Fatalf("a refused handoff released the interface: %q", holder)
	}
	// A valid ID suffices for a completed handoff.
	s.must(request{Action: "validate_handoff", Session: rootSession, Child: child, Result: "Standard", ResultIDs: []int64{final}})
	var resp returned
	s.must(handoff("Standard", final)).decode(t, &resp)
	resp.requireFourFields(t)
	if resp.Envelope.Result != "Standard" || !slices.Equal(resp.Envelope.Memory, []int64{final}) {
		t.Fatalf("handoff envelope = %+v, want result Standard and the delivered [%d]", resp.Envelope, final)
	}
	if holder := s.projection().Budgets(rootSession).InterlocutorHolder; holder != "" || s.count(history.KindInterlocutorHandoff) != 1 {
		t.Fatalf("the handoff did not return the interface: holder %q", holder)
	}
}
