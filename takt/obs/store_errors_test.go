// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.

package obs_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/obs"
)

const (
	testSession     = "s1"
	notADatabase    = "this is not a sqlite database, only text padding the header check"
	corruptAttrs    = "{not json"
	wallStampNanos  = int64(1_700_000_000_000_000_000)
	eventsPageLimit = 2
)

func TestStorePathIsAbsoluteInStateDir(t *testing.T) {
	rel, err := obs.StorePath("some/workspace")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(rel) || !strings.HasSuffix(rel, filepath.Join("some", "workspace", obs.StateDirName, "events.db")) {
		t.Fatalf("StorePath = %q; want an absolute path ending in the state dir", rel)
	}
}

func TestOpenStoreFailsWhenStateDirCannotBeCreated(t *testing.T) {
	ws := t.TempDir()
	// A regular file where the state directory must live.
	if err := os.WriteFile(filepath.Join(ws, obs.StateDirName), []byte("x"), obs.PrivateTelemetryFileMode); err != nil {
		t.Fatal(err)
	}
	if s, err := obs.OpenStore(ws); err == nil {
		_ = s.Close()
		t.Fatal("OpenStore succeeded with a file in place of the state directory")
	}
}

func TestOpenStoreFailsWhenDatabaseFileCannotBeOpened(t *testing.T) {
	ws := t.TempDir()
	path, err := obs.StorePath(ws)
	if err != nil {
		t.Fatal(err)
	}
	// A directory where the database file must live.
	if err := os.MkdirAll(path, obs.PrivateTelemetryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if s, err := obs.OpenStore(ws); err == nil {
		_ = s.Close()
		t.Fatal("OpenStore succeeded with a directory in place of the database")
	}
}

func TestOpenStoreRejectsForeignDatabaseFile(t *testing.T) {
	ws := t.TempDir()
	path, err := obs.StorePath(ws)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), obs.PrivateTelemetryDirectoryMode); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat(notADatabase, 100)), obs.PrivateTelemetryFileMode); err != nil {
		t.Fatal(err)
	}
	if s, err := obs.OpenStore(ws); err == nil {
		_ = s.Close()
		t.Fatal("OpenStore accepted a file that is not a database")
	}
}

func TestOpenStoreCreatesOwnerOnlyDatabase(t *testing.T) {
	ws := t.TempDir()
	openStore(t, ws)
	path, _ := obs.StorePath(ws)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != obs.PrivateTelemetryFileMode {
		t.Fatalf("database mode = %v; want %v", info.Mode().Perm(), obs.PrivateTelemetryFileMode)
	}
}

func TestStoreRequiresSessionID(t *testing.T) {
	s := openStore(t, t.TempDir())
	e := newEnv(obs.NewClock(), obs.PlaneVFS, "agent-a")
	e.Correlations.SessionID = ""
	if _, err := s.AppendEvent(e); err == nil || !strings.Contains(err.Error(), "SessionID is required") {
		t.Fatalf("AppendEvent without session: %v", err)
	}
	r := obs.ControlRecord{ActionClass: obs.ActionGate, TriggeringCondition: "c", PolicyRef: "p", ActingAgent: "a"}
	if _, err := s.AppendAction(r); err == nil || !strings.Contains(err.Error(), "SessionID is required") {
		t.Fatalf("AppendAction without session: %v", err)
	}
}

func TestStoreRejectsInvalidControlRecord(t *testing.T) {
	s := openStore(t, t.TempDir())
	r := obs.ControlRecord{ActionClass: "bogus", TriggeringCondition: "c", PolicyRef: "p", ActingAgent: "a",
		Correlations: obs.CorrelationIDs{SessionID: testSession}}
	if _, err := s.AppendAction(r); err == nil {
		t.Fatal("AppendAction accepted an unknown action class")
	}
	r.ActionClass, r.PolicyRef = obs.ActionGate, ""
	if _, err := s.AppendAction(r); err == nil {
		t.Fatal("AppendAction accepted a record without a policy reference")
	}
}

func TestStoreEventsRoundTripWallTimestampAndBounds(t *testing.T) {
	s := openStore(t, t.TempDir())
	clock := obs.NewClock()
	var ids []int64
	for i := 0; i < 4; i++ {
		e := newEnv(clock, obs.PlaneVFS, "agent-a")
		e.Correlations.SessionID = testSession
		if i == 0 {
			e.WallTimestamp = time.Unix(0, wallStampNanos)
		} else {
			e.WallTimestamp = time.Time{}
		}
		id, err := s.AppendEvent(e)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	all, err := s.Events(testSession, "", 0, 0, 0)
	if err != nil || len(all) != len(ids) {
		t.Fatalf("Events = %d, %v; want %d", len(all), err, len(ids))
	}
	if !all[0].Envelope.WallTimestamp.Equal(time.Unix(0, wallStampNanos)) {
		t.Errorf("wall timestamp = %v; want it preserved", all[0].Envelope.WallTimestamp)
	}
	if !all[1].Envelope.WallTimestamp.IsZero() {
		t.Errorf("unset wall timestamp = %v; want zero", all[1].Envelope.WallTimestamp)
	}
	// (after, before] with a limit: the upper bound is inclusive, the lower exclusive.
	got, err := s.Events(testSession, "", ids[0], ids[2], eventsPageLimit)
	if err != nil || len(got) != eventsPageLimit || got[0].ID != ids[1] || got[1].ID != ids[2] {
		t.Fatalf("bounded Events = %+v, %v; want ids %d and %d", got, err, ids[1], ids[2])
	}
}

func TestStoreReadsFailOnCorruptAttributes(t *testing.T) {
	ws := t.TempDir()
	s := openStore(t, ws)
	e := newEnv(obs.NewClock(), obs.PlaneVFS, "agent-a")
	e.Correlations.SessionID = testSession
	id, err := s.AppendEvent(e)
	if err != nil {
		t.Fatal(err)
	}
	path, _ := obs.StorePath(ws)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Errorf("close database: %v", err)
		}
	}()
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE events SET attributes=? WHERE id=?", corruptAttrs, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Events(testSession, "", 0, 0, 0); err == nil {
		t.Fatal("Events decoded corrupt attributes without error")
	}
}

func TestStoreOperationsFailAfterClose(t *testing.T) {
	s, err := obs.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	e := newEnv(obs.NewClock(), obs.PlaneVFS, "agent-a")
	e.Correlations.SessionID = testSession
	r := obs.ControlRecord{ActionClass: obs.ActionGate, TriggeringCondition: "c", PolicyRef: "p", ActingAgent: "a",
		Correlations: obs.CorrelationIDs{SessionID: testSession}}
	if _, err := s.AppendEvent(e); err == nil {
		t.Error("AppendEvent succeeded on a closed store")
	}
	if _, err := s.AppendAction(r); err == nil {
		t.Error("AppendAction succeeded on a closed store")
	}
	if _, err := s.Events(testSession, "", 0, 0, 0); err == nil {
		t.Error("Events succeeded on a closed store")
	}
	if _, err := s.CountEvents(testSession, "", 0, 0); err == nil {
		t.Error("CountEvents succeeded on a closed store")
	}
}
