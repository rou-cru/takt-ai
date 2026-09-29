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

package doctor

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

func TestDeploymentChecksReportsGlobalAndPerTargetMissingFiles(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "present.txt"), []byte("managed"), 0o644); err != nil {
		t.Fatal(err)
	}
	present, err := setup.NewOwnershipEntry("present.txt", []byte("managed"), 0o644, false, "", "", setup.TargetOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	missing, err := setup.NewOwnershipEntry("missing.txt", []byte("expected"), 0o644, false, "", "", setup.TargetSkills)
	if err != nil {
		t.Fatal(err)
	}
	manifest := setup.NewOwnershipManifest()
	if err := manifest.Add(present, missing); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(home); err != nil {
		t.Fatal(err)
	}

	checks := deploymentChecks(home)
	if len(checks) != 3 {
		t.Fatalf("deploymentChecks() returned %d checks, want global and two targets: %#v", len(checks), checks)
	}
	if checks[0].Name != "deployment:manifest" || checks[0].Status != CheckStatusWarn || checks[0].Detail != "1 of 2 managed files missing" {
		t.Fatalf("global deployment check = %#v", checks[0])
	}
	if checks[1].Name != "deployment:opencode" || checks[1].Status != CheckStatusPass {
		t.Fatalf("OpenCode deployment check = %#v", checks[1])
	}
	if checks[2].Name != "deployment:skills" || checks[2].Status != CheckStatusWarn {
		t.Fatalf("skills deployment check = %#v", checks[2])
	}
}

func TestDefaultHTTPGetAndStatfsFreeBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		_, _ = w.Write([]byte("health"))
	}))
	t.Cleanup(server.Close)

	status, err := defaultHTTPGet(server.URL, time.Second)
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("defaultHTTPGet() = (%d, %v), want (%d, nil)", status, err, http.StatusNoContent)
	}
	if _, err := defaultHTTPGet("://invalid", time.Second); err == nil {
		t.Fatal("defaultHTTPGet(invalid URL) returned no error")
	}

	available, err := statfsFreeBytes(t.TempDir())
	if err != nil || available == 0 {
		t.Fatalf("statfsFreeBytes(tempdir) = (%d, %v), want positive bytes and nil error", available, err)
	}
	if _, err := statfsFreeBytes(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("statfsFreeBytes(missing path) returned no error")
	}
}

// withSeams replaces every doctor seam with deterministic doubles for the
// duration of the test and restores the originals via t.Cleanup. Nil
// arguments install passing defaults; an empty home installs a fresh temp dir.
func withSeams(t *testing.T, home string, look func(string) (string, error), copies func(string) []string, get func(string, time.Duration) (int, error), free func(string) (uint64, error)) {
	t.Helper()
	origHome, origLook, origCopies, origGet, origFree, origVersion, origResolve, origControl := userHomeDir, lookPath, toolCopies, httpGet, diskFree, engramVersionFn, resolveEngram, controlPlaneHealth
	origCodegraphResolve, origCodegraphVersion, origHandshake := resolveCodegraph, codegraphVersionFn, openCodeHandshake
	origFetch, origWorkingDir, origResolveProject := httpFetch, workingDir, resolveProject
	t.Cleanup(func() {
		resolveCodegraph, codegraphVersionFn, openCodeHandshake = origCodegraphResolve, origCodegraphVersion, origHandshake
		userHomeDir, lookPath, toolCopies, httpGet, diskFree, engramVersionFn, resolveEngram, controlPlaneHealth = origHome, origLook, origCopies, origGet, origFree, origVersion, origResolve, origControl
		httpFetch, workingDir, resolveProject = origFetch, origWorkingDir, origResolveProject
	})
	// engram:diagnostics/engram:needs-review default to the empty-body,
	// zero-value response ({}), which every case below reads as a clean pass.
	httpFetch = func(string, string, io.Reader, time.Duration) (int, []byte, error) { return 200, []byte("{}"), nil }
	resolveProject = func(string) string { return "test-project" }
	// The passing control-plane default keeps the healthy-path test
	// deterministic; dedicated tests override controlPlaneHealth explicitly.
	controlPlaneHealth = func() CheckResult {
		return CheckResult{Name: "control-plane:health", Status: CheckStatusPass, Detail: "control-plane healthy"}
	}
	if home == "" {
		home = t.TempDir()
		userHomeDir = func() (string, error) { return home, nil }
	} else {
		userHomeDir = func() (string, error) { return home, nil }
	}
	// The healthy-path fixture deploys the Takt OpenCode plugins so their checks pass.
	if err := os.MkdirAll(filepath.Join(home, ".config", "opencode", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "opencode", "plugins", model.VFSPluginFile), []byte("plugin"), 0o644); err != nil {
		t.Fatal(err)
	}
	if look == nil {
		lookPath = func(tool string) (string, error) { return "/bin/" + tool, nil }
	} else {
		lookPath = look
	}
	// Keep the engram version check deterministic regardless of the host.
	resolveEngram = func(string) (string, bool) { return "/bin/engram", true }
	engramVersionFn = func(string) (string, error) { return "test-version", nil }
	resolveCodegraph = func(string) (string, bool) { return "/bin/codegraph", true }
	codegraphVersionFn = func(string) (string, error) { return "1.6.0", nil }
	// Takt's plugins target the V2 API, so the check needs a fixed version.
	openCodeHandshake = func() (opencodeapi.Handshake, error) {
		return opencodeapi.Handshake{Version: "2.0.16", Major: 2, ModelRoutes: true}, nil
	}
	if copies == nil {
		toolCopies = func(string) []string { return []string{"/bin"} }
	} else {
		toolCopies = copies
	}
	if get == nil {
		httpGet = func(string, time.Duration) (int, error) { return 200, nil }
	} else {
		httpGet = get
	}
	if free == nil {
		diskFree = func(string) (uint64, error) { return 1024 * 1024 * 1024, nil }
	} else {
		diskFree = free
	}
}

func assertContains(t *testing.T, output, want string) {
	t.Helper()
	if !strings.Contains(output, want) {
		t.Errorf("output missing %q\ngot:\n%s", want, output)
	}
}

func TestRunReportsMissingMemoryWriter(t *testing.T) {
	home := t.TempDir()
	withSeams(t, home, nil, nil, nil, nil)
	var output bytes.Buffer
	if err := Run(&output); err != nil {
		t.Fatal(err)
	}
	assertContains(t, output.String(), "opencode:memory-plugin")
	assertContains(t, output.String(), "memory_record")
	assertContains(t, output.String(), "takt-ai setup sync")
}

// TestEngramChecksUseResolvedBinary verifies the binary check follows engram.Resolve and the version check runs that path.
func TestEngramChecksUseResolvedBinary(t *testing.T) {
	home := t.TempDir()
	withSeams(t, home, nil, nil, nil, nil)
	resolveEngram = func(root string) (string, bool) {
		if root != home {
			t.Errorf("Resolve root = %q, want %q", root, home)
		}
		return "", false
	}
	checks := engramChecks(home)
	if len(checks) != 1 || checks[0].Status != CheckStatusFail || !strings.Contains(checks[0].Detail, ".takt-ai/bin/engram") {
		t.Fatalf("missing binary checks = %+v", checks)
	}

	managed := engram.ManagedBinaryPath(home)
	resolveEngram = func(string) (string, bool) { return managed, true }
	var ran string
	engramVersionFn = func(binary string) (string, error) { ran = binary; return "engram 2.1.0", nil }
	checks = engramChecks(home)
	if checks[0].Status != CheckStatusPass || ran != managed {
		t.Fatalf("checks = %+v, version ran %q; want managed binary", checks, ran)
	}
}

// TestCodegraphChecksUseResolvedBinary verifies the binary check follows codegraph.Resolve and the version check runs that path.
func TestCodegraphChecksUseResolvedBinary(t *testing.T) {
	home := t.TempDir()
	withSeams(t, home, nil, nil, nil, nil)
	resolveCodegraph = func(string) (string, bool) { return "", false }
	checks := codegraphChecks(home)
	if len(checks) != 1 || checks[0].Status != CheckStatusFail || !strings.Contains(checks[0].Detail, codegraph.ManagedBinaryPath(home)) {
		t.Fatalf("missing binary checks = %+v", checks)
	}

	managed := codegraph.ManagedBinaryPath(home)
	resolveCodegraph = func(string) (string, bool) { return managed, true }
	var ran string
	codegraphVersionFn = func(binary string) (string, error) { ran = binary; return "1.6.0", nil }
	checks = codegraphChecks(home)
	if len(checks) != 2 || checks[0].Status != CheckStatusPass || checks[1].Status != CheckStatusPass || ran != managed {
		t.Fatalf("checks = %+v, version ran %q; want managed binary", checks, ran)
	}
}

func TestRunEngramCheck(t *testing.T) {
	tests := []struct {
		name       string
		env        string
		status     int
		getErr     error
		wantSubstr []string
	}{
		{
			name:   "transport error fails with remedy",
			getErr: errors.New("connection refused"),
			wantSubstr: []string{
				"[xx]",
				"engram health endpoint unreachable at " + model.DefaultEngramURL + "/health: connection refused",
				"Remedy: Start engram or check that it is configured as an MCP server",
				"Status:  unhealthy",
			},
		},
		{
			name:       "non-2xx warns",
			status:     503,
			wantSubstr: []string{"[!!]", "engram health endpoint " + model.DefaultEngramURL + "/health returned HTTP 503", "Status:  degraded"},
		},
		{
			name:       "2xx passes",
			status:     200,
			wantSubstr: []string{"[ok]", "engram reachable at " + model.DefaultEngramURL + "/health"},
		},
		{
			name:       "env override drives URL",
			env:        "http://engram.example:9999",
			status:     200,
			wantSubstr: []string{"[ok]", "engram reachable at http://engram.example:9999/health"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENGRAM_BASE_URL", tc.env)
			withSeams(t, t.TempDir(), nil, nil, func(string, time.Duration) (int, error) {
				return tc.status, tc.getErr
			}, nil)
			var out bytes.Buffer
			if err := Run(&out); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			for _, want := range tc.wantSubstr {
				assertContains(t, out.String(), want)
			}
		})
	}
}

func TestEngramDiagnosticsCheck(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		fetchErr   error
		wantStatus CheckStatus
		wantSubstr []string
	}{
		{
			name: "unknown project passes", status: 404, body: `{"code":"unknown_project","available_projects":["other"]}`,
			wantStatus: CheckStatusPass, wantSubstr: []string{`no Engram history yet`, "other"},
		},
		{
			name: "clean report passes", status: 200, body: `{"summary":{"total":2,"ok":2}}`,
			wantStatus: CheckStatusPass, wantSubstr: []string{"2 engram diagnostic check(s), all clean"},
		},
		{
			name:       "warning report warns with remedy",
			status:     200,
			body:       `{"summary":{"total":2,"ok":1,"warnings":1},"checks":[{"check_id":"orphaned_observation_session","result":"warning","message":"3 orphaned","safe_next_step":"run cleanup"}]}`,
			wantStatus: CheckStatusWarn,
			wantSubstr: []string{"1 warning", "orphaned_observation_session: 3 orphaned"},
		},
		{
			name: "non-2xx warns", status: 500, body: `{"error":"boom"}`,
			wantStatus: CheckStatusWarn, wantSubstr: []string{"HTTP 500", "boom"},
		},
		{
			name: "transport error warns", fetchErr: errors.New("refused"),
			wantStatus: CheckStatusWarn, wantSubstr: []string{"unreachable", "refused"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			httpFetch = func(string, string, io.Reader, time.Duration) (int, []byte, error) {
				return tc.status, []byte(tc.body), tc.fetchErr
			}
			got := engramDiagnosticsCheck("proj")
			if got.Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q (detail: %s)", got.Status, tc.wantStatus, got.Detail)
			}
			for _, want := range tc.wantSubstr {
				if !strings.Contains(got.Detail, want) {
					t.Errorf("Detail = %q, want substring %q", got.Detail, want)
				}
			}
		})
	}
}

func TestEngramNeedsReviewCheck(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		fetchErr   error
		wantStatus CheckStatus
		wantSubstr []string
	}{
		{
			name: "unknown project passes", status: 404, body: `{"code":"unknown_project"}`,
			wantStatus: CheckStatusPass, wantSubstr: []string{"no Engram history yet"},
		},
		{
			name: "none due passes", status: 200, body: `{"observations":[],"count":0}`,
			wantStatus: CheckStatusPass, wantSubstr: []string{"no observations are due"},
		},
		{
			name:       "under limit warns without hedge",
			status:     200,
			body:       `{"observations":[{"id":7,"title":"Adopt X","type":"decision"}],"count":1}`,
			wantStatus: CheckStatusWarn,
			wantSubstr: []string{`#7 "Adopt X" (decision)`, "memory_record"},
		},
		{
			name:   "at limit hedges",
			status: 200,
			body: `{"observations":[{"id":1,"title":"a","type":"decision"},{"id":2,"title":"b","type":"decision"},` +
				`{"id":3,"title":"c","type":"decision"},{"id":4,"title":"d","type":"decision"},{"id":5,"title":"e","type":"decision"}],"count":5}`,
			wantStatus: CheckStatusWarn,
			wantSubstr: []string{"showing first 5"},
		},
		{
			name: "transport error warns", fetchErr: errors.New("refused"),
			wantStatus: CheckStatusWarn, wantSubstr: []string{"unreachable", "refused"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			httpFetch = func(string, string, io.Reader, time.Duration) (int, []byte, error) {
				return tc.status, []byte(tc.body), tc.fetchErr
			}
			got := engramNeedsReviewCheck("proj")
			if got.Status != tc.wantStatus {
				t.Fatalf("Status = %q, want %q (detail: %s)", got.Status, tc.wantStatus, got.Detail)
			}
			for _, want := range tc.wantSubstr {
				if !strings.Contains(got.Detail+got.Remedy, want) {
					t.Errorf("Detail/Remedy = %q / %q, want substring %q", got.Detail, got.Remedy, want)
				}
			}
		})
	}
}

func TestRunIncludesEngramDiagnosticsAndReview(t *testing.T) {
	home := t.TempDir()
	withSeams(t, home, nil, nil, nil, nil)
	var calledProject string
	resolveProject = func(dir string) string { calledProject = dir; return "takt-ai" }
	workingDir = func() (string, error) { return "/work/takt-ai", nil }

	var out bytes.Buffer
	if err := Run(&out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	assertContains(t, out.String(), "engram:diagnostics")
	assertContains(t, out.String(), "engram:needs-review")
	if calledProject != "/work/takt-ai" {
		t.Fatalf("resolveProject called with %q, want the working directory", calledProject)
	}
}

func TestWorkingDirOrHomeFallsBackToHome(t *testing.T) {
	orig := workingDir
	t.Cleanup(func() { workingDir = orig })
	workingDir = func() (string, error) { return "", errors.New("no cwd") }
	if got := workingDirOrHome("/home/x"); got != "/home/x" {
		t.Fatalf("workingDirOrHome() = %q, want fallback to home", got)
	}
}

func TestRunDiskCheck(t *testing.T) {
	const mb = 1024 * 1024
	tests := []struct {
		name       string
		free       func(string) (uint64, error)
		wantSubstr []string
		wantDir    bool
	}{
		{
			name:       "stat error warns",
			free:       func(string) (uint64, error) { return 0, errors.New("statfs failed") },
			wantSubstr: []string{"[!!]", "could not determine free disk space for"},
			wantDir:    true,
		},
		{
			name:       "critically low fails",
			free:       func(string) (uint64, error) { return 5 * mb, nil },
			wantSubstr: []string{"[xx]", "critically low disk space: 5 MB free on", "Status:  unhealthy"},
			wantDir:    true,
		},
		{
			name:       "low warns",
			free:       func(string) (uint64, error) { return 50 * mb, nil },
			wantSubstr: []string{"[!!]", "low disk space: 50 MB free", "Status:  degraded"},
		},
		{
			name:       "ample space passes",
			free:       func(string) (uint64, error) { return 1024 * mb, nil },
			wantSubstr: []string{"[ok]", "1024 MB free on"},
			wantDir:    true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			withSeams(t, home, nil, nil, nil, tc.free)
			var out bytes.Buffer
			if err := Run(&out); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			for _, want := range tc.wantSubstr {
				assertContains(t, out.String(), want)
			}
			if tc.wantDir {
				assertContains(t, out.String(), filepath.Join(home, ".takt-ai"))
			}
		})
	}
}

func TestRunReturnsErrorWhenHomeUnresolvable(t *testing.T) {
	orig := userHomeDir
	t.Cleanup(func() { userHomeDir = orig })
	userHomeDir = func() (string, error) { return "", errors.New("no home") }
	var out bytes.Buffer
	if err := Run(&out); err == nil {
		t.Fatal("Run() error = nil, want home resolution failure")
	}
}

type errWriter struct{}

func (errWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

func TestRunReturnsWriteError(t *testing.T) {
	withSeams(t, t.TempDir(), nil, nil, nil, nil)
	if err := Run(errWriter{}); err == nil {
		t.Fatal("Run() error = nil, want write failure")
	}
}

func TestRunControlPlaneCheck(t *testing.T) {
	tests := []struct {
		name       string
		health     func() CheckResult
		wantSubstr []string
	}{
		{
			name: "healthy passes",
			health: func() CheckResult {
				return CheckResult{Name: "control-plane:health", Status: CheckStatusPass, Detail: "control-plane healthy"}
			},
			wantSubstr: []string{
				"[ok]",
				"control-plane:health",
				"control-plane healthy",
			},
		},
		{
			name: "degraded warns",
			health: func() CheckResult {
				return CheckResult{Name: "control-plane:health", Status: CheckStatusWarn, Detail: "bus degraded"}
			},
			wantSubstr: []string{
				"[!!]",
				"control-plane:health",
				"bus degraded",
				"Status:  degraded",
			},
		},
		{
			name: "no live session warns, never fails",
			health: func() CheckResult {
				return CheckResult{Name: "control-plane:health", Status: CheckStatusWarn, Detail: "no live session to inspect"}
			},
			wantSubstr: []string{
				"[!!]",
				"control-plane:health",
				"no live session to inspect",
				"Status:  degraded",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withSeams(t, t.TempDir(), nil, nil, nil, nil)
			controlPlaneHealth = tc.health
			var out bytes.Buffer
			if err := Run(&out); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
			for _, want := range tc.wantSubstr {
				assertContains(t, out.String(), want)
			}
		})
	}
}

// TestOpenCodePluginChecksWarnWhenMissingAndFailWhenNotAFile pins the Takt VFS plugin:
// an absent plugin warns with the setup remedy, a directory in its place fails.
func TestOpenCodePluginChecksWarnWhenMissingAndFailWhenNotAFile(t *testing.T) {
	home := t.TempDir()
	checks := map[string]func(string) CheckResult{
		"opencode:vfs-plugin": opencodeVFSPluginCheck,
	}
	for name, check := range checks {
		got := check(home)
		if got.Name != name || got.Status != CheckStatusWarn || !strings.Contains(got.Remedy, "takt-ai setup sync") {
			t.Errorf("%s missing = %#v, want warn with setup remedy", name, got)
		}
	}
	plugins := filepath.Join(home, ".config", "opencode", "plugins")
	if err := os.MkdirAll(filepath.Join(plugins, model.VFSPluginFile), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, check := range checks {
		if got := check(home); got.Status != CheckStatusFail {
			t.Errorf("%s as directory = %#v, want fail", name, got)
		}
	}
}

// TestOpenCodeVersionCheckFailsBelowV2 covers the gate that keeps a V1 install
// from silently loading none of Takt's plugins.
func TestOpenCodeVersionCheckFailsBelowV2(t *testing.T) {
	previous := openCodeHandshake
	t.Cleanup(func() { openCodeHandshake = previous })
	for _, tc := range []struct {
		name   string
		major  int
		err    error
		status CheckStatus
	}{
		{name: "v2 passes", major: 2, status: CheckStatusPass},
		{name: "v1 fails", major: 1, status: CheckStatusFail},
		{name: "unknown warns", err: errors.New("no opencode"), status: CheckStatusWarn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			openCodeHandshake = func() (opencodeapi.Handshake, error) {
				return opencodeapi.Handshake{Version: fmt.Sprintf("%d.0.0", tc.major), Major: tc.major, ModelRoutes: tc.err == nil}, tc.err
			}
			if got := opencodeVersionCheck().Status; got != tc.status {
				t.Fatalf("status = %v, want %v", got, tc.status)
			}
		})
	}
}
