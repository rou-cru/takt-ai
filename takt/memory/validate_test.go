package memory_test

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/memory"
)

// recordAs binds host session as a crew session of root and records one observation, returning its ID.
func recordAs(t *testing.T, r *running, host, title string, relates *memory.RelatesTo) int64 {
	t.Helper()
	admin, err := memory.DialAdmin(context.Background(), r.dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()
	if err := admin.Bind(context.Background(), memory.BindArgs{HostSession: host, Agent: "pm", ClaimedRoot: "root", Workspace: "ws"}); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"nature": "observation", "scope": "workspace", "title": title, "content": "body of " + title, "evidence": "verified in the test"}
	if relates != nil {
		args["relates_to"] = relates
	}
	text := text(t, call(t, r.connect(t, memory.PathCrew), memory.ToolRecord, args, crewMeta(host)))
	id, err := strconv.ParseInt(strings.TrimPrefix(text, "Recorded #"), 10, 64)
	if err != nil {
		t.Fatalf("record answered %q", text)
	}
	return id
}

func TestValidateRules(t *testing.T) {
	dir := shortDir(t)
	initStore(t, dir)
	r := start(t, dir)
	own := recordAs(t, r, "child", "draft", nil)
	sibling := recordAs(t, r, "sibling", "other", nil)
	revised := recordAs(t, r, "child", "final", &memory.RelatesTo{ID: own, Relation: "supersedes"})
	admin, err := memory.DialAdmin(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = admin.Close() }()

	for _, tc := range []struct {
		name string
		ids  []int64
		rule memory.Rule
		want string
	}{
		{"current own entry", []int64{revised}, memory.Rule{WrittenBy: "child"}, ""},
		{"inputs need only be current", []int64{sibling, revised}, memory.Rule{}, ""},
		{"superseded names its current version", []int64{own}, memory.Rule{}, "current version is #" + itoa(revised)},
		{"sibling session", []int64{sibling}, memory.Rule{WrittenBy: "child"}, "written by another session"},
		{"unknown id", []int64{999}, memory.Rule{}, "#999 does not exist"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := memory.Validate(context.Background(), admin, tc.ids, tc.rule)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Validate() error = %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate() error = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}
