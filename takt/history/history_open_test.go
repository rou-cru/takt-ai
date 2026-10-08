// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package history_test

import (
	"database/sql"
	"github.com/rou-cru/takt-ai/takt/history"
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
	if err := os.WriteFile(blocker, []byte("x"), privateFileMode); err != nil {
		t.Fatal(err)
	}
	if h, err := history.Open(filepath.Join(blocker, "state")); err == nil {
		_ = h.Close()
		t.Fatal("Open succeeded beneath a regular file")
	}
}

func TestOpenFailsWhenStoreFileCannotBeOpened(t *testing.T) {
	state := t.TempDir()
	if err := os.Mkdir(filepath.Join(state, storeFile), privateDirMode); err != nil {
		t.Fatal(err)
	}
	if h, err := history.Open(state); err == nil {
		_ = h.Close()
		t.Fatal("Open succeeded with a directory in place of the store")
	}
}

func TestOpenRejectsForeignStore(t *testing.T) {
	state := t.TempDir()
	body := strings.Repeat(foreignFileBody, foreignFileReps)
	if err := os.WriteFile(filepath.Join(state, storeFile), []byte(body), privateFileMode); err != nil {
		t.Fatal(err)
	}
	if h, err := history.Open(state); err == nil {
		_ = h.Close()
		t.Fatal("Open accepted a file that is not a database")
	}
}

// insertRaw writes one row straight into the store, bypassing Append's validation.
func insertRaw(t *testing.T, state string, seq int, data string) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(state, storeFile))
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
			h, err := history.Open(state)
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
	h, err := history.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	if err := h.Append(observed(t, "a", history.KindAdmitted, "")); err == nil {
		t.Fatal("Append succeeded on a closed store")
	}
	if n := len(h.Entries()); n != 0 {
		t.Fatalf("a failed write left %d entries in memory", n)
	}
}

func activityEntry(kind history.Kind, node history.NodeKind, outcome history.Outcome) history.Entry {
	return history.Entry{Author: history.AuthorOf(kind), Kind: kind, SessionID: "root", ActivityID: "act", NodeKind: node,
		Cause: history.CauseNone, Outcome: outcome}
}

func TestAppendValidatesActivityRecords(t *testing.T) {
	h := open(t, t.TempDir())
	for name, tc := range map[string]struct {
		entry history.Entry
		want  string
	}{
		"orchestrator start":    {activityEntry(history.KindActivityStarted, history.NodeKindOrchestrator, ""), ""},
		"orchestrator finish":   {activityEntry(history.KindActivityFinished, history.NodeKindOrchestrator, history.OutcomeCompleted), ""},
		"maintenance start":     {activityEntry(history.KindMaintenanceStarted, history.NodeKindMaintenance, ""), ""},
		"maintenance interrupt": {activityEntry(history.KindMaintenanceFinished, history.NodeKindMaintenance, history.OutcomeInterrupted), ""},
		"missing activity id": {func() history.Entry {
			e := activityEntry(history.KindActivityStarted, history.NodeKindOrchestrator, "")
			e.ActivityID = ""
			return e
		}(), "require activity_id"},
		"carries work unit": {func() history.Entry {
			e := activityEntry(history.KindActivityStarted, history.NodeKindOrchestrator, "")
			e.WorkUnitID = "u"
			return e
		}(), "require activity_id"},
		"wrong node kind":        {activityEntry(history.KindActivityStarted, history.NodeKindMaintenance, ""), "requires node kind"},
		"finish without outcome": {activityEntry(history.KindActivityFinished, history.NodeKindOrchestrator, ""), "only terminal"},
		"finish backtracked":     {activityEntry(history.KindActivityFinished, history.NodeKindOrchestrator, history.OutcomeBacktracked), "invalid outcome"},
		"start with outcome":     {activityEntry(history.KindActivityStarted, history.NodeKindOrchestrator, history.OutcomeCompleted), "only terminal"},
		"work entry with activity id": {func() history.Entry {
			e := observed(t, "a", history.KindAdmitted, "")
			e.ActivityID = "act"
			return e
		}(), "work entries require"},
		"work entry on orchestrator node": {func() history.Entry {
			e := observed(t, "a", history.KindAdmitted, "")
			e.NodeKind = history.NodeKindOrchestrator
			return e
		}(), "require node kind delegated"},
		"unknown outcome": {observed(t, "a", history.KindTerminated, "vanished"), "unknown outcome"},
		"wrong author": {func() history.Entry {
			e := observed(t, "a", history.KindAdmitted, "")
			e.Author = history.AuthorOrchestrator
			return e
		}(), "is authored by"},
		"no session": {func() history.Entry { e := observed(t, "a", history.KindAdmitted, ""); e.SessionID = ""; return e }(), "session identity required"},
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
	revised := func(mutate func(*history.Entry)) history.Entry {
		e := history.Entry{Author: history.AuthorOf(history.KindRevised), Kind: history.KindRevised, SessionID: "root", WorkUnitID: "plan", AttemptID: history.FirstAttempt,
			Cause: history.CauseNone, BaseVersion: "v1", PlanVersion: "v2", Classification: history.ClassificationTactical}
		mutate(&e)
		return e
	}
	for name, tc := range map[string]struct {
		entry history.Entry
		want  string
	}{
		"tactical":       {revised(func(*history.Entry) {}), ""},
		"strategic":      {revised(func(e *history.Entry) { e.Classification = history.ClassificationStrategic }), ""},
		"missing base":   {revised(func(e *history.Entry) { e.BaseVersion = "" }), "expected base version"},
		"missing result": {revised(func(e *history.Entry) { e.PlanVersion = "" }), "expected base version"},
		"unknown class":  {revised(func(e *history.Entry) { e.Classification = "cosmetic" }), "unknown revision classification"},
		"invalid w/o reason": {revised(func(e *history.Entry) {
			e.Kind, e.Author = history.KindInvalidRevision, history.AuthorOf(history.KindInvalidRevision)
		}), "records its reason"},
		"invalid with reason": {revised(func(e *history.Entry) {
			e.Kind, e.Author, e.Reason = history.KindInvalidRevision, history.AuthorOf(history.KindInvalidRevision), "cycle"
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
