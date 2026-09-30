// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package doctor

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

const (
	// maxListedIssues is how many failing engram diagnostics the detail names.
	maxListedIssues = 3
	// totalDiagnosticChecks is more than maxListedIssues so the cap is observable.
	totalDiagnosticChecks = 5
)

func TestToolCheckReportsMissingAndShadowedBinaries(t *testing.T) {
	withSeams(t, "", nil, nil, nil, nil)

	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	missing := toolCheck("opencode")
	if missing.Status != CheckStatusFail || missing.Name != "tool:opencode" ||
		missing.Detail != "opencode not found in PATH" || !strings.Contains(missing.Remedy, "Install opencode") {
		t.Errorf("missing tool check = %#v", missing)
	}

	lookPath = func(tool string) (string, error) { return "/a/" + tool, nil }
	toolCopies = func(string) []string { return []string{"/a", "/b"} }
	shadowed := toolCheck("engram")
	if shadowed.Status != CheckStatusWarn || !strings.Contains(shadowed.Detail, "2 copies found in PATH: /a, /b") ||
		!strings.Contains(shadowed.Remedy, "Remove duplicate engram binaries") {
		t.Errorf("shadowed tool check = %#v", shadowed)
	}
}

func TestScanToolCopiesListsEachPathDirectoryOnceInOrder(t *testing.T) {
	first, second, empty := t.TempDir(), t.TempDir(), t.TempDir()
	for _, dir := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(dir, "takt-tool"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// A directory named like the tool is not a binary copy.
	if err := os.Mkdir(filepath.Join(empty, "takt-tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The duplicate and unclean spelling of first collapse into one entry.
	path := strings.Join([]string{first, filepath.Join(first, "."), empty, second, filepath.Join(t.TempDir(), "absent")}, string(os.PathListSeparator))
	t.Setenv("PATH", path)

	got := scanToolCopies("takt-tool")
	if len(got) != 2 || got[0] != first || got[1] != second {
		t.Errorf("scanToolCopies() = %v, want [%s %s]", got, first, second)
	}
}

func TestDefaultControlPlaneHealthWarnsWithoutALiveSession(t *testing.T) {
	got := controlPlaneHealth()
	if got.Name != "control-plane:health" || got.Status != CheckStatusWarn || got.Remedy == "" {
		t.Errorf("default control-plane check = %#v, want a warning with a remedy", got)
	}
}

func TestDefaultResolveProjectUsesEngramsOwnDetection(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"project": "from-engram", "cwd": dir})
	}))
	t.Cleanup(server.Close)
	t.Setenv(model.EnvEngramURL, server.URL)

	if got := defaultResolveProject(dir); got != "from-engram" {
		t.Errorf("defaultResolveProject() = %q, want the project Engram reported", got)
	}
}

func TestDeploymentChecksFailOnUnreadableManifestAndListTargetsWhenHealthy(t *testing.T) {
	broken := t.TempDir()
	if err := os.WriteFile(filepath.Join(broken, setup.OwnershipManifestFilename), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	checks := deploymentChecks(broken)
	if len(checks) != 1 || checks[0].Status != CheckStatusFail || !strings.Contains(checks[0].Detail, "parse ownership manifest") ||
		!strings.Contains(checks[0].Remedy, "setup sync") {
		t.Errorf("corrupt manifest checks = %#v, want one failure with a repair remedy", checks)
	}

	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "present.txt"), []byte("managed"), 0o644); err != nil {
		t.Fatal(err)
	}
	entry, err := setup.NewOwnershipEntry("present.txt", []byte("managed"), 0o644, false, "", "", setup.TargetOpenCode)
	if err != nil {
		t.Fatal(err)
	}
	manifest := setup.NewOwnershipManifest()
	if err := manifest.Add(entry); err != nil {
		t.Fatal(err)
	}
	if err := manifest.Save(home); err != nil {
		t.Fatal(err)
	}
	checks = deploymentChecks(home)
	if checks[0].Status != CheckStatusPass || checks[0].Detail != "1 managed files deployed (opencode)" {
		t.Errorf("healthy global check = %#v, want the deployed targets listed", checks[0])
	}
}

func TestEngramNativePluginCheckWarnsWhenCompetingPluginIsInstalled(t *testing.T) {
	home := t.TempDir()
	if got := engramNativePluginCheck(home); got.Status != CheckStatusPass {
		t.Fatalf("clean home check = %#v, want pass", got)
	}
	plugin := model.OpenCodePluginPath(home, model.EngramPluginFile)
	if err := os.MkdirAll(filepath.Dir(plugin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(plugin, []byte("plugin"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := engramNativePluginCheck(home)
	if got.Status != CheckStatusWarn || !strings.Contains(got.Detail, plugin) || !strings.Contains(got.Remedy, model.EngramPluginFile) {
		t.Errorf("competing plugin check = %#v, want a warning naming %s", got, plugin)
	}
}

func TestEngramBodiesThatAreNotTheExpectedShapeDegradeToWarnings(t *testing.T) {
	withSeams(t, "", nil, nil, nil, nil)
	respond := func(status int, body string) {
		httpFetch = func(string, string, io.Reader, time.Duration) (int, []byte, error) { return status, []byte(body), nil }
	}

	t.Run("404 with a body that is not JSON is not an unknown project", func(t *testing.T) {
		respond(http.StatusNotFound, "no such route")
		got := engramDiagnosticsCheck("proj")
		if got.Status != CheckStatusWarn || !strings.Contains(got.Detail, "HTTP 404: no such route") {
			t.Errorf("check = %#v, want the raw body in the warning", got)
		}
	})
	t.Run("error JSON is unwrapped", func(t *testing.T) {
		respond(http.StatusInternalServerError, `{"error":"db locked"}`)
		if got := engramNeedsReviewCheck("proj"); got.Status != CheckStatusWarn || !strings.Contains(got.Detail, "HTTP 500: db locked") {
			t.Errorf("check = %#v, want the unwrapped error message", got)
		}
	})
	t.Run("unparseable diagnostics", func(t *testing.T) {
		respond(http.StatusOK, "<html>")
		if got := engramDiagnosticsCheck("proj"); got.Status != CheckStatusWarn || got.Detail != "could not parse engram /doctor response" {
			t.Errorf("check = %#v", got)
		}
	})
	t.Run("unparseable review list", func(t *testing.T) {
		respond(http.StatusOK, "<html>")
		if got := engramNeedsReviewCheck("proj"); got.Status != CheckStatusWarn || got.Detail != "could not parse engram /review response" {
			t.Errorf("check = %#v", got)
		}
	})
}

func TestEngramDiagnosticsListsOnlyFirstIssuesAndSkipsHealthyChecks(t *testing.T) {
	withSeams(t, "", nil, nil, nil, nil)
	type diagnostic struct {
		CheckID      string `json:"check_id"`
		Result       string `json:"result"`
		Message      string `json:"message"`
		SafeNextStep string `json:"safe_next_step"`
	}
	checks := []diagnostic{{CheckID: "healthy", Result: "ok", Message: "fine"}}
	for _, id := range []string{"one", "two", "three", "four"} {
		checks = append(checks, diagnostic{CheckID: id, Result: "warning", Message: "broken " + id, SafeNextStep: "fix " + id})
	}
	body, err := json.Marshal(map[string]any{
		"summary": map[string]int{"Total": totalDiagnosticChecks, "OK": 1, "Warnings": 4},
		"checks":  checks,
	})
	if err != nil {
		t.Fatal(err)
	}
	httpFetch = func(string, string, io.Reader, time.Duration) (int, []byte, error) { return http.StatusOK, body, nil }

	got := engramDiagnosticsCheck("proj")
	if got.Status != CheckStatusWarn || got.Remedy != "fix one" {
		t.Fatalf("check = %#v, want a warning carrying the first remedy", got)
	}
	for _, listed := range []string{"one: broken one", "two: broken two", "three: broken three"} {
		if !strings.Contains(got.Detail, listed) {
			t.Errorf("detail %q missing %q", got.Detail, listed)
		}
	}
	if strings.Contains(got.Detail, "healthy") || strings.Contains(got.Detail, "four") {
		t.Errorf("detail %q lists a healthy check or exceeds %d issues", got.Detail, maxListedIssues)
	}
}

func TestDefaultHTTPFetchSendsJSONBodiesAndReportsTransportErrors(t *testing.T) {
	var gotType, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotType = r.Header.Get("Content-Type")
		data, _ := io.ReadAll(r.Body)
		gotBody = string(data)
	}))
	url := server.URL

	status, _, err := defaultHTTPFetch(http.MethodPost, url, strings.NewReader(`{"a":1}`), time.Second)
	if err != nil || status != http.StatusOK {
		t.Fatalf("defaultHTTPFetch(POST) = (%d, %v), want 200 and no error", status, err)
	}
	if gotType != "application/json" || gotBody != `{"a":1}` {
		t.Errorf("server saw Content-Type %q body %q, want JSON", gotType, gotBody)
	}

	server.Close()
	if _, _, err := defaultHTTPFetch(http.MethodGet, url, nil, time.Second); err == nil {
		t.Error("defaultHTTPFetch() against a closed server returned no error")
	}
}
