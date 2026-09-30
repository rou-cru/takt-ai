// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// foreignFileBody pads a non-database file past the SQLite header size.
	foreignFileBody = "not a sqlite database, only text; "
	foreignFileReps = 100
	// gapSeq is a sequence position that skips the recorded prefix.
	gapSeq = 5
)

func TestOpenFailsWhenStateDirCannotBeCreated(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("x"), stateFileMode); err != nil {
		t.Fatal(err)
	}
	if h, err := Open(filepath.Join(blocker, "state")); err == nil {
		_ = h.Close()
		t.Fatal("Open succeeded beneath a regular file")
	}
}

func TestOpenFailsWhenStoreFileCannotBeOpened(t *testing.T) {
	state := t.TempDir()
	if err := os.Mkdir(filepath.Join(state, historyFile), stateDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if h, err := Open(state); err == nil {
		_ = h.Close()
		t.Fatal("Open succeeded with a directory in place of the store")
	}
}

func TestOpenRejectsForeignStore(t *testing.T) {
	state := t.TempDir()
	body := strings.Repeat(foreignFileBody, foreignFileReps)
	if err := os.WriteFile(filepath.Join(state, historyFile), []byte(body), stateFileMode); err != nil {
		t.Fatal(err)
	}
	if h, err := Open(state); err == nil {
		_ = h.Close()
		t.Fatal("Open accepted a file that is not a database")
	}
}

// insertRaw writes one row straight into the store, bypassing Append's validation.
func insertRaw(t *testing.T, state string, seq int, data string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(state, historyFile))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}()
	if _, err := db.Exec("INSERT INTO history(seq,data) VALUES(?,?)", seq, []byte(data)); err != nil {
		t.Fatal(err)
	}
}

func TestOpenRefusesCorruptRecords(t *testing.T) {
	for name, tc := range map[string]struct {
		seq  int
		data string
		want string
	}{
		"undecodable record": {0, "{not json", "invalid character"},
		"sequence gap":       {gapSeq, `{"seq":5}`, "sequence corrupt"},
		"sequence mismatch":  {0, `{"seq":3}`, "sequence corrupt"},
	} {
		t.Run(name, func(t *testing.T) {
			state := t.TempDir()
			open(t, state) // creates the schema
			insertRaw(t, state, tc.seq, tc.data)
			h, err := Open(state)
			if err == nil {
				_ = h.Close()
				t.Fatal("Open replayed a corrupt history instead of failing")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Open error = %v; want %q", err, tc.want)
			}
		})
	}
}

func TestAppendFailsAfterCloseWithoutRecording(t *testing.T) {
	h, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(observed(t, "a", KindAdmitted, "")); err == nil {
		t.Fatal("Append succeeded on a closed store")
	}
	if n := len(h.Entries()); n != 0 {
		t.Fatalf("a failed write left %d entries in memory", n)
	}
}

func activityEntry(kind Kind, node NodeKind, outcome Outcome) Entry {
	return Entry{Author: AuthorOf(kind), Kind: kind, SessionID: "root", ActivityID: "act", NodeKind: node,
		Cause: CauseNone, Outcome: outcome}
}

func TestAppendValidatesActivityRecords(t *testing.T) {
	h := open(t, t.TempDir())
	for name, tc := range map[string]struct {
		entry Entry
		want  string
	}{
		"orchestrator start":    {activityEntry(KindActivityStarted, NodeKindOrchestrator, ""), ""},
		"orchestrator finish":   {activityEntry(KindActivityFinished, NodeKindOrchestrator, OutcomeCompleted), ""},
		"maintenance start":     {activityEntry(KindMaintenanceStarted, NodeKindMaintenance, ""), ""},
		"maintenance interrupt": {activityEntry(KindMaintenanceFinished, NodeKindMaintenance, OutcomeInterrupted), ""},
		"missing activity id": {func() Entry {
			e := activityEntry(KindActivityStarted, NodeKindOrchestrator, "")
			e.ActivityID = ""
			return e
		}(), "require activity_id"},
		"carries work unit": {func() Entry {
			e := activityEntry(KindActivityStarted, NodeKindOrchestrator, "")
			e.WorkUnitID = "u"
			return e
		}(), "require activity_id"},
		"wrong node kind":        {activityEntry(KindActivityStarted, NodeKindMaintenance, ""), "requires node kind"},
		"finish without outcome": {activityEntry(KindActivityFinished, NodeKindOrchestrator, ""), "only terminal"},
		"finish backtracked":     {activityEntry(KindActivityFinished, NodeKindOrchestrator, OutcomeBacktracked), "invalid outcome"},
		"start with outcome":     {activityEntry(KindActivityStarted, NodeKindOrchestrator, OutcomeCompleted), "only terminal"},
		"work entry with activity id": {func() Entry {
			e := observed(t, "a", KindAdmitted, "")
			e.ActivityID = "act"
			return e
		}(), "work entries require"},
		"work entry on orchestrator node": {func() Entry {
			e := observed(t, "a", KindAdmitted, "")
			e.NodeKind = NodeKindOrchestrator
			return e
		}(), "require node kind delegated"},
		"unknown outcome": {observed(t, "a", KindTerminated, "vanished"), "unknown outcome"},
		"wrong author":    {func() Entry { e := observed(t, "a", KindAdmitted, ""); e.Author = AuthorOrchestrator; return e }(), "is authored by"},
		"no session":      {func() Entry { e := observed(t, "a", KindAdmitted, ""); e.SessionID = ""; return e }(), "session identity required"},
	} {
		t.Run(name, func(t *testing.T) {
			before := len(h.Entries())
			err := h.Append(tc.entry)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("Append() error = %v", err)
				}
				if got := len(h.Entries()); got != before+1 {
					t.Fatalf("entries = %d; want %d", got, before+1)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Append() error = %v; want %q", err, tc.want)
			}
			if got := len(h.Entries()); got != before {
				t.Fatalf("rejected entry was recorded: %d entries, want %d", got, before)
			}
		})
	}
}

func TestAppendValidatesRevisions(t *testing.T) {
	h := open(t, t.TempDir())
	revised := func(mutate func(*Entry)) Entry {
		e := Entry{Author: AuthorOf(KindRevised), Kind: KindRevised, SessionID: "root", WorkUnitID: "plan", AttemptID: FirstAttempt,
			Cause: CauseNone, BaseVersion: "v1", PlanVersion: "v2", Classification: ClassificationTactical}
		mutate(&e)
		return e
	}
	for name, tc := range map[string]struct {
		entry Entry
		want  string
	}{
		"tactical":           {revised(func(*Entry) {}), ""},
		"strategic":          {revised(func(e *Entry) { e.Classification = ClassificationStrategic }), ""},
		"missing base":       {revised(func(e *Entry) { e.BaseVersion = "" }), "expected base version"},
		"missing result":     {revised(func(e *Entry) { e.PlanVersion = "" }), "expected base version"},
		"unknown class":      {revised(func(e *Entry) { e.Classification = "cosmetic" }), "unknown revision classification"},
		"invalid w/o reason": {revised(func(e *Entry) { e.Kind, e.Author = KindInvalidRevision, AuthorOf(KindInvalidRevision) }), "records its reason"},
		"invalid with reason": {revised(func(e *Entry) {
			e.Kind, e.Author, e.Reason = KindInvalidRevision, AuthorOf(KindInvalidRevision), "cycle"
		}), ""},
	} {
		t.Run(name, func(t *testing.T) {
			err := h.Append(tc.entry)
			if tc.want == "" && err != nil {
				t.Fatalf("Append() error = %v", err)
			}
			if tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("Append() error = %v; want %q", err, tc.want)
			}
		})
	}
}
