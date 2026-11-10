package memory_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

// relayLines feeds lines to the adapter against the owner of dir and returns each output line.
func relayLines(t *testing.T, dir string, opts memory.AdapterOptions, lines ...string) []map[string]any {
	t.Helper()
	var out bytes.Buffer
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	if err := memory.RunAdapter(context.Background(), dir, opts, in, &out); err != nil {
		t.Fatal(err)
	}
	var replies []map[string]any
	sc := bufio.NewScanner(&out)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("output line is not JSON: %q", sc.Text())
		}
		replies = append(replies, m)
	}
	return replies
}

func callLine(t *testing.T, id int, tool string, args map[string]any, meta map[string]any) string {
	t.Helper()
	params := map[string]any{"name": tool, "arguments": args}
	if meta != nil {
		params["_meta"] = meta
	}
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "tools/call", "params": params})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func resultText(t *testing.T, reply map[string]any) (string, bool) {
	t.Helper()
	res, _ := reply["result"].(map[string]any)
	content, _ := res["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("reply has no content: %v", reply)
	}
	isError, _ := res["isError"].(bool)
	return content[0].(map[string]any)["text"].(string), isError
}

func TestAdapterRelaysCrewUnchanged(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	start(t, dir)
	replies := relayLines(t, dir, memory.AdapterOptions{Crew: true},
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		callLine(t, 2, memory.ToolRecord, recordArgs("a"), nil))
	if len(replies) != 2 {
		t.Fatalf("got %d replies: %v", len(replies), replies)
	}
	tools := replies[0]["result"].(map[string]any)["tools"].([]any)
	if len(tools) != len(memory.Tools) {
		t.Fatalf("/crew lists %d tools, want %d", len(tools), len(memory.Tools))
	}
	// Only /external accepts a call carrying the adapter's session key; a crew relay adds none.
	if _, isError := resultText(t, replies[1]); !isError {
		t.Fatal("a crew call without a host session was accepted")
	}
}

func TestAdapterExternalOverwritesIdentityAndSilencesNotifications(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	start(t, dir)
	workspaceArgs := map[string]any{"nature": "hypothesis", "scope": "workspace", "title": "a", "content": "body"}
	forged := map[string]any{memory.MetaExternalSession: "forged", memory.MetaExternalWorkspace: "forged"}

	replies := relayLines(t, dir, memory.AdapterOptions{Workspace: dir},
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		callLine(t, 1, memory.ToolRecord, workspaceArgs, forged),
		callLine(t, 2, memory.ToolRecord, recordArgs("b"), forged))
	if len(replies) != 2 {
		t.Fatalf("a notification produced output: %v", replies)
	}
	for i, want := range []string{"Recorded #1", "Recorded #2"} {
		if got, isError := resultText(t, replies[i]); isError || got != want {
			t.Fatalf("reply %d: %q (error %v), want %q", i, got, isError, want)
		}
	}
	admin, err := memory.DialAdmin(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	res, err := admin.Describe(context.Background(), memory.DescribeInput{IDs: []int64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	first, second := res.Entries[0].WriterHostSession, res.Entries[1].WriterHostSession
	if first == "" || first == "forged" || first != second {
		t.Fatalf("session keys %q and %q: want one generated key per process", first, second)
	}

	noWorkspace := relayLines(t, dir, memory.AdapterOptions{}, callLine(t, 1, memory.ToolRecord, workspaceArgs, forged))
	if _, isError := resultText(t, noWorkspace[0]); !isError {
		t.Fatal("a client-supplied workspace key was accepted without --workspace")
	}
}

func TestAdapterExternalAnswersANullLineAndKeepsServing(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	start(t, dir)

	replies := relayLines(t, dir, memory.AdapterOptions{Workspace: dir}, `null`, callLine(t, 1, memory.ToolRecord, recordArgs("a"), nil))
	if len(replies) != 2 {
		t.Fatalf("got %d replies, want an error and a result: %v", len(replies), replies)
	}
	rpcErr, _ := replies[0]["error"].(map[string]any)
	if msg, _ := rpcErr["message"].(string); !strings.Contains(msg, "each line must be one JSON-RPC message object") {
		t.Fatalf("null line reply: %v", replies[0])
	}
	if got, isError := resultText(t, replies[1]); isError || got != "Recorded #1" {
		t.Fatalf("line after null: %q (error %v)", got, isError)
	}
}

func TestAdapterPassesTheRefusalBodyOfAnOversizedRequest(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	start(t, dir)
	big := map[string]any{"nature": "hypothesis", "scope": "global", "title": "big", "content": strings.Repeat("x", requestCap+1)}
	replies := relayLines(t, dir, memory.AdapterOptions{}, callLine(t, 7, memory.ToolRecord, big, nil))
	if len(replies) != 1 {
		t.Fatalf("got %v", replies)
	}
	rpcErr, _ := replies[0]["error"].(map[string]any)
	msg, _ := rpcErr["message"].(string)
	if !strings.Contains(msg, "request body exceeds 4194304 bytes") || replies[0]["id"] != float64(7) {
		t.Fatalf("got %v", replies[0])
	}
}
