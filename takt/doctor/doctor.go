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
// availability in PATH, ownership-manifest deployment state, memory service
// status, and free disk space.
package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
	"github.com/rou-cru/takt-ai/takt/memory"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/setup"
)

const (
	// doctorCheckCapacity reserves the expected number of health checks.
	doctorCheckCapacity = 3
)

// CheckStatus is the outcome of a single doctor check.
type CheckStatus string

// Check outcomes.
const (
	// CheckStatusPass describes a check that holds, including benign states
	// such as a workspace with no event store yet.
	CheckStatusPass CheckStatus = "pass"
	// CheckStatusWarn describes something functional but needing attention,
	// for example a store readable by other users.
	CheckStatusWarn CheckStatus = "warn"
	// CheckStatusFail describes a broken or missing capability. The report
	// may include a Remedy hint.
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
var doctorTools = []string{"takt-ai", "opencode"}

// controlPlaneCheck inspects the event store of the workspace doctor runs in:
// it is the control plane's record, and the only part of it that outlives a
// session. A workspace without a store yet is healthy, not degraded.
func controlPlaneCheck(workspace string) CheckResult {
	const name = "control-plane:health"
	path, _ := obs.StorePath(workspace)
	health, err := obs.InspectStore(workspace)
	switch {
	case err != nil:
		return CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: "event store is unusable: " + err.Error(),
			Remedy: "Move " + path + " aside; Takt recreates it, but its recorded history is lost",
		}
	case !health.Exists:
		return CheckResult{Name: name, Status: CheckStatusPass, Detail: "no event store in this workspace yet"}
	case !health.Private:
		return CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: "event store is readable by other users",
			Remedy: "Run 'chmod 600 " + path + "'",
		}
	case health.Events == 0:
		return CheckResult{Name: name, Status: CheckStatusPass, Detail: "event store is intact, with no events recorded"}
	}
	return CheckResult{
		Name:   name,
		Status: CheckStatusPass,
		Detail: fmt.Sprintf("event store is intact: %d recorded, last at %s", health.Events, health.LastEventAt.Local().Format(time.DateTime)),
	}
}

// Run executes every doctor check and renders the report to stdout. Failed
// checks do not affect the returned error: only internal failures (home
// resolution, write errors) are returned as errors.
func Run(stdout io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	report := DoctorReport{Checks: toolChecks()}
	workspace := workingDirOrHome(home)
	report.Checks = append(report.Checks, controlPlaneCheck(workspace))
	report.Checks = append(report.Checks, deploymentChecks(home)...)
	report.Checks = append(report.Checks, memoryServiceCheck(context.Background()))
	report.Checks = append(report.Checks, codegraphChecks(home)...)
	report.Checks = append(report.Checks, opencodeVersionCheck(), opencodeVFSPluginCheck(home), opencodeMemoryPluginCheck(home), opencodeSandboxAdapterCheck(home), opencodeDAGPluginCheck(home))
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
	path, err := exec.LookPath(tool)
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: fmt.Sprintf("%s not found in PATH", tool),
			Remedy: fmt.Sprintf("Install %s or add its directory to PATH", tool),
		}
	}
	if copies := scanToolCopies(tool); len(copies) > 1 {
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

// binaryChecks probes one managed binary: whether a compatible copy resolved
// (on PATH or under Takt's managed path) and, when it did, its version. ok is
// false when the binary is missing, so the caller skips its remaining checks.
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
	binary, found := codegraph.Resolve(home)
	checks, _ := binaryChecks("codegraph", codegraph.CodegraphVersion, codegraph.ManagedBinaryPath(home),
		"Run 'takt-ai setup sync' to install codegraph so agents can explore the codebase",
		binary, found, codegraph.VerifyVersion)
	return checks
}

// workingDirOrHome resolves the directory doctor should report a project
// for. os.Getwd() failing is rare and not worth failing Run() over.
func workingDirOrHome(home string) string {
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return home
}

// memoryServiceCheck probes the memory service's status and never starts it (PR-MSV-33): ready, not
// ready with its reason, unreachable, malformed response, and newer store are each their own outcome.
// A ready service with entries lacking a semantic vector is a coverage gap, reported as a warning.
func memoryServiceCheck(ctx context.Context) CheckResult {
	const name = "memory:service"
	const startRemedy = "Run 'takt-ai memory start --init'"
	dir, err := memory.ResolveDir()
	if err != nil {
		return CheckResult{Name: name, Status: CheckStatusFail, Detail: "memory directory cannot be resolved: " + err.Error()}
	}
	status, health, err := memory.Assess(ctx, dir)
	switch health {
	case memory.HealthUnreachable:
		return CheckResult{Name: name, Status: CheckStatusFail, Detail: "no memory service answers in " + dir, Remedy: startRemedy}
	case memory.HealthMalformed:
		return CheckResult{Name: name, Status: CheckStatusFail, Detail: fmt.Sprintf("the memory service in %s answered with a malformed response: %v", dir, err),
			Remedy: "Stop whatever holds " + filepath.Join(dir, memory.SocketFile) + ", then run 'takt-ai memory start'"}
	case memory.HealthNewerStore:
		return CheckResult{Name: name, Status: CheckStatusFail, Detail: "the memory store is newer than this Takt build: " + status.Reason,
			Remedy: "Update Takt to a release at least as new as the store"}
	case memory.HealthNotReady:
		result := CheckResult{Name: name, Status: CheckStatusFail, Detail: fmt.Sprintf("the memory service %s is not ready (%s): %s", status.Version, status.State, status.Reason)}
		if status.State == memory.StateNoStore {
			result.Remedy = startRemedy
		}
		return result
	case memory.HealthUncovered:
		return CheckResult{Name: name, Status: CheckStatusWarn, Detail: fmt.Sprintf("the memory service %s is ready, but %d of %d entries have no semantic vector", status.Version, status.Total-status.Covered, status.Total)}
	}
	return CheckResult{Name: name, Status: CheckStatusPass, Detail: fmt.Sprintf("the memory service %s is ready: %d entries, all with semantic vectors", status.Version, status.Total)}
}

func checkStatusForError(err error) CheckStatus {
	if err != nil {
		return CheckStatusFail
	}
	return CheckStatusPass
}

// opencodeVersionCheck reports whether the installed OpenCode meets the minimum
// release Takt supports (opencodeapi.MinimumVersion). An older install skips
// Takt's plugins or lacks the session identity the memory tools rely on, without
// an error of its own.
func opencodeVersionCheck() CheckResult {
	const name = "opencode:version"
	handshake, err := opencode.Handshake(context.Background())
	if errors.Is(err, opencodeapi.ErrVersion) {
		return CheckResult{
			Name:   name,
			Status: CheckStatusFail,
			Detail: opencodeapi.RedactError(err).Error(),
			Remedy: "Upgrade OpenCode, then run 'takt-ai setup sync'",
		}
	}
	if err != nil {
		return CheckResult{
			Name:   name,
			Status: CheckStatusWarn,
			Detail: "OpenCode V2 handshake failed: " + opencodeapi.RedactError(err).Error(),
			Remedy: "Check that 'opencode api GET /api/info' and the model API work",
		}
	}
	return CheckResult{Name: name, Status: CheckStatusPass, Detail: fmt.Sprintf("OpenCode %s with functional V2 API", handshake.Version)}
}

// opencodeVFSPluginCheck reports whether the governed VFS plugin is deployed
// with the native write tools denied, so OpenCode mutations go through the
// durable core instead of the workspace.
func opencodeVFSPluginCheck(home string) CheckResult {
	return opencodePluginCheck(home, "opencode:vfs-plugin", model.VFSPluginFile, "VFS plugin", "the governed VFS plugin")
}

// opencodeMemoryPluginCheck reports whether the memory plugin is deployed.
// Without it nothing binds each crew call to its session and agent, so the
// memory tools refuse every call.
func opencodeMemoryPluginCheck(home string) CheckResult {
	return opencodePluginCheck(home, "opencode:memory-plugin", "takt-memory.ts", "Memory binding", "the plugin that binds each crew memory call to its session and agent")
}

// opencodeSandboxAdapterCheck reports whether the sandbox adapter the VFS
// plugin loads to wrap shell commands is deployed. Without it every shell
// command is denied (PR-HAR-15 fail-closed): safe, but shell is unusable, so
// the missing adapter is surfaced on its own.
func opencodeSandboxAdapterCheck(home string) CheckResult {
	// "takt-sandbox.mjs" matches the filename TaktSandboxAdapterArtifact
	// deploys and the sibling name the VFS plugin loads it by; no model
	// constant exists for it (unlike VFSPluginFile).
	return opencodePluginCheck(home, "opencode:sandbox-adapter", "takt-sandbox.mjs", "Sandbox adapter", "the sandbox adapter used to run captured shell commands")
}

// opencodeDAGPluginCheck reports whether the DAG view plugin is deployed at its
// TUI discovery path, so the execution DAG is not silently missing from OpenCode.
func opencodeDAGPluginCheck(home string) CheckResult {
	return opencodePluginCheck(home, "opencode:dag-plugin", "takt-dag/tui.tsx", "DAG plugin", "the DAG view plugin")
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
	dir := filepath.Join(home, ".takt-ai")
	free, err := statfsFreeBytes(dir)
	if err != nil {
		return CheckResult{
			Name:   diskCheckName,
			Status: CheckStatusWarn,
			Detail: fmt.Sprintf("could not determine free disk space for %s", dir),
		}
	}
	return ClassifyFreeSpace(dir, free)
}

// diskCheckName names the free-space check in the report.
const diskCheckName = "disk:space"

// ClassifyFreeSpace turns the bytes free on the filesystem holding dir into a
// check result: a failure under 10 MB, a warning under 100 MB, a pass above.
func ClassifyFreeSpace(dir string, free uint64) CheckResult {
	const mb = 1024 * 1024
	megabytes := free / mb
	switch {
	case free < 10*mb:
		return CheckResult{
			Name:   diskCheckName,
			Status: CheckStatusFail,
			Detail: fmt.Sprintf("critically low disk space: %d MB free on %s filesystem", megabytes, dir),
		}
	case free < 100*mb:
		return CheckResult{
			Name:   diskCheckName,
			Status: CheckStatusWarn,
			Detail: fmt.Sprintf("low disk space: %d MB free", megabytes),
		}
	default:
		return CheckResult{
			Name:   diskCheckName,
			Status: CheckStatusPass,
			Detail: fmt.Sprintf("%d MB free on %s filesystem", megabytes, dir),
		}
	}
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

// terminalText escapes control characters, line breaks included, in text a
// check carries from outside (memory service responses, error bodies), so it can
// neither drive the terminal nor forge report lines.
func terminalText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) {
			quoted := strconv.QuoteRuneToASCII(r)
			b.WriteString(quoted[1 : len(quoted)-1])
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// render writes the report in the format established by the validation
// branch: header, per-check lines with optional remedy lines, summary,
// and overall status.
func render(w io.Writer, report DoctorReport) error {
	var out strings.Builder
	out.WriteString("Takt AI doctor\n\n")

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
		fmt.Fprintf(&out, "  %s  %-30s %s\n", checkIcons[check.Status], check.Name, terminalText(check.Detail))
		if check.Remedy != "" {
			fmt.Fprintf(&out, "       Remedy: %s\n", terminalText(check.Remedy))
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

	if _, err := io.WriteString(w, out.String()); err != nil {
		return err
	}
	if failed > 0 {
		return ErrUnhealthy
	}
	return nil
}

// ErrUnhealthy reports that at least one check failed, so scripts can act on
// the exit status; the report itself was already written.
var ErrUnhealthy = errors.New("doctor: unhealthy")
