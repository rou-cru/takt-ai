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
package gc_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

func e(op vfs.OperationType, path, before, after string) vfs.JournalEntry {
	return vfs.JournalEntry{SessionID: "s", Operation: op, Path: path, BeforeHash: before, AfterHash: after}
}

var req = gc.Request{SessionID: "s", CycleID: "c1", Mandate: gc.MandateDeadCode}

func TestSessionDeltaIsWhatChangedNotWhatWasOpened(t *testing.T) {
	entries := []vfs.JournalEntry{
		e(vfs.OpRead, "read.go", "", ""),
		e(vfs.OpCreate, "new.go", "", "h1"),
		e(vfs.OpPatch, "old.go", "h0", "h2"),
		e(vfs.OpDelete, "gone.go", "h3", ""),
		e(vfs.OpFlush, "old.go", "h0", "h2"),
	}
	want := []gc.Change{{Path: "gone.go", Deleted: true}, {Path: "new.go", Introduced: true}, {Path: "old.go"}}
	if got := gc.SessionDelta(entries, "s"); !reflect.DeepEqual(got, want) {
		t.Fatalf("delta = %+v, want %+v", got, want)
	}
}

func TestSessionDeltaDropsRolledBackAndNetZeroPaths(t *testing.T) {
	entries := []vfs.JournalEntry{
		e(vfs.OpPatch, "rolled.go", "h0", "h1"),
		e(vfs.OpRollback, "rolled.go", "h1", ""),
		e(vfs.OpCreate, "temp.go", "", "h2"),
		e(vfs.OpDelete, "temp.go", "h2", ""),
		e(vfs.OpPatch, "flushed.go", "h0", "h3"),
		e(vfs.OpFlush, "flushed.go", "h0", "h3"),
		e(vfs.OpPatch, "flushed.go", "h3", "h4"),
		e(vfs.OpRollback, "flushed.go", "h4", ""),
	}
	want := []gc.Change{{Path: "flushed.go"}}
	if got := gc.SessionDelta(entries, "s"); !reflect.DeepEqual(got, want) {
		t.Fatalf("delta = %+v, want %+v", got, want)
	}
}

func TestSessionDeltaKeepsIntroducedAcrossLaterEdits(t *testing.T) {
	entries := []vfs.JournalEntry{
		e(vfs.OpCreate, "a.go", "", "h1"),
		e(vfs.OpFlush, "a.go", "", "h1"),
		e(vfs.OpPatch, "a.go", "h1", "h2"),
		e(vfs.OpFlush, "a.go", "h1", "h2"),
	}
	if got := gc.SessionDelta(entries, "s"); !reflect.DeepEqual(got, []gc.Change{{Path: "a.go", Introduced: true}}) {
		t.Fatalf("delta = %+v", got)
	}
}

func TestSessionDeltaIgnoresOtherSessionsCyclesAndDeniedEntries(t *testing.T) {
	other := e(vfs.OpPatch, "other.go", "h0", "h1")
	other.SessionID = "t"
	cycle := e(vfs.OpFlush, "cleaned.go", "h0", "h1")
	cycle.CycleID = "c0"
	denied := e(vfs.OpPatch, "denied.go", "h0", "h1")
	denied.Outcome = "denied"
	if got := gc.SessionDelta([]vfs.JournalEntry{other, cycle, denied}, "s"); len(got) != 0 {
		t.Fatalf("delta = %+v, want empty", got)
	}
}

func TestDeclareClosureFollowsReachabilityOutward(t *testing.T) {
	entries := []vfs.JournalEntry{e(vfs.OpPatch, "a.go", "h0", "h1"), e(vfs.OpCreate, "b.go", "", "h2")}
	p, err := gc.Declare(context.Background(), entries, req, gc.Reach(func(_ context.Context, path string) ([]string, error) {
		return map[string][]string{"a.go": {"x.go", "y.go"}, "b.go": {"y.go"}}[path], nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.go", "b.go", "x.go", "y.go"}; !reflect.DeepEqual(p.Closure, want) {
		t.Fatalf("closure = %v, want %v", p.Closure, want)
	}
	if p.Reachability != gc.ReachCodegraph || p.Gap != "" || p.Mandate != gc.MandateDeadCode || p.CycleID != "c1" {
		t.Fatalf("plan = %+v", p)
	}
}

func TestDeclareFallsBackToDeltaAndRecordsTheGap(t *testing.T) {
	entries := []vfs.JournalEntry{e(vfs.OpPatch, "a.go", "h0", "h1")}
	for name, reach := range map[string]gc.Reach{"absent": nil, "failing": func(context.Context, string) ([]string, error) {
		return nil, errors.New("index stale")
	}} {
		p, err := gc.Declare(context.Background(), entries, req, reach)
		if err != nil {
			t.Fatal(err)
		}
		if p.Reachability != gc.ReachJournalOnly || p.Gap == "" || !reflect.DeepEqual(p.Closure, []string{"a.go"}) {
			t.Fatalf("%s: plan = %+v", name, p)
		}
	}
}

func TestDeclareRequiresIdentityAndOneKnownMandate(t *testing.T) {
	for name, r := range map[string]gc.Request{
		"no cycle":      {SessionID: "s", Mandate: gc.MandateDeadCode},
		"no session":    {CycleID: "c", Mandate: gc.MandateDeadCode},
		"no mandate":    {SessionID: "s", CycleID: "c"},
		"unknown class": {SessionID: "s", CycleID: "c", Mandate: "dead-code,complexity"},
	} {
		if _, err := gc.Declare(context.Background(), nil, r, nil); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

// fakeCodegraph replays outputs captured from codegraph 1.6.0.
func fakeCodegraph(calls *[]string) gc.Runner {
	return func(_ context.Context, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(args, " "))
		switch args[0] {
		case "node":
			return []byte("**a.go** — 2 symbols, used by 1 file: b.go\n\n**Symbols**\n- `Session` (struct) — :34\n- `Close` (method) () error — :46\n- `Close` (method) () error — :90\n"), nil
		case "query":
			return []byte(`[{"node":{"qualifiedName":"Store::Close","filePath":"store.go"}},{"node":{"qualifiedName":"Session::Close","filePath":"a.go"}},{"node":{"qualifiedName":"Session","filePath":"a.go"}}]`), nil
		}
		if args[len(args)-1] == "Session" {
			return []byte("ℹ Symbol \"Session\" not found\n"), nil
		}
		return []byte(`{"symbol":"Session::Close","affected":[{"filePath":"a.go"},{"filePath":"mcp.go"}]}`), nil
	}
}

func TestCodegraphDependentsResolvesQualifiedSymbolsBeforeImpact(t *testing.T) {
	var calls []string
	got, err := gc.NewCodegraph(fakeCodegraph(&calls)).Dependents(context.Background(), "a.go")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.go", "mcp.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependents = %v, want %v", got, want)
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "impact") && strings.HasSuffix(c, "Store::Close") {
			t.Fatalf("impact ran on another file's symbol: %v", calls)
		}
	}
}

func TestCodegraphDependentsSurfacesRunnerFailure(t *testing.T) {
	run := func(context.Context, ...string) ([]byte, error) { return nil, errors.New("not initialized") }
	if _, err := gc.NewCodegraph(run).Dependents(context.Background(), "a.go"); err == nil {
		t.Fatal("failure swallowed")
	}
}
