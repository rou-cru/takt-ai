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

package main_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/obs"
)

// obsIngest drives the public CLI entry once, as a separate invocation, and returns the assigned id.
func obsIngest(t *testing.T, bin, workspace, payload string) (int64, error) {
	t.Helper()
	var out, stderr bytes.Buffer
	cmd := exec.Command(bin, "obs", "ingest", "--workspace", workspace)
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	cmd.Stdin, cmd.Stdout, cmd.Stderr = strings.NewReader(payload), &out, &stderr
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	var resp struct {
		OK bool  `json:"ok"`
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil || !resp.OK {
		t.Fatalf("response %q: %v", out.String(), err)
	}
	return resp.ID, nil
}

func obsEvents(t *testing.T, workspace, session string) []obs.StoredEvent {
	t.Helper()
	store, err := obs.OpenStore(workspace)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	}()
	events, err := store.Events(session, "", 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestObsIngestPersistsAndKeepsOrderAcrossInvocations(t *testing.T) {
	bin := buildTaktAI(t)
	ws := t.TempDir()
	first, err := obsIngest(t, bin, ws, `{"ipc_version":1,"session_id":"s1","work_unit_id":"u1","attempt_id":"a1","agent":"orch","source":"takt.orchestration","event_class":"dispatch","attributes":{"decision":"dispatch","specialist":"dev","parallelism":2}}`)
	if err != nil {
		t.Fatal(err)
	}
	second, err := obsIngest(t, bin, ws, `{"ipc_version":1,"session_id":"s1","agent":"author","source":"takt.platform","event_class":"tool_activity","attributes":{"platform":"opencode","tool":"bash","phase":"end","duration_ms":12}}`)
	if err != nil {
		t.Fatal(err)
	}
	if second <= first {
		t.Fatalf("ids not increasing: %d then %d", first, second)
	}
	events := obsEvents(t, ws, "s1")
	if len(events) != 2 || events[0].ID != first || events[1].ID != second {
		t.Fatalf("stream = %+v", events)
	}
	e := events[0].Envelope
	if e.EventClass != obs.EventDispatch || e.Source != obs.PlaneOrchestration || e.Authority != "orch" ||
		e.Correlations.WorkUnitID != "u1" || e.Correlations.AttemptID != "a1" || e.Attributes["specialist"] != "dev" {
		t.Fatalf("first event = %+v", e)
	}
	if events[1].Envelope.EventClass != obs.EventToolActivity || events[1].Envelope.Source != obs.PlanePlatform {
		t.Fatalf("second event = %+v", events[1].Envelope)
	}
}

func TestObsIngestRejectsInvalidWithoutWriting(t *testing.T) {
	bin := buildTaktAI(t)
	ws := t.TempDir()
	base := func(class, source, attrs string) string {
		return `{"ipc_version":1,"session_id":"s1","agent":"a","source":"` + source + `","event_class":"` + class + `","attributes":` + attrs + `}`
	}
	cases := map[string]string{
		"vfs class":             base("vfs_delta", "takt.vfs", `{}`),
		"control class":         base("control_gate", "takt.control", `{"policy_ref":"p","triggering_condition":"c"}`),
		"source/class mismatch": base("dispatch", "takt.platform", `{}`),
		"unknown source":        base("dispatch", "unknown", `{}`),
		"unknown class":         base("unknown", "takt.platform", `{}`),
		"attribute not listed":  base("dispatch", "takt.orchestration", `{"decision":"x","note":"y"}`),
		"content attribute":     base("tool_activity", "takt.platform", `{"content":"secret text"}`),
		"nested attribute":      base("dispatch", "takt.orchestration", `{"decision":{"a":1}}`),
		"array attribute":       base("tool_activity", "takt.platform", `{"tool":["read"]}`),
		"null attribute":        base("tool_activity", "takt.platform", `{"tool":null}`),
		"unknown field":         `{"ipc_version":1,"session_id":"s1","agent":"a","source":"takt.orchestration","event_class":"dispatch","extra":1}`,
		"missing session_id":    `{"ipc_version":1,"agent":"a","source":"takt.orchestration","event_class":"dispatch"}`,
		"missing agent":         `{"ipc_version":1,"session_id":"s1","source":"takt.orchestration","event_class":"dispatch"}`,
		"wrong ipc_version":     `{"ipc_version":2,"session_id":"s1","agent":"a","source":"takt.orchestration","event_class":"dispatch"}`,
		"two values":            base("dispatch", "takt.orchestration", `{}`) + base("dispatch", "takt.orchestration", `{}`),
	}
	for name, payload := range cases {
		if _, err := obsIngest(t, bin, ws, payload); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for _, session := range []string{"s1", ""} {
		if n := len(obsEvents(t, ws, session)); n != 0 {
			t.Errorf("session %q: %d events written by rejected requests", session, n)
		}
	}
}
