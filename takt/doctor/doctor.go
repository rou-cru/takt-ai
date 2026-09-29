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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/engram"
	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/setup"
)

const (
	// doctorCheckCapacity reserves the expected number of health checks.
	doctorCheckCapacity = 3
	// doctorHTTPTimeout bounds each remote health probe.
	doctorHTTPTimeout = 3 * time.Second
	// doctorRuleWidth is the separator width used by the human-readable report.
	doctorRuleWidth = 39
)

// CheckStatus is the outcome of a single doctor check.
type CheckStatus string

// Check outcomes: pass, warn (works but needs attention), fail (broken or missing).
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
var doctorTools = []string{"takt-ai", "opencode", "engram"}

// Injected seams, swapped in tests with t.Cleanup restore.
var (
	userHomeDir    = os.UserHomeDir
	lookPath       = exec.LookPath
	toolCopies     = scanToolCopies
	httpFetch      = defaultHTTPFetch
	diskFree       = statfsFreeBytes
	workingDir     = os.Getwd
	resolveProject = defaultResolveProject
)

// defaultResolveProject mirrors memory_record's own project detection, read-only.
func defaultResolveProject(dir string) string {
	return memory.ResolveProject(context.Background(), memory.Config{}, dir)
}

// controlPlaneHealth reports control-plane/bus health. Injectable seam so
// doctor stays decoupled from session/obs (importing session here would risk
// an import cycle). Default is warn — never fail when there is no live
// session to inspect.
var controlPlaneHealth = func() CheckResult {
	return CheckResult{
		Name:   "control-plane:health",
		Status: CheckStatusWarn,
		Detail: "no live session to inspect",
		Remedy: "Run within a live session for a full control-plane check",
	}
}

// Run executes every doctor check and renders the report to stdout. Failed
// checks do not affect the returned error: only internal failures (home
// resolution, write errors) are returned as errors.
func Run(stdout io.Writer) error {
	home, err := userHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	report := DoctorReport{Checks: toolChecks()}
	report.Checks = append(report.Checks, controlPlaneHealth())
	report.Checks = append(report.Checks, deploymentChecks(home)...)
	report.Checks = append(report.Checks, engramChecks(home)...)
	report.Checks = append(report.Checks, engramNativePluginCheck(home))
	project := resolveProject(workingDirOrHome(home))
	report.Checks = append(report.Checks, engramDiagnosticsCheck(project), engramNeedsReviewCheck(project))
	report.Checks = append(report.Checks, codegraphChecks(home)...)
	report.Checks = append(report.Checks, opencodeVersionCheck(), opencodeVFSPluginCheck(home), opencodeMemoryPluginCheck(home), opencodeSandboxAdapterCheck(home))
	report.Checks = append(report.Checks, diskCheck(home))
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

// deploymentChecks returns the global manifest check plus one
// "deployment:<target>" check per seen target, counting missing
// files per target instead of only globally.
func deploymentChecks(home string) []CheckResult {
	const name = "deployment:manifest"
	manifest, err := setup.LoadOwnershipManifest(home)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []CheckResult{{
				Name:   name,
				Status: CheckStatusWarn,
				Detail: fmt.Sprintf("no ownership manifest at %s (expected for first-time use)", home),
				Remedy: "Run 'takt-ai setup install' to deploy agent configs",
			}}
		}
		return []CheckResult{{
			Name:   name,
			Status: CheckStatusFail,
			Detail: err.Error(),
			Remedy: "Run 'takt-ai setup sync' or reinstall to repair",
		}}
	}
	total, byTarget := inspectDeployment(home, manifest)
	targetNames := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targetNames = append(targetNames, string(target))
	}
	slices.Sort(targetNames)
	global := deploymentResult(name, "managed", total)
	if total.missing == 0 && len(targetNames) > 0 {
		global.Detail += fmt.Sprintf(" (%s)", strings.Join(targetNames, ", "))
	}
	checks := []CheckResult{global}
	for _, target := range targetNames {
		checks = append(checks, deploymentResult("deployment:"+target, target, byTarget[setup.OwnershipTarget(target)]))
	}
	return checks
}

type deploymentTotals struct {
	total, missing int
}

// inspectDeployment preserves the existing classification: any stat error is missing.
func inspectDeployment(home string, manifest *setup.OwnershipManifest) (deploymentTotals, map[setup.OwnershipTarget]deploymentTotals) {
	total := deploymentTotals{}
	byTarget := make(map[setup.OwnershipTarget]deploymentTotals)
	for entryPath, entry := range manifest.Entries {
		_, err := os.Stat(filepath.Join(home, filepath.FromSlash(entryPath)))
		total.add(err != nil)
		for _, target := range entry.Targets {
			counts := byTarget[target]
			counts.add(err != nil)
			byTarget[target] = counts
		}
	}
	return total, byTarget
}

func (t *deploymentTotals) add(missing bool) {
	t.total++
	if missing {
		t.missing++
	}
}

func deploymentResult(name, label string, totals deploymentTotals) CheckResult {
	if totals.missing > 0 {
		return CheckResult{
			Name: name, Status: CheckStatusWarn,
			Detail: fmt.Sprintf("%d of %d %s files missing", totals.missing, totals.total, label),
			Remedy: "Run 'takt-ai setup sync' to restore missing files",
		}
	}
	return CheckResult{Name: name, Status: CheckStatusPass, Detail: fmt.Sprintf("%d %s files deployed", totals.total, label)}
}

// codegraphVersionFn and resolveCodegraph run the codegraph checks; vars for test seams.
var (
	codegraphVersionFn = codegraph.VerifyVersion
	resolveCodegraph   = codegraph.Resolve
)

// binaryChecks probes one managed binary: whether a compatible copy resolved
// (on PATH or under Takt's managed path) and, when it did, its version. ok is
// false when the binary is missing, so the caller can skip its remaining
// checks instead of producing noise.
func binaryChecks(prefix, expectedVersion, managedPath, missingRemedy, binary string, found bool, verifyVersion func(string) (string, error)) ([]CheckResult, bool) {
	var binaryErr error
	if !found {
		binaryErr = fmt.Errorf("no %s %s or newer on PATH or at %s", prefix, expectedVersion, managedPath)
	}
	checks := make([]CheckResult, 0, doctorCheckCapacity)
	checks = append(checks, CheckResult{
		Name:   prefix + ":binary",
		Status: checkStatusForError(binaryErr),
		Detail: checkDetailOrRemedy(binaryErr, prefix+" found at "+binary, missingRemedy),
	})
	if binaryErr != nil {
		return checks, false
	}
	version, versionErr := verifyVersion(binary)
	return append(checks, CheckResult{
		Name:   prefix + ":version",
		Status: checkStatusForError(versionErr),
		Detail: checkDetailOrRemedy(versionErr, prefix+" version: "+version, "Reinstall or update the "+prefix+" binary"),
	}), true
}

// codegraphChecks verifies a compatible codegraph binary (on PATH or Takt's
// managed copy) exists and answers its version, the two things its MCP entry needs.
func codegraphChecks(home string) []CheckResult {
	binary, found := resolveCodegraph(home)
	checks, _ := binaryChecks("codegraph", codegraph.CodegraphVersion, codegraph.ManagedBinaryPath(home),
		"Run 'takt-ai setup sync' to install codegraph so agents can explore the codebase",
		binary, found, codegraphVersionFn)
	return checks
}

// engramVersionFn and resolveEngram run the engram checks; vars for test seams.
var (
	engramVersionFn = engram.VerifyVersion
	resolveEngram   = engram.Resolve
)

// engramChecks verifies the engram installation (a compatible binary on PATH
// or Takt's managed copy, its version output) and the MCP health endpoint answers.
func engramChecks(home string) []CheckResult {
	binary, found := resolveEngram(home)
	checks, ok := binaryChecks("engram", engram.EngramVersion, engram.ManagedBinaryPath(home),
		"Run 'takt-ai setup sync' to install engram so agents can reach the memory server",
		binary, found, engramVersionFn)
	if !ok {
		// Without the binary the remaining engram checks only produce noise.
		return checks
	}

	const name = "engram:reachable"
	healthURL := engramBaseURL() + "/health"
	status, _, err := httpFetch(http.MethodGet, healthURL, nil, doctorHTTPTimeout)
	if err != nil {
		checks = append(checks, CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: fmt.Sprintf("engram health endpoint unreachable at %s: %s", healthURL, err),
			Remedy: "Start engram or check that it is configured as an MCP server",
		})
		return checks
	}
	if status < 200 || status >= 300 {
		checks = append(checks, CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: fmt.Sprintf("engram health endpoint %s returned HTTP %d", healthURL, status),
		})
		return checks
	}
	checks = append(checks, CheckResult{
		Name:   name,
		Status: CheckStatusPass,
		Detail: fmt.Sprintf("engram reachable at %s", healthURL),
	})
	return checks
}

// engramNativePluginCheck warns when Engram's own plugin is installed, since it injects a protocol that competes with Takt's memory contract.
func engramNativePluginCheck(home string) CheckResult {
	const name = "engram:native-plugin"
	found := engram.NativePluginFootprints(home)
	if len(found) == 0 {
		return CheckResult{Name: name, Status: CheckStatusPass, Detail: "no Engram plugin competes with Takt's memory contract"}
	}
	return CheckResult{
		Name:   name,
		Status: CheckStatusWarn,
		Detail: "Engram's own plugin injects a second memory protocol: " + strings.Join(found, ", "),
		Remedy: "Remove it to keep Takt's memory contract the only source: delete " +
			"~/" + model.OpenCodeConfigDir + "/" + model.OpenCodePluginsDir + "/" + model.EngramPluginFile,
	}
}

// engramBaseURL resolves the Engram HTTP endpoint the same way every engram check does.
func engramBaseURL() string {
	base := os.Getenv(model.EnvEngramURL)
	if base == "" {
		base = model.DefaultEngramURL
	}
	return strings.TrimRight(base, "/")
}

// workingDirOrHome resolves the directory doctor should report a project
// for. os.Getwd() failing is rare and not worth failing Run() over.
func workingDirOrHome(home string) string {
	if dir, err := workingDir(); err == nil {
		return dir
	}
	return home
}

// unknownProjectDetail reports whether status/body is Engram's 404 for a
// project it has never seen — the normal case for a brand-new repository,
// which must read as pass, not warn.
func unknownProjectDetail(status int, body []byte) (unknown bool, available []string) {
	if status != http.StatusNotFound {
		return false, nil
	}
	var payload struct {
		Code      string   `json:"code"`
		Available []string `json:"available_projects"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return false, nil
	}
	return payload.Code == "unknown_project", payload.Available
}

// errMessage extracts Engram's {"error": "..."} body, or the raw body when it isn't that shape.
func errMessage(body []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		return payload.Error
	}
	return strings.TrimSpace(string(body))
}

// engramDiagnosticsCheck surfaces Engram's own /doctor report — data-integrity
// checks (e.g. orphaned sessions) that engramChecks' plain /health probe never sees.
func engramDiagnosticsCheck(project string) CheckResult {
	const name = "engram:diagnostics"
	u := engramBaseURL() + "/doctor?project=" + url.QueryEscape(project)
	status, body, err := httpFetch(http.MethodGet, u, nil, doctorHTTPTimeout)
	if err != nil {
		return CheckResult{Name: name, Status: CheckStatusWarn,
			Detail: fmt.Sprintf("engram diagnostics unreachable at %s: %s", u, err)}
	}
	if unknown, available := unknownProjectDetail(status, body); unknown {
		return CheckResult{Name: name, Status: CheckStatusPass,
			Detail: fmt.Sprintf("project %q has no Engram history yet (known: %s)", project, strings.Join(available, ", "))}
	}
	if status < 200 || status >= 300 {
		return CheckResult{Name: name, Status: CheckStatusWarn,
			Detail: fmt.Sprintf("engram /doctor returned HTTP %d: %s", status, errMessage(body))}
	}
	var report struct {
		Summary struct{ Total, OK, Warnings, Blocked, Errors int } `json:"summary"`
		Checks  []struct {
			CheckID      string `json:"check_id"`
			Result       string `json:"result"`
			Message      string `json:"message"`
			SafeNextStep string `json:"safe_next_step"`
		} `json:"checks"`
	}
	if json.Unmarshal(body, &report) != nil {
		return CheckResult{Name: name, Status: CheckStatusWarn, Detail: "could not parse engram /doctor response"}
	}
	if report.Summary.Warnings+report.Summary.Blocked+report.Summary.Errors == 0 {
		return CheckResult{Name: name, Status: CheckStatusPass,
			Detail: fmt.Sprintf("%d engram diagnostic check(s), all clean", report.Summary.Total)}
	}
	var issues []string
	var remedy string
	for _, c := range report.Checks {
		if c.Result == "ok" {
			continue
		}
		issues = append(issues, fmt.Sprintf("%s: %s", c.CheckID, c.Message))
		if remedy == "" {
			remedy = c.SafeNextStep
		}
		if len(issues) == 3 {
			break
		}
	}
	return CheckResult{Name: name, Status: CheckStatusWarn,
		Detail: fmt.Sprintf("engram diagnostics: %d ok, %d warning, %d blocked, %d error across %d check(s) — %s",
			report.Summary.OK, report.Summary.Warnings, report.Summary.Blocked, report.Summary.Errors, report.Summary.Total, strings.Join(issues, "; ")),
		Remedy: remedy}
}

// engramNeedsReviewCheck surfaces decisions Engram auto-flagged for review
// (review_after passed) that nothing in Takt otherwise reads.
func engramNeedsReviewCheck(project string) CheckResult {
	const name = "engram:needs-review"
	const limit = 5
	u := fmt.Sprintf("%s/review?project=%s&limit=%d", engramBaseURL(), url.QueryEscape(project), limit)
	status, body, err := httpFetch(http.MethodGet, u, nil, doctorHTTPTimeout)
	if err != nil {
		return CheckResult{Name: name, Status: CheckStatusWarn,
			Detail: fmt.Sprintf("engram review list unreachable at %s: %s", u, err)}
	}
	if unknown, _ := unknownProjectDetail(status, body); unknown {
		return CheckResult{Name: name, Status: CheckStatusPass, Detail: fmt.Sprintf("project %q has no Engram history yet", project)}
	}
	if status < 200 || status >= 300 {
		return CheckResult{Name: name, Status: CheckStatusWarn,
			Detail: fmt.Sprintf("engram /review returned HTTP %d: %s", status, errMessage(body))}
	}
	var resp struct {
		Observations []struct {
			ID    int64  `json:"id"`
			Title string `json:"title"`
			Type  string `json:"type"`
		} `json:"observations"`
		Count int `json:"count"`
	}
	if json.Unmarshal(body, &resp) != nil {
		return CheckResult{Name: name, Status: CheckStatusWarn, Detail: "could not parse engram /review response"}
	}
	if resp.Count == 0 {
		return CheckResult{Name: name, Status: CheckStatusPass, Detail: "no observations are due for review"}
	}
	titles := make([]string, 0, len(resp.Observations))
	for _, o := range resp.Observations {
		titles = append(titles, fmt.Sprintf("#%d %q (%s)", o.ID, o.Title, o.Type))
	}
	hedge := ""
	if resp.Count == limit {
		hedge = fmt.Sprintf(" (showing first %d; more may exist)", limit)
	}
	return CheckResult{Name: name, Status: CheckStatusWarn,
		Detail: fmt.Sprintf("%d observation(s) due for review%s: %s", resp.Count, hedge, strings.Join(titles, "; ")),
		Remedy: "Record a fresh decision or observation via memory_record that reconfirms or supersedes the stale entry"}
}

func checkStatusForError(err error) CheckStatus {
	if err != nil {
		return CheckStatusFail
	}
	return CheckStatusPass
}

// opencodeVersionCheck reports whether the installed OpenCode is new enough to
// load Takt's plugins. Takt targets the V2 plugin API, and a V1 install skips
// every Takt plugin without an error of its own.
func opencodeVersionCheck() CheckResult {
	const name = "opencode:version"
	handshake, err := openCodeHandshake()
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: "OpenCode V2 handshake failed: " + opencodeapi.RedactError(err).Error(),
			Remedy: "Check that 'opencode api GET /api/info' and the model API work",
		}
	}
	if handshake.Major < opencodeapi.MinimumMajor {
		return CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: fmt.Sprintf("OpenCode %s is installed; Takt needs V2 or newer", handshake.Version),
			Remedy: "Upgrade OpenCode, then run 'takt-ai setup sync'",
		}
	}
	return CheckResult{Name: name, Status: CheckStatusPass, Detail: fmt.Sprintf("OpenCode %s with functional V2 API", handshake.Version)}
}

// openCodeHandshake is the V2 capability lookup seam; tests replace it.
var openCodeHandshake = func() (opencodeapi.Handshake, error) {
	return opencode.Handshake(context.Background())
}

// opencodeVFSPluginCheck reports whether the governed VFS plugin is deployed
// with the native write tools denied, so OpenCode mutations go through the
// durable core instead of the workspace.
func opencodeVFSPluginCheck(home string) CheckResult {
	return opencodePluginCheck(home, "opencode:vfs-plugin", model.VFSPluginFile, "VFS plugin", "the governed VFS plugin")
}

// opencodeMemoryPluginCheck reports whether the governed memory writer is
// deployed. Engram's MCP server is read-only; without this plugin an agent
// cannot call memory_record even though its contract requires it.
func opencodeMemoryPluginCheck(home string) CheckResult {
	return opencodePluginCheck(home, "opencode:memory-plugin", "takt-memory.ts", "Memory writer", "the memory writer that registers memory_record")
}

// opencodeSandboxAdapterCheck reports whether the sandbox adapter the VFS
// plugin loads to wrap shell commands is deployed. Without it every shell
// command is denied (PR-HAR-15 fail-closed), which is safe but leaves shell
// unusable, so a missing adapter is worth surfacing on its own.
func opencodeSandboxAdapterCheck(home string) CheckResult {
	// "takt-sandbox.mjs" matches the filename TaktSandboxAdapterArtifact
	// deploys and the sibling name the VFS plugin loads it by; no model
	// constant exists for it (unlike VFSPluginFile).
	return opencodePluginCheck(home, "opencode:sandbox-adapter", "takt-sandbox.mjs", "Sandbox adapter", "the sandbox adapter used to run captured shell commands")
}

// opencodePluginCheck verifies one Takt-deployed OpenCode plugin is a regular file, so a
// missing or replaced plugin is reported instead of silently disabling its behavior.
func opencodePluginCheck(home, name, file, label, deploys string) CheckResult {
	plugin := model.OpenCodePluginPath(home, file)
	info, err := os.Stat(plugin)
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: label + " not installed",
			Remedy: "Run 'takt-ai setup sync' to deploy " + deploys,
		}
	}
	if !info.Mode().IsRegular() {
		return CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: fmt.Sprintf("%s is not a regular file", plugin),
			Remedy: "Remove it and run 'takt-ai setup sync'",
		}
	}
	return CheckResult{Name: name, Status: CheckStatusPass, Detail: fmt.Sprintf("plugin installed at %s", plugin)}
}

func checkDetailOrRemedy(err error, okDetail, remedy string) string {
	if err == nil {
		return okDetail
	}
	return err.Error() + " — " + remedy
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

// defaultHTTPFetch performs one HTTP request and returns status plus body,
// capped so a misbehaving server can't exhaust memory.
func defaultHTTPFetch(method, url string, body io.Reader, timeout time.Duration) (int, []byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), method, url, body)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, data, err
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
	var out strings.Builder
	out.WriteString("takt-ai doctor — system health check\n")
	out.WriteString(strings.Repeat("=", doctorRuleWidth) + "\n\n")

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
		fmt.Fprintf(&out, "  %s  %-30s %s\n", checkIcons[check.Status], check.Name, check.Detail)
		if check.Remedy != "" {
			fmt.Fprintf(&out, "       Remedy: %s\n", check.Remedy)
		}
	}

	fmt.Fprintf(&out, "\nSummary: %d passed, %d failed, %d warnings\n", passed, failed, warnings)
	status := "healthy"
	if failed > 0 {
		status = "unhealthy"
	} else if warnings > 0 {
		status = "degraded"
	}
	fmt.Fprintf(&out, "Status:  %s\n", status)

	_, err := io.WriteString(w, out.String())
	return err
}
