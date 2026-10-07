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

package doctor_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/doctor"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"
)

// Status markers of the report, which is doctor's public output.
const (
	passMark = "[ok]"
	warnMark = "[!!]"
	failMark = "[xx]"
)

// executableMode makes the fake tools runnable.
const executableMode = 0o755

// check is one parsed report line with its optional remedy.
type check struct {
	mark, detail, remedy string
}

// route is one canned answer of the Engram stub.
type route struct {
	status int
	body   string
}

// host is a machine doctor can inspect: its own HOME, a PATH holding only
// fake tools, a working directory, and a stub Engram server. Nothing leaks in
// from the machine running the tests.
type host struct {
	t         *testing.T
	home      string
	bin       string
	workspace string

	server *httptest.Server
	mu     sync.Mutex
	routes map[string]route
	seen   []string
}

// newHost builds a healthy machine: every tool installed and compatible, every
// plugin deployed, a manifest whose files all exist, Engram answering cleanly.
func newHost(t *testing.T) *host {
	t.Helper()
	h := &host{t: t, home: t.TempDir(), bin: t.TempDir(), workspace: t.TempDir(), routes: map[string]route{
		"/health": {http.StatusOK, "ok"},
		"/doctor": {http.StatusOK, `{"summary":{"total":3,"ok":3},"checks":[]}`},
		"/review": {http.StatusOK, `{"observations":[],"count":0}`},
	}}
	t.Setenv("HOME", h.home)
	t.Setenv("PATH", h.bin)
	t.Chdir(h.workspace)
	h.server = httptest.NewServer(http.HandlerFunc(h.serve))
	t.Cleanup(h.server.Close)
	t.Setenv(model.EnvEngramURL, h.server.URL)

	h.tool("takt-ai", []byte("#!/bin/sh\n"))
	h.tool("engram", testutil.FakeEngramScript(engram.EngramVersion))
	h.tool("codegraph", testutil.FakeCodegraphScript(codegraph.CodegraphVersion))
	h.tool("opencode", testutil.FakeOpenCodeScript())
	plugins := []string{model.VFSPluginFile, "takt-memory.ts", "takt-sandbox.mjs"}
	manifest := setup.NewOwnershipManifest()
	for _, file := range plugins {
		h.plugin(file)
		entry, err := setup.NewOwnershipEntry(".config/opencode/plugins/"+file, []byte("plugin"), 0o644, false, "", "", setup.TargetOpenCode)
		if err != nil {
			t.Fatal(err)
		}
		if err := manifest.Add(entry); err != nil {
			t.Fatal(err)
		}
	}
	if err := manifest.Save(h.home); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(h.home, ".takt-ai"), 0o755); err != nil {
		t.Fatal(err)
	}
	return h
}

// serve answers like Engram: canned routes by path, and the project Engram
// detects for the caller's directory.
func (h *host) serve(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	h.seen = append(h.seen, r.URL.RequestURI())
	answer, ok := h.routes[r.URL.Path]
	h.mu.Unlock()
	if r.URL.Path == "/project/current" {
		_ = json.NewEncoder(w).Encode(map[string]string{"project": "takt-fixture", "cwd": r.URL.Query().Get("cwd")})
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(answer.status)
	_, _ = w.Write([]byte(answer.body))
}

// answer replaces what the Engram stub says for one path.
func (h *host) answer(path string, status int, body string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.routes[path] = route{status, body}
}

// requests lists what the Engram stub received for one path, in order.
func (h *host) requests(path string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []string
	for _, uri := range h.seen {
		if strings.HasPrefix(uri, path) {
			out = append(out, uri)
		}
	}
	return out
}

// tool installs an executable on PATH, replacing any earlier one.
func (h *host) tool(name string, script []byte) {
	h.t.Helper()
	if err := os.WriteFile(filepath.Join(h.bin, name), script, executableMode); err != nil {
		h.t.Fatal(err)
	}
}

// plugin deploys a file under OpenCode's plugins directory.
func (h *host) plugin(file string) {
	h.t.Helper()
	path := model.OpenCodePluginPath(h.home, file)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("plugin"), 0o644); err != nil {
		h.t.Fatal(err)
	}
}

// run executes doctor and parses what it printed.
func (h *host) run() (map[string]check, string, error) {
	h.t.Helper()
	var out bytes.Buffer
	err := doctor.Run(&out)
	return parse(out.String()), out.String(), err
}

// mustRun runs doctor, tolerating only a verdict of unhealthy.
func (h *host) mustRun() map[string]check {
	h.t.Helper()
	checks, _, err := h.run()
	if err != nil && !errors.Is(err, doctor.ErrUnhealthy) {
		h.t.Fatalf("Run() error = %v", err)
	}
	return checks
}

// parse reads the report's check lines: "  [ok]  name  detail", with an
// optional "       Remedy: ..." line below.
func parse(out string) map[string]check {
	checks := map[string]check{}
	var last string
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "  ["):
			rest := line[8:]
			fields := strings.Fields(rest)
			last = fields[0]
			checks[last] = check{mark: line[2:6], detail: strings.TrimSpace(strings.TrimPrefix(rest, last))}
		case strings.HasPrefix(line, "       Remedy: ") && last != "":
			entry := checks[last]
			entry.remedy = strings.TrimPrefix(line, "       Remedy: ")
			checks[last] = entry
		}
	}
	return checks
}

// expect asserts one check's mark and that its detail and remedy contain the given text.
func expect(t *testing.T, checks map[string]check, name, mark, detail, remedy string) {
	t.Helper()
	got, ok := checks[name]
	if !ok {
		t.Fatalf("report has no %q check; got %v", name, checks)
	}
	if got.mark != mark || !strings.Contains(got.detail, detail) || !strings.Contains(got.remedy, remedy) {
		t.Errorf("%s = %+v, want %s with detail containing %q and remedy containing %q", name, got, mark, detail, remedy)
	}
}
