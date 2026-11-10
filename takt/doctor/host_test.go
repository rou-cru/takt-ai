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
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/doctor"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/memory/embed"
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

// readyWait bounds how long a test waits for its in-process memory service to open its socket.
const readyWait = 10 * time.Second

// check is one parsed report line with its optional remedy.
type check struct {
	mark, detail, remedy string
}

// host is a machine doctor can inspect: its own HOME, a PATH holding only
// fake tools, a working directory, and a memory directory. Nothing leaks in
// from the machine running the tests.
type host struct {
	t         *testing.T
	home      string
	bin       string
	workspace string
	memoryDir string
}

// newHost builds a healthy machine: every tool installed and compatible, every
// plugin deployed, a manifest whose files all exist, and a ready memory service
// with its semantic model loaded.
func newHost(t *testing.T) *host {
	t.Helper()
	h := newHostWithoutMemory(t)
	testutil.WriteTinyModel(t, embed.ModelDir(h.home))
	if err := memory.Init(h.memoryDir); err != nil {
		t.Fatal(err)
	}
	h.serve()
	return h
}

// newHostWithoutMemory is newHost with nothing in the memory directory and no memory service.
func newHostWithoutMemory(t *testing.T) *host {
	t.Helper()
	h := &host{t: t, home: t.TempDir(), bin: t.TempDir(), workspace: t.TempDir(), memoryDir: testutil.MemoryDir(t)}
	t.Setenv("HOME", h.home)
	t.Setenv("PATH", h.bin)
	t.Chdir(h.workspace)

	h.tool("takt-ai", []byte("#!/bin/sh\n"))
	h.tool("codegraph", testutil.FakeCodegraphScript(codegraph.CodegraphVersion))
	h.tool("opencode", testutil.FakeOpenCodeScript())
	plugins := []string{model.VFSPluginFile, "takt-memory.ts", "takt-sandbox.mjs", "takt-dag/tui.tsx"}
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

// serve runs the memory service of the host's directory in this process until the test ends.
func (h *host) serve() {
	h.t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- memory.Serve(ctx, h.memoryDir, "test-version") }()
	h.t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			h.t.Error(err)
		}
	})
	h.waitForSocket(done)
}

func (h *host) waitForSocket(done <-chan error) {
	h.t.Helper()
	deadline := time.Now().Add(readyWait)
	for {
		if _, err := os.Stat(filepath.Join(h.memoryDir, memory.SocketFile)); err == nil {
			return
		}
		select {
		case err := <-done:
			h.t.Fatalf("Serve returned early: %v", err)
		default:
		}
		if time.Now().After(deadline) {
			h.t.Fatal("the memory socket never appeared")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// answerGarbage makes something that is not a memory service listen on the service's socket.
func (h *host) answerGarbage() {
	h.t.Helper()
	listener, err := net.Listen("unix", filepath.Join(h.memoryDir, memory.SocketFile))
	if err != nil {
		h.t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("not a memory service")) })}
	go func() { _ = server.Serve(listener) }()
	h.t.Cleanup(func() { _ = server.Close() })
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
