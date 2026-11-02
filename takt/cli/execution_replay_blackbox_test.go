// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package main_test

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPreparedAndSettledDispatchRejectsIdentityReuseBeforeReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("builds and drives the CLI in an isolated HOME")
	}
	bin := buildTaktAI(t)
	for _, settled := range []bool{false, true} {
		name := "prepared"
		if settled {
			name = "settled"
		}
		t.Run(name, func(t *testing.T) {
			s := newSession(t, bin)
			s.must(request{Action: "inputs", Session: rootSession, Event: "brief"})
			original := request{Action: "prepare", Session: rootSession, Event: "brief", Dispatch: "original-call", Caller: rootSession, Agent: "pm", Owner: "instance"}
			var prepared struct {
				Launchable bool `json:"launchable"`
			}
			s.must(original).decode(t, &prepared)
			if !prepared.Launchable {
				t.Fatal("first preparation was not launchable")
			}
			if settled {
				s.must(call("finish", "brief"))
			}
			count := len(s.entries())
			repeated := s.must(original)
			var replay struct {
				Launchable bool `json:"launchable"`
			}
			if err := json.Unmarshal([]byte(repeated.stdout), &replay); err != nil {
				t.Fatal(err)
			}
			if replay.Launchable {
				t.Fatal("transport replay could launch another execution")
			}
			for _, tc := range []struct {
				name   string
				mutate func(*request)
			}{
				{"root", func(r *request) { r.Session = "other-root" }},
				{"unit", func(r *request) { r.Event = "other-unit" }},
				{"caller", func(r *request) { r.Caller = "other-caller" }},
				{"agent", func(r *request) { r.Agent = "architect" }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					changed := original
					tc.mutate(&changed)
					if result := s.rejected(changed); !strings.Contains(result.stderr, "already identifies") {
						t.Fatalf("wrong refusal: %+v", result)
					}
				})
			}
			body, err := json.Marshal(struct {
				request
				Background bool `json:"background"`
			}{original, true})
			if err != nil {
				t.Fatal(err)
			}
			changedMode := s.run("dispatch", "--workspace", s.root, "--state", s.state, "--request", string(body))
			if changedMode.code == 0 || !strings.Contains(changedMode.stderr, "already identifies") {
				t.Fatalf("background identity mismatch: %+v", changedMode)
			}
			if len(s.entries()) != count {
				t.Fatal("replay or identity refusal changed history")
			}
		})
	}
}
