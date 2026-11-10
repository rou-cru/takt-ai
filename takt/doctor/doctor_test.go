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
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/doctor"
	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui/testutil"

	_ "modernc.org/sqlite"
)

// everyCheck is the report of a healthy machine, in the order doctor runs them.
var everyCheck = []string{
	"tool:takt-ai", "tool:opencode", "control-plane:health",
	"deployment:manifest", "deployment:opencode",
	"memory:service",
	"codegraph:binary", "codegraph:version",
	"opencode:version", "opencode:vfs-plugin", "opencode:memory-plugin", "opencode:sandbox-adapter", "opencode:dag-plugin",
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
	expect(t, checks, "deployment:manifest", passMark, "4 managed files deployed (opencode)", "")
	for _, want := range []string{"Summary: 14 passed, 0 failed, 0 warnings", "Status:  healthy"} {
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
	expect(t, checks, "opencode:memory-plugin", warnMark, "Memory binding not installed", "setup sync")
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
	expect(t, checks, "tool:takt-ai", passMark, "", "")
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
		name, script, detail string
	}{
		{"a binary that fails its version command", "#!/bin/sh\nexit 1\n", ".takt-ai/codegraph"},
		{"a binary older than the pinned version", "#!/bin/sh\necho 0.0.1\n", "setup sync"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(t)
			h.tool("codegraph", []byte(tc.script))
			checks, _, err := h.run()
			if !errors.Is(err, doctor.ErrUnhealthy) {
				t.Fatalf("Run() error = %v, want ErrUnhealthy", err)
			}
			expect(t, checks, "codegraph:binary", failMark, tc.detail, "")
			if _, found := checks["codegraph:version"]; found {
				t.Error("codegraph:version reported although its binary is unusable")
			}
		})
	}
}

func TestRunMemoryServiceStates(t *testing.T) {
	const name = "memory:service"
	t.Run("ready with its model loaded passes", func(t *testing.T) {
		expect(t, newHost(t).mustRun(), name, passMark, "test-version is ready", "")
	})
	t.Run("not ready without a store", func(t *testing.T) {
		h := newHostWithoutMemory(t)
		h.serve()
		expect(t, h.mustRun(), name, failMark, "not ready (no_store)", "memory start --init")
	})
	t.Run("not ready with a corrupt store", func(t *testing.T) {
		h := newHostWithoutMemory(t)
		if err := os.WriteFile(filepath.Join(h.memoryDir, memory.StoreFile), []byte("not a database, just text padding the file"), 0o600); err != nil {
			t.Fatal(err)
		}
		h.serve()
		expect(t, h.mustRun(), name, failMark, "not ready (corrupt)", "")
	})
	t.Run("a newer store is its own outcome", func(t *testing.T) {
		h := newHostWithoutMemory(t)
		if err := memory.Init(h.memoryDir); err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("sqlite", filepath.Join(h.memoryDir, memory.StoreFile))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("PRAGMA user_version = 999"); err != nil {
			t.Fatal(err)
		}
		_ = db.Close()
		h.serve()
		expect(t, h.mustRun(), name, failMark, "newer than this Takt build", "Update Takt")
	})
	t.Run("unreachable", func(t *testing.T) {
		h := newHostWithoutMemory(t)
		expect(t, h.mustRun(), name, failMark, "no memory service answers in "+h.memoryDir, "memory start")
	})
	t.Run("malformed response", func(t *testing.T) {
		h := newHostWithoutMemory(t)
		h.answerGarbage()
		expect(t, h.mustRun(), name, failMark, "malformed response", "memory start")
	})
	t.Run("entries without a semantic vector warn", func(t *testing.T) {
		h := newHostWithoutMemory(t)
		if err := memory.Init(h.memoryDir); err != nil {
			t.Fatal(err)
		}
		store, err := memory.Open(h.memoryDir)
		if err != nil {
			t.Fatal(err)
		}
		external := memory.Identity{Principal: memory.PrincipalExternal, HostSession: "k1", Workspace: "ws"}
		_, err = store.Record(context.Background(), external, memory.RecordArgs{Nature: memory.NatureObservation, Scope: memory.ScopeWorkspace, Title: "a", Content: "body", Evidence: "seen"})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		h.serve()
		expect(t, h.mustRun(), name, warnMark, "1 of 1 entries have no semantic vector", "")
	})
}

func TestRunNeverStartsTheMemoryService(t *testing.T) {
	h := newHostWithoutMemory(t)
	h.mustRun()
	if _, err := os.Stat(filepath.Join(h.memoryDir, memory.SocketFile)); !os.IsNotExist(err) {
		t.Errorf("doctor started a memory service: stat error = %v", err)
	}
}

// openCodeAt is a fake OpenCode that reports version.
func openCodeAt(version string) []byte {
	return bytes.ReplaceAll(testutil.FakeOpenCodeScript(), []byte(opencodeapi.MinimumVersion), []byte(version))
}

func TestRunOpenCodeVersion(t *testing.T) {
	older := []string{"1.9.0", "2.0.3"}
	for _, version := range older {
		t.Run("below the minimum fails: "+version, func(t *testing.T) {
			h := newHost(t)
			h.tool("opencode", openCodeAt(version))
			checks, _, err := h.run()
			if !errors.Is(err, doctor.ErrUnhealthy) {
				t.Fatalf("Run() error = %v, want ErrUnhealthy", err)
			}
			expect(t, checks, "opencode:version", failMark, "needs OpenCode "+opencodeapi.MinimumVersion+" or newer", "Upgrade OpenCode")
		})
	}
	for _, version := range []string{opencodeapi.MinimumVersion, "2.2.6", "3.0.0"} {
		t.Run("at or above the minimum passes: "+version, func(t *testing.T) {
			h := newHost(t)
			h.tool("opencode", openCodeAt(version))
			expect(t, h.mustRun(), "opencode:version", passMark, "OpenCode "+version, "")
		})
	}
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
		"opencode:dag-plugin":      "takt-dag/tui.tsx",
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
	h := newHostWithoutMemory(t)
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
