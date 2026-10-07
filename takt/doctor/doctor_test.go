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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/doctor"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/setup"
)

// everyCheck is the report of a healthy machine, in the order doctor runs them.
var everyCheck = []string{
	"tool:takt-ai", "tool:opencode", "tool:engram", "control-plane:health",
	"deployment:manifest", "deployment:opencode",
	"engram:binary", "engram:version", "engram:reachable", "engram:native-plugin", "engram:diagnostics", "engram:needs-review",
	"codegraph:binary", "codegraph:version",
	"opencode:version", "opencode:vfs-plugin", "opencode:memory-plugin", "opencode:sandbox-adapter",
	"disk:space",
}

func TestRunHealthyMachinePassesEveryCheck(t *testing.T) {
	h := newHost(t)
	checks, out, err := h.run()
	if err != nil {
		t.Fatalf("Run() error = %v\n%s", err, out)
	}
	if len(checks) != len(everyCheck) {
		t.Errorf("report has %d checks, want %d:\n%s", len(checks), len(everyCheck), out)
	}
	for _, name := range everyCheck {
		expect(t, checks, name, passMark, "", "")
	}
	expect(t, checks, "deployment:manifest", passMark, "3 managed files deployed (opencode)", "")
	for _, want := range []string{"Summary: 19 passed, 0 failed, 0 warnings", "Status:  healthy"} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q:\n%s", want, out)
		}
	}
}

func TestRunReportsDegradedWhenOnlyWarnings(t *testing.T) {
	h := newHost(t)
	if err := os.Remove(model.OpenCodePluginPath(h.home, "takt-memory.ts")); err != nil {
		t.Fatal(err)
	}
	checks, out, err := h.run()
	if err != nil {
		t.Fatalf("Run() error = %v, want none for warnings only", err)
	}
	expect(t, checks, "opencode:memory-plugin", warnMark, "Memory writer not installed", "setup sync")
	if !strings.Contains(out, "Status:  degraded") {
		t.Errorf("report missing degraded status:\n%s", out)
	}
}

func TestRunFailsTheVerdictWhenACheckFails(t *testing.T) {
	h := newHost(t)
	if err := os.Remove(filepath.Join(h.bin, "opencode")); err != nil {
		t.Fatal(err)
	}
	checks, out, err := h.run()
	if !errors.Is(err, doctor.ErrUnhealthy) {
		t.Fatalf("Run() error = %v, want ErrUnhealthy", err)
	}
	expect(t, checks, "tool:opencode", failMark, "opencode not found in PATH", "Install opencode")
	if !strings.Contains(out, "Status:  unhealthy") {
		t.Errorf("report missing unhealthy status:\n%s", out)
	}
}

func TestRunToolsFlagShadowedCopiesOnce(t *testing.T) {
	h := newHost(t)
	other, third := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "opencode"), []byte("#!/bin/sh\n"), executableMode); err != nil {
		t.Fatal(err)
	}
	// A directory named like the tool is not a copy of it.
	if err := os.Mkdir(filepath.Join(third, "opencode"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The first directory twice, spelled differently, counts once.
	t.Setenv("PATH", strings.Join([]string{h.bin, other, third, h.bin + string(os.PathSeparator)}, string(os.PathListSeparator)))
	checks := h.mustRun()
	expect(t, checks, "tool:opencode", warnMark, "2 copies found in PATH: "+h.bin+", "+other, "Remove duplicate opencode")
	expect(t, checks, "tool:engram", passMark, "", "")
}

func TestRunDeploymentChecks(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, h *host)
		check func(t *testing.T, checks map[string]check)
	}{
		{"no manifest is expected on first use", func(t *testing.T, h *host) {
			if err := os.Remove(filepath.Join(h.home, setup.OwnershipManifestFilename)); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, checks map[string]check) {
			expect(t, checks, "deployment:manifest", warnMark, "no ownership manifest", "setup install")
		}},
		{"unreadable manifest fails", func(t *testing.T, h *host) {
			if err := os.WriteFile(filepath.Join(h.home, setup.OwnershipManifestFilename), []byte("{not json"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, checks map[string]check) {
			expect(t, checks, "deployment:manifest", failMark, "parse ownership manifest", "setup sync")
		}},
		{"missing files are counted per target", func(t *testing.T, h *host) {
			manifest := setup.NewOwnershipManifest()
			for _, item := range []struct {
				path   string
				target setup.OwnershipTarget
				exists bool
			}{
				{"opencode/present.md", setup.TargetOpenCode, true},
				{"opencode/gone.md", setup.TargetOpenCode, false},
				{"skills/x/SKILL.md", setup.TargetSkills, true},
			} {
				entry, err := setup.NewOwnershipEntry(item.path, []byte("x"), 0o644, false, "", "", item.target)
				if err != nil {
					t.Fatal(err)
				}
				if err := manifest.Add(entry); err != nil {
					t.Fatal(err)
				}
				if item.exists {
					path := filepath.Join(h.home, filepath.FromSlash(item.path))
					if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := manifest.Save(h.home); err != nil {
				t.Fatal(err)
			}
		}, func(t *testing.T, checks map[string]check) {
			expect(t, checks, "deployment:manifest", warnMark, "1 of 3 managed files missing", "setup sync")
			expect(t, checks, "deployment:opencode", warnMark, "1 of 2 opencode files missing", "setup sync")
			expect(t, checks, "deployment:skills", passMark, "1 skills files deployed", "")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			tc.setup(t, h)
			tc.check(t, h.mustRun())
		})
	}
}

func TestRunBinaryChecks(t *testing.T) {
	tests := []struct {
		name                       string
		engramScript, codegraphOld string
		engramDetail, codegraphDet string
	}{
		{"binaries that fail their version command", "#!/bin/sh\nexit 1\n", "#!/bin/sh\nexit 1\n", ".takt-ai/bin/engram", ".takt-ai/codegraph"},
		{"binaries older than the pinned version", "#!/bin/sh\necho 'engram 0.0.1'\n", "#!/bin/sh\necho 0.0.1\n", "setup sync", "setup sync"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			h.tool("engram", []byte(tc.engramScript))
			h.tool("codegraph", []byte(tc.codegraphOld))
			checks, _, err := h.run()
			if !errors.Is(err, doctor.ErrUnhealthy) {
				t.Fatalf("Run() error = %v, want ErrUnhealthy", err)
			}
			expect(t, checks, "engram:binary", failMark, tc.engramDetail, "")
			expect(t, checks, "codegraph:binary", failMark, tc.codegraphDet, "")
			for _, skipped := range []string{"engram:version", "engram:reachable", "codegraph:version"} {
				if _, found := checks[skipped]; found {
					t.Errorf("%s reported although its binary is unusable", skipped)
				}
			}
		})
	}
}

func TestRunEngramReachability(t *testing.T) {
	h := newHost(t)
	h.answer("/health", 503, "down")
	expect(t, h.mustRun(), "engram:reachable", warnMark, "returned HTTP 503", "")

	h.server.Close()
	expect(t, h.mustRun(), "engram:reachable", failMark, "unreachable at "+h.server.URL+"/health", "Start engram")
}

func TestRunEngramAddress(t *testing.T) {
	t.Run("defaults to the local server", func(t *testing.T) {
		h := newHost(t)
		t.Setenv(model.EnvEngramURL, "")
		got := h.mustRun()["engram:reachable"]
		if !strings.Contains(got.detail, model.DefaultEngramURL+"/health") {
			t.Errorf("engram:reachable = %+v, want it to probe %s/health", got, model.DefaultEngramURL)
		}
	})
	t.Run("an address that is not a URL fails the probe", func(t *testing.T) {
		h := newHost(t)
		t.Setenv(model.EnvEngramURL, "://bad")
		expect(t, h.mustRun(), "engram:reachable", failMark, "missing protocol scheme", "Start engram")
	})
}

func TestRunWarnsAboutEngramsOwnPlugin(t *testing.T) {
	h := newHost(t)
	h.plugin(model.EngramPluginFile)
	expect(t, h.mustRun(), "engram:native-plugin", warnMark, "second memory protocol", "plugins/engram.ts")
}

func TestRunEngramReports(t *testing.T) {
	issues := `{"summary":{"total":6,"ok":2,"warnings":2,"blocked":1,"errors":1},"checks":[
		{"check_id":"fine","result":"ok","message":"all good"},
		{"check_id":"c1","result":"warning","message":"m1","safe_next_step":"fix c1"},
		{"check_id":"c2","result":"blocked","message":"m2","safe_next_step":"fix c2"},
		{"check_id":"c3","result":"error","message":"m3"},
		{"check_id":"c4","result":"warning","message":"m4"}]}`
	due := func(count int) string {
		var items []string
		for i := 1; i <= count; i++ {
			items = append(items, fmt.Sprintf(`{"id":%d,"title":"T%d","type":"decision"}`, i, i))
		}
		return fmt.Sprintf(`{"observations":[%s],"count":%d}`, strings.Join(items, ","), count)
	}
	type answer struct {
		status       int
		body         string
		mark, detail string
		remedy       string
	}
	// Each row serves one answer per endpoint, so one run covers both checks.
	tests := []struct {
		name        string
		doctor, rev answer
	}{
		{"clean",
			answer{200, `{"summary":{"total":3,"ok":3},"checks":[]}`, passMark, "3 engram diagnostic check(s), all clean", ""},
			answer{200, `{"observations":[],"count":0}`, passMark, "no observations are due for review", ""}},
		{"project without history",
			answer{404, `{"code":"unknown_project","available_projects":["a","b"]}`, passMark, `project "takt-fixture" has no Engram history yet (known: a, b)`, ""},
			answer{404, `{"code":"unknown_project"}`, passMark, `project "takt-fixture" has no Engram history yet`, ""}},
		{"issues and observations due",
			answer{200, issues, warnMark, "2 ok, 2 warning, 1 blocked, 1 error across 6 check(s) — c1: m1; c2: m2; c3: m3", "fix c1"},
			answer{200, due(2), warnMark, `2 observation(s) due for review: #1 "T1" (decision); #2 "T2" (decision)`, "memory_record"}},
		{"server errors",
			answer{500, `{"error":"boom"}`, warnMark, "returned HTTP 500: boom", ""},
			answer{200, due(5), warnMark, "5 observation(s) due for review (showing first 5; more may exist)", "memory_record"}},
		{"plain-text error",
			answer{404, "nope", warnMark, "returned HTTP 404: nope", ""},
			answer{500, `{"error":"boom"}`, warnMark, "returned HTTP 500: boom", ""}},
		{"bodies of another shape",
			answer{200, "not json", warnMark, "could not parse engram /doctor response", ""},
			answer{200, "not json", warnMark, "could not parse engram /review response", ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			h.answer("/doctor", tc.doctor.status, tc.doctor.body)
			h.answer("/review", tc.rev.status, tc.rev.body)
			checks := h.mustRun()
			expect(t, checks, "engram:diagnostics", tc.doctor.mark, tc.doctor.detail, tc.doctor.remedy)
			expect(t, checks, "engram:needs-review", tc.rev.mark, tc.rev.detail, tc.rev.remedy)
			if strings.Contains(checks["engram:diagnostics"].detail, "c4") {
				t.Errorf("fourth issue listed: %s", checks["engram:diagnostics"].detail)
			}
			if got := h.requests("/doctor"); len(got) != 1 || got[0] != "/doctor?project=takt-fixture" {
				t.Errorf("diagnostics requests = %v, want one for the project Engram detects for the working directory", got)
			}
			if got := h.requests("/review"); len(got) != 1 || got[0] != "/review?project=takt-fixture&limit=5" {
				t.Errorf("review requests = %v, want one limited to 5 for the detected project", got)
			}
		})
	}

	t.Run("unreachable server", func(t *testing.T) {
		h := newHost(t)
		h.server.Close()
		checks := h.mustRun()
		expect(t, checks, "engram:diagnostics", warnMark, "unreachable", "")
		expect(t, checks, "engram:needs-review", warnMark, "unreachable", "")
	})
}

func TestRunOpenCodeVersion(t *testing.T) {
	t.Run("older than V2 fails", func(t *testing.T) {
		h := newHost(t)
		h.tool("opencode", []byte("#!/bin/sh\n[ \"$*\" = 'api GET /api/info' ] && printf '%s' '{\"version\":\"1.9.0\"}' || exit 1\n"))
		checks, _, err := h.run()
		if !errors.Is(err, doctor.ErrUnhealthy) {
			t.Fatalf("Run() error = %v, want ErrUnhealthy", err)
		}
		expect(t, checks, "opencode:version", failMark, "needs OpenCode v2 or newer", "Upgrade OpenCode")
	})
	t.Run("a broken API only warns", func(t *testing.T) {
		h := newHost(t)
		h.tool("opencode", []byte("#!/bin/sh\nexit 1\n"))
		expect(t, h.mustRun(), "opencode:version", warnMark, "handshake failed", "opencode api GET /api/info")
	})
}

func TestRunOpenCodePlugins(t *testing.T) {
	plugins := map[string]string{
		"opencode:vfs-plugin":      model.VFSPluginFile,
		"opencode:memory-plugin":   "takt-memory.ts",
		"opencode:sandbox-adapter": "takt-sandbox.mjs",
	}
	t.Run("missing", func(t *testing.T) {
		h := newHost(t)
		for _, file := range plugins {
			if err := os.Remove(model.OpenCodePluginPath(h.home, file)); err != nil {
				t.Fatal(err)
			}
		}
		checks := h.mustRun()
		for name := range plugins {
			expect(t, checks, name, warnMark, "not installed", "setup sync")
		}
	})
	t.Run("replaced by a directory", func(t *testing.T) {
		h := newHost(t)
		for _, file := range plugins {
			path := model.OpenCodePluginPath(h.home, file)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}
		checks := h.mustRun()
		for name := range plugins {
			expect(t, checks, name, failMark, "is not a regular file", "setup sync")
		}
	})
}

func TestRunControlPlane(t *testing.T) {
	record := func(t *testing.T, workspace string) {
		t.Helper()
		store, err := obs.OpenStore(workspace)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := store.Close(); err != nil {
				t.Errorf("close store: %v", err)
			}
		}()
		e := obs.NewEnvelope(obs.NewClock(), obs.PlaneVFS, "agent-a")
		e.EventClass = obs.EventVFSDelta
		e.Correlations.SessionID = "s1"
		if _, err := store.AppendEvent(e); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name         string
		prepare      func(t *testing.T, workspace string)
		mark, detail string
		remedy       string
	}{
		{"no store yet passes", func(*testing.T, string) {}, passMark, "no event store in this workspace yet", ""},
		{"recorded events pass", record, passMark, "1 recorded", ""},
		{"empty store passes", func(t *testing.T, workspace string) {
			store, err := obs.OpenStore(workspace)
			if err != nil {
				t.Fatal(err)
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
		}, passMark, "no events recorded", ""},
		{"store readable by others warns", func(t *testing.T, workspace string) {
			record(t, workspace)
			path, err := obs.StorePath(workspace)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, 0o644); err != nil {
				t.Fatal(err)
			}
		}, warnMark, "readable by other users", "chmod 600"},
		{"corrupt store fails", func(t *testing.T, workspace string) {
			path, err := obs.StorePath(workspace)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Dir(path), obs.PrivateTelemetryDirectoryMode); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("this is not a sqlite database, not even close to one"), 0o600); err != nil {
				t.Fatal(err)
			}
		}, failMark, "event store is unusable", "Move"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			tc.prepare(t, h.workspace)
			expect(t, h.mustRun(), "control-plane:health", tc.mark, tc.detail, tc.remedy)
		})
	}
}

func TestRunNeverCreatesWorkspaceState(t *testing.T) {
	h := newHost(t)
	h.mustRun()
	if _, err := os.Stat(filepath.Join(h.workspace, obs.StateDirName)); !os.IsNotExist(err) {
		t.Errorf("doctor created %s in the workspace: stat error = %v", obs.StateDirName, err)
	}
}

func TestRunDiskSpace(t *testing.T) {
	h := newHost(t)
	if err := os.Remove(filepath.Join(h.home, ".takt-ai")); err != nil {
		t.Fatal(err)
	}
	expect(t, h.mustRun(), "disk:space", warnMark, "could not determine free disk space", "")
}

func TestClassifyFreeSpace(t *testing.T) {
	const mb = 1024 * 1024
	tests := []struct {
		free   uint64
		mark   string
		detail string
	}{
		{0, failMark, "critically low disk space: 0 MB free"},
		{10*mb - 1, failMark, "critically low disk space: 9 MB free"},
		{10 * mb, warnMark, "low disk space: 10 MB free"},
		{100*mb - 1, warnMark, "low disk space: 99 MB free"},
		{100 * mb, passMark, "100 MB free on /work filesystem"},
		{1024 * mb, passMark, "1024 MB free on /work filesystem"},
	}
	marks := map[doctor.CheckStatus]string{doctor.CheckStatusPass: passMark, doctor.CheckStatusWarn: warnMark, doctor.CheckStatusFail: failMark}
	for _, tc := range tests {
		got := doctor.ClassifyFreeSpace("/work", tc.free)
		if got.Name != "disk:space" || marks[got.Status] != tc.mark || !strings.Contains(got.Detail, tc.detail) {
			t.Errorf("ClassifyFreeSpace(%d) = %+v, want %s with %q", tc.free, got, tc.mark, tc.detail)
		}
	}
}

func TestRunReportsUnresolvableHome(t *testing.T) {
	newHost(t)
	t.Setenv("HOME", "")
	var out strings.Builder
	err := doctor.Run(&out)
	if err == nil || !strings.Contains(err.Error(), "resolve home directory") {
		t.Fatalf("Run() error = %v, want the home directory failure", err)
	}
	if out.Len() != 0 {
		t.Errorf("Run() wrote a report despite failing: %q", out.String())
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRunReportsWriteFailure(t *testing.T) {
	newHost(t)
	if err := doctor.Run(failingWriter{}); err == nil || !strings.Contains(err.Error(), "write failed") {
		t.Fatalf("Run() error = %v, want the write failure", err)
	}
}
