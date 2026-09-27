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

package vfs

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestCycleAttributionSealedAndDurable seals the cycle and mandate class of a
// bound dispatch on every entry it produces, on both stamping paths (binding
// stamp and correlation), leaves other dispatches unattributed, and keeps both
// across close and reopen.
func TestCycleAttributionSealedAndDurable(t *testing.T) {
	f, root, state := durable(t)
	key, e := f.Bind(Identity{SessionID: "s", WorkUnitID: "unit", AttemptID: "1", AgentID: "maint", Specialist: "dev", InvariantsHash: "frozen-invariants", CycleID: "cycle-7", MandateClass: "dead-code"}, []string{"a.txt"})
	if e != nil {
		t.Fatal(e)
	}
	r := apply(t, f, applyCase{key, "create", 0, OpCreate, "a.txt", "x"})
	r = apply(t, f, applyCase{key, "patch", r.Revision, OpPatch, "a.txt", "y"})
	if _, e = f.Apply(Operation{key, "denied", 0, OpCreate, "not-owned", nil}); e == nil {
		t.Fatal("out-of-scope create allowed")
	}
	gate(t, f, key, r)
	if e = f.ConsolidateCheckpoint(key, "cp", r.Revision); e != nil {
		t.Fatal(e)
	}
	check := func(f *FS) {
		t.Helper()
		var flushed, other int
		for _, en := range f.JournalPage("s", "unit", -1, 500) {
			if en.Agent == "maint" {
				if en.CycleID != "cycle-7" || en.MandateClass != "dead-code" {
					t.Errorf("entry %s %s not attributed: %+v", en.Ref(), en.Operation, en)
				}
				if en.Operation == OpFlush {
					flushed++
				}
			} else {
				other++
				if en.CycleID != "" || en.MandateClass != "" {
					t.Errorf("entry of %q attributed: %+v", en.Agent, en)
				}
			}
		}
		if flushed != 1 || other == 0 {
			t.Errorf("flush entries %d, other-agent entries %d; want 1 and some", flushed, other)
		}
	}
	check(f)
	if e = f.Close(); e != nil {
		t.Fatal(e)
	}
	f, e = Open(root, state)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close filesystem: %v", err)
		}
	})
	check(f)
	if id, ok := f.BindingIdentity(key); !ok || id.CycleID != "cycle-7" || id.MandateClass != "dead-code" {
		t.Fatalf("binding lost attribution: %+v %v", id, ok)
	}
}

// TestStoreWrittenBeforeCycleFieldsStillOpens opens a store whose journal and
// bindings were written without the cycle fields: it loads with them empty and
// keeps accepting new dispatches.
func TestStoreWrittenBeforeCycleFieldsStillOpens(t *testing.T) {
	root := t.TempDir()
	canonical, e := filepath.EvalSymlinks(root)
	if e != nil {
		t.Fatal(e)
	}
	state := filepath.Join(t.TempDir(), "private")
	if e = os.Mkdir(state, 0700); e != nil {
		t.Fatal(e)
	}
	dbPath := filepath.Join(state, "vfs.sqlite")
	if e = os.WriteFile(dbPath, nil, 0600); e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", dbPath)
	if e != nil {
		t.Fatal(e)
	}
	legacyState := `{"JournalCount":1,"Version":1,"Workspace":` + quote(canonical) + `,"Deltas":{},"Owners":{},"Collisions":null,"Bindings":{"old":{"SessionID":"s","WorkUnitID":"u","AttemptID":"1","AgentID":"a","Specialist":"dev","Role":"execution","InvariantsHash":"h"}},"Calls":{},"Recovery":null}`
	legacyEntry := `{"role":"execution","session_id":"s","work_unit_id":"u","attempt_id":"1","seq":0,"timestamp":"2026-01-01T00:00:00Z","agent":"a","path":"p","operation":"flush"}`
	for _, stmt := range []struct{ q, data string }{
		{stateSchema, ""},
		{"INSERT INTO state(id,data) VALUES(1,?)", legacyState},
		{"INSERT INTO journal(seq,data) VALUES(0,?)", legacyEntry},
	} {
		if stmt.data == "" {
			_, e = db.Exec(stmt.q)
		} else {
			_, e = db.Exec(stmt.q, stmt.data)
		}
		if e != nil {
			t.Fatal(e)
		}
	}
	if e = db.Close(); e != nil {
		t.Fatal(e)
	}
	f, e := Open(root, state)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close filesystem: %v", err)
		}
	})
	page := f.JournalPage("s", "u", -1, 10)
	if len(page) != 1 || page[0].Operation != OpFlush || page[0].CycleID != "" || page[0].MandateClass != "" {
		t.Fatalf("legacy journal = %+v", page)
	}
	if id, ok := f.BindingIdentity("old"); !ok || id.AgentID != "a" || id.CycleID != "" || id.MandateClass != "" {
		t.Fatalf("legacy binding = %+v %v", id, ok)
	}
	key := bind(t, f, "new", "u2", "dev", "n.txt")
	apply(t, f, applyCase{key, "create", 0, OpCreate, "n.txt", "x"})
	if page = f.JournalPage("s", "u2", -1, 10); len(page) != 1 || page[0].Seq != 1 {
		t.Fatalf("new entry after legacy journal = %+v", page)
	}
}

func quote(s string) string { return `"` + s + `"` }
