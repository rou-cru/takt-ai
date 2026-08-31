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
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// withSeams replaces every doctor seam with deterministic doubles for the
// duration of the test and restores the originals via t.Cleanup. Nil
// arguments install passing defaults; an empty home installs a fresh temp dir.
func withSeams(t *testing.T, home string, look func(string) (string, error), copies func(string) []string, get func(string, time.Duration) (int, error), free func(string) (uint64, error)) {
	t.Helper()
	origHome, origLook, origCopies, origGet, origFree := userHomeDir, lookPath, toolCopies, httpGet, diskFree
	t.Cleanup(func() {
		userHomeDir, lookPath, toolCopies, httpGet, diskFree = origHome, origLook, origCopies, origGet, origFree
	})
	if home == "" {
		userHomeDir = func() (string, error) { return t.TempDir(), nil }
	} else {
		userHomeDir = func() (string, error) { return home, nil }
	}
	if look == nil {
		lookPath = func(tool string) (string, error) { return "/bin/" + tool, nil }
	} else {
		lookPath = look
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

// installManifest writes a real ownership manifest at home recording the
// given slash-relative entry paths; deployFiles also creates the files.
func installManifest(t *testing.T, home string, paths []string, deployFiles bool) {
	t.Helper()
	manifest := setup.NewOwnershipManifest()
	for _, entryPath := range paths {
		entry, err := setup.NewOwnershipEntry(entryPath, []byte("managed"), 0o644, false, "", "", setup.TargetClaude)
		if err != nil {
			t.Fatal(err)
		}
		if err := manifest.Add(entry); err != nil {
			t.Fatal(err)
		}
		if deployFiles {
			full := filepath.Join(home, filepath.FromSlash(entryPath))
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte("managed"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := manifest.Save(home); err != nil {
		t.Fatal(err)
	}
}

func assertContains(t *testing.T, output, want string) {
	t.Helper()
	if !strings.Contains(output, want) {
		t.Errorf("output missing %q\ngot:\n%s", want, output)
	}
}

func TestRunHealthySystemRendersReport(t *testing.T) {
	home := t.TempDir()
	installManifest(t, home, []string{".claude/CLAUDE.md"}, true)
	var gotURL string
	var gotTimeout time.Duration
	withSeams(t, home, nil, nil, func(url string, timeout time.Duration) (int, error) {
		gotURL, gotTimeout = url, timeout
		return 200, nil
	}, nil)

	var out bytes.Buffer
	if err := Run(&out); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	lines := strings.Split(out.String(), "\n")
	if lines[0] != "takt-ai doctor — system health check" {
		t.Errorf("header = %q, want doctor banner", lines[0])
	}
	if lines[1] != strings.Repeat("=", 39) {
		t.Errorf("rule = %q, want 39 equals signs", lines[1])
	}
	if lines[2] != "" {
		t.Errorf("line after rule = %q, want blank", lines[2])
	}
	wantToolLine := "  [ok]  tool:takt-ai" + strings.Repeat(" ", 19) + "takt-ai found at /bin/takt-ai"
	if lines[3] != wantToolLine {
		t.Errorf("tool line = %q, want %q", lines[3], wantToolLine)
	}
	output := out.String()
	assertContains(t, output, "1 managed files deployed (claude)")
	assertContains(t, output, "engram reachable at http://localhost:7437/health")
	assertContains(t, output, "Summary: 8 passed, 0 failed, 0 warnings")
	assertContains(t, output, "Status:  healthy")
	if gotURL != "http://localhost:7437/health" {
		t.Errorf("engram URL = %q, want default health endpoint", gotURL)
	}
	if gotTimeout != 3*time.Second {
		t.Errorf("engram timeout = %v, want 3s", gotTimeout)
	}
	if strings.Count(output, "[ok]") != 8 {
		t.Errorf("[ok] count = %d, want 8\noutput:\n%s", strings.Count(output, "[ok]"), output)
	}
}

func TestRunToolChecks(t *testing.T) {
	tests := []struct {
		name       string
		look       func(string) (string, error)
		copies     func(string) []string
		wantSubstr []string
	}{
		{
			name: "missing tool fails with remedy",
			look: func(string) (string, error) { return "", errors.New("executable file not found") },
			wantSubstr: []string{
				"[xx]",
				"codex not found in PATH",
				"Remedy: Install codex or add its directory to PATH",
				"Status:  unhealthy",
			},
		},
		{
			name:   "shadowed tool warns with copies list",
			copies: func(string) []string { return []string{"/usr/local/bin", "/opt/homebrew/bin"} },
			wantSubstr: []string{
				"[!!]",
				"claude found at /bin/claude but 2 copies found in PATH: /usr/local/bin, /opt/homebrew/bin",
				"Remedy: Remove duplicate claude binaries from PATH directories to avoid shadowing",
				"Status:  degraded",
			},
		},
		{
			name:       "single copy passes",
			wantSubstr: []string{"[ok]", "claude found at /bin/claude"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			withSeams(t, t.TempDir(), tc.look, tc.copies, nil, nil)
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

func TestRunDeploymentCheck(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(t *testing.T, home string)
		wantSubstr []string
	}{
		{
			name: "first-time use without manifest warns",
			wantSubstr: []string{
				"[!!]",
				"no ownership manifest at ",
				" (expected for first-time use)",
				"Remedy: Run 'takt-ai setup install' to deploy agent configs",
			},
		},
		{
			name: "corrupt manifest fails",
			prepare: func(t *testing.T, home string) {
				if err := os.WriteFile(filepath.Join(home, setup.OwnershipManifestFilename), []byte("{not json"), 0o644); err != nil {
					t.Fatal(err)
				}
			},
			wantSubstr: []string{
				"[xx]",
				"parse ownership manifest",
				"Remedy: Run 'takt-ai setup sync' or reinstall to repair",
				"Status:  unhealthy",
			},
		},
		{
			name: "all managed files present passes",
			prepare: func(t *testing.T, home string) {
				installManifest(t, home, []string{".claude/CLAUDE.md", ".codex/AGENTS.md"}, true)
			},
			wantSubstr: []string{"[ok]", "2 managed files deployed (claude)"},
		},
		{
			name: "missing managed files warn",
			prepare: func(t *testing.T, home string) {
				installManifest(t, home, []string{".claude/CLAUDE.md", ".codex/AGENTS.md"}, false)
			},
			wantSubstr: []string{
				"[!!]",
				"2 of 2 managed files missing",
				"Remedy: Run 'takt-ai setup sync' to restore missing files",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if tc.prepare != nil {
				tc.prepare(t, home)
			}
			withSeams(t, home, nil, nil, nil, nil)
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
				"engram health endpoint unreachable at http://localhost:7437/health: connection refused",
				"Remedy: Start engram or check that it is configured as an MCP server",
				"Status:  unhealthy",
			},
		},
		{
			name:       "non-2xx warns",
			status:     503,
			wantSubstr: []string{"[!!]", "engram health endpoint http://localhost:7437/health returned HTTP 503", "Status:  degraded"},
		},
		{
			name:       "2xx passes",
			status:     200,
			wantSubstr: []string{"[ok]", "engram reachable at http://localhost:7437/health"},
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

func TestScanToolCopies(t *testing.T) {
	dirA, dirB, dirC := t.TempDir(), t.TempDir(), t.TempDir()
	for _, dir := range []string{dirA, dirB} {
		if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// dirC holds a directory named like the tool: not a copy.
	if err := os.Mkdir(filepath.Join(dirC, "claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	separator := string(filepath.ListSeparator)
	t.Setenv("PATH", strings.Join([]string{dirA, dirB, dirA, dirC}, separator))

	got := scanToolCopies("claude")
	want := []string{dirA, dirB}
	if len(got) != len(want) {
		t.Fatalf("scanToolCopies = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("scanToolCopies[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
