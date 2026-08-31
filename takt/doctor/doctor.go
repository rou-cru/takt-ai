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

// Package doctor implements the `takt-ai doctor` system health check: tool
// availability in PATH, ownership-manifest deployment state, engram
// reachability, and free disk space.
package doctor

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// CheckStatus is the outcome of a single doctor check.
type CheckStatus string

// Check outcomes.
const (
	CheckStatusPass CheckStatus = "pass"
	CheckStatusWarn CheckStatus = "warn"
	CheckStatusFail CheckStatus = "fail"
)

// CheckResult is one health check outcome with an optional remediation hint.
type CheckResult struct {
	Name   string
	Status CheckStatus
	Detail string
	Remedy string
}

// DoctorReport aggregates every check executed in one doctor run.
type DoctorReport struct {
	Checks []CheckResult
}

// doctorTools are the CLI executables the Takt workflow depends on. The list
// is hardcoded because these are plain executable names; no catalog mapping
// exists for them.
var doctorTools = []string{"takt-ai", "claude", "codex", "opencode", "engram"}

// Injected seams, swapped in tests with t.Cleanup restore.
var (
	userHomeDir = os.UserHomeDir
	lookPath    = exec.LookPath
	toolCopies  = scanToolCopies
	httpGet     = defaultHTTPGet
	diskFree    = statfsFreeBytes
)

// Run executes every doctor check and renders the report to stdout. Failed
// checks do not affect the returned error: only internal failures (home
// resolution, write errors) are returned as errors.
func Run(stdout io.Writer) error {
	home, err := userHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	report := DoctorReport{Checks: toolChecks()}
	report.Checks = append(report.Checks, deploymentCheck(home), engramCheck(), diskCheck(home))
	return render(stdout, report)
}

// toolChecks returns one result per tool binary in doctorTools.
func toolChecks() []CheckResult {
	results := make([]CheckResult, 0, len(doctorTools))
	for _, tool := range doctorTools {
		results = append(results, toolCheck(tool))
	}
	return results
}

// toolCheck verifies a tool resolves in PATH and flags shadowed duplicates.
func toolCheck(tool string) CheckResult {
	name := "tool:" + tool
	path, err := lookPath(tool)
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: fmt.Sprintf("%s not found in PATH", tool),
			Remedy: fmt.Sprintf("Install %s or add its directory to PATH", tool),
		}
	}
	if copies := toolCopies(tool); len(copies) > 1 {
		return CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: fmt.Sprintf("%s found at %s but %d copies found in PATH: %s", tool, path, len(copies), strings.Join(copies, ", ")),
			Remedy: fmt.Sprintf("Remove duplicate %s binaries from PATH directories to avoid shadowing", tool),
		}
	}
	return CheckResult{
		Name:   name,
		Status: CheckStatusPass,
		Detail: fmt.Sprintf("%s found at %s", tool, path),
	}
}

// scanToolCopies reports the cleaned, deduplicated PATH directories that
// contain a non-directory file named tool, in PATH order.
func scanToolCopies(tool string) []string {
	seen := make(map[string]bool)
	var copies []string
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		if info, err := os.Stat(filepath.Join(dir, tool)); err == nil && !info.IsDir() {
			copies = append(copies, dir)
		}
	}
	return copies
}

// deploymentCheck inspects the ownership manifest beneath the home directory
// and whether every managed file is still deployed.
func deploymentCheck(home string) CheckResult {
	const name = "deployment:manifest"
	manifest, err := setup.LoadOwnershipManifest(home)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return CheckResult{
				Name:   name,
				Status: CheckStatusWarn,
				Detail: fmt.Sprintf("no ownership manifest at %s (expected for first-time use)", home),
				Remedy: "Run 'takt-ai setup install' to deploy agent configs",
			}
		}
		return CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: err.Error(),
			Remedy: "Run 'takt-ai setup sync' or reinstall to repair",
		}
	}
	missing := 0
	seenTargets := make(map[setup.OwnershipTarget]bool)
	for entryPath, entry := range manifest.Entries {
		if _, err := os.Stat(filepath.Join(home, filepath.FromSlash(entryPath))); err != nil {
			missing++
		}
		for _, target := range entry.Targets {
			seenTargets[target] = true
		}
	}
	total := len(manifest.Entries)
	if missing > 0 {
		return CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: fmt.Sprintf("%d of %d managed files missing", missing, total),
			Remedy: "Run 'takt-ai setup sync' to restore missing files",
		}
	}
	detail := fmt.Sprintf("%d managed files deployed", total)
	if len(seenTargets) > 0 {
		names := make([]string, 0, len(seenTargets))
		for target := range seenTargets {
			names = append(names, string(target))
		}
		sort.Strings(names)
		detail += fmt.Sprintf(" (%s)", strings.Join(names, ", "))
	}
	return CheckResult{Name: name, Status: CheckStatusPass, Detail: detail}
}

// engramCheck verifies the engram health endpoint answers.
func engramCheck() CheckResult {
	const name = "engram:reachable"
	base := os.Getenv("ENGRAM_BASE_URL")
	if base == "" {
		base = "http://localhost:7437"
	}
	url := strings.TrimRight(base, "/") + "/health"
	status, err := httpGet(url, 3*time.Second)
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: fmt.Sprintf("engram health endpoint unreachable at %s: %s", url, err),
			Remedy: "Start engram or check that it is configured as an MCP server",
		}
	}
	if status < 200 || status >= 300 {
		return CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: fmt.Sprintf("engram health endpoint %s returned HTTP %d", url, status),
		}
	}
	return CheckResult{
		Name:   name,
		Status: CheckStatusPass,
		Detail: fmt.Sprintf("engram reachable at %s", url),
	}
}

// diskCheck reports free space on the filesystem holding ~/.takt-ai.
func diskCheck(home string) CheckResult {
	const name = "disk:space"
	const mb = 1024 * 1024
	dir := filepath.Join(home, ".takt-ai")
	free, err := diskFree(dir)
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: fmt.Sprintf("could not determine free disk space for %s", dir),
		}
	}
	megabytes := free / mb
	switch {
	case free < 10*mb:
		return CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: fmt.Sprintf("critically low disk space: %d MB free on %s filesystem", megabytes, dir),
		}
	case free < 100*mb:
		return CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: fmt.Sprintf("low disk space: %d MB free", megabytes),
		}
	default:
		return CheckResult{
			Name:   name,
			Status: CheckStatusPass,
			Detail: fmt.Sprintf("%d MB free on %s filesystem", megabytes, dir),
		}
	}
}

// defaultHTTPGet performs one GET request and returns the HTTP status code.
func defaultHTTPGet(url string, timeout time.Duration) (int, error) {
	resp, err := (&http.Client{Timeout: timeout}).Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// statfsFreeBytes reports bytes available to unprivileged users on dir's
// filesystem (f_bsize * f_bavail).
func statfsFreeBytes(dir string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil {
		return 0, err
	}
	return uint64(stat.Bsize) * stat.Bavail, nil
}

// checkIcons renders the fixed-width status markers.
var checkIcons = map[CheckStatus]string{
	CheckStatusPass: "[ok]",
	CheckStatusWarn: "[!!]",
	CheckStatusFail: "[xx]",
}

// render writes the report in the format established by the validation
// branch: header, per-check lines with optional remedy lines, summary,
// and overall status.
func render(w io.Writer, report DoctorReport) error {
	if _, err := fmt.Fprintln(w, "takt-ai doctor — system health check"); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, strings.Repeat("=", 39)); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	var passed, failed, warnings int
	for _, check := range report.Checks {
		switch check.Status {
		case CheckStatusPass:
			passed++
		case CheckStatusWarn:
			warnings++
		case CheckStatusFail:
			failed++
		}
		if _, err := fmt.Fprintf(w, "  %s  %-30s %s\n", checkIcons[check.Status], check.Name, check.Detail); err != nil {
			return err
		}
		if check.Remedy != "" {
			if _, err := fmt.Fprintf(w, "       Remedy: %s\n", check.Remedy); err != nil {
				return err
			}
		}
	}
	if _, err := fmt.Fprintf(w, "\nSummary: %d passed, %d failed, %d warnings\n", passed, failed, warnings); err != nil {
		return err
	}
	status := "healthy"
	switch {
	case failed > 0:
		status = "unhealthy"
	case warnings > 0:
		status = "degraded"
	}
	_, err := fmt.Fprintf(w, "Status:  %s\n", status)
	return err
}
