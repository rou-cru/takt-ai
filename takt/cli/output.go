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

package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/verify"
)

// deploymentResult adds readiness info so old JSON readers keep working.
type deploymentResult struct {
	setup.DeploymentResult
	Verify *verify.Report `json:"verify,omitempty"`
}

// labeledLineFmt renders a bracketed label followed by a subject and an
// explanation, one per line.
const labeledLineFmt = "  [%s] %s — %s\n"

// renderPlanText shows the preview as text so users can review before applying.
func renderPlanText(w io.Writer, command string, plan any) error {
	switch p := plan.(type) {
	case lifecycle.InstallPreview:
		return renderInstallPlanText(w, command, p)
	case setup.UninstallResult:
		return renderUninstallPlanText(w, p)
	default:
		return fmt.Errorf("render plan: unsupported plan type %T", plan)
	}
}

// renderInstallPlanText lists planned files and conflicts so users know what needs a decision.
func renderInstallPlanText(w io.Writer, command string, plan lifecycle.InstallPreview) error {
	managed := 0
	for _, targetPlan := range plan.Plans {
		managed += len(targetPlan.ManagedPaths)
	}
	if err := writef(w, "Plan for %s: %d target(s), %d managed file(s).\n", command, len(plan.Plans), managed); err != nil {
		return err
	}
	if len(plan.Conflicts) == 0 {
		return writeln(w, "No conflicts: nothing blocks this plan.")
	}
	if err := writeln(w, "\nConflicts that require a decision (only available in the TUI):"); err != nil {
		return err
	}
	for _, conflict := range plan.Conflicts {
		label, _, explanation := setup.DriftLabel(conflict.Reason)
		if err := writef(w, labeledLineFmt, label, conflict.Path, explanation); err != nil {
			return err
		}
	}
	return nil
}

// renderUninstallPlanText lists removals and restores so users can review before deleting.
func renderUninstallPlanText(w io.Writer, result setup.UninstallResult) error {
	if err := writef(w, "Plan for uninstall: %d file(s) to remove, %d file(s) preserved.\n", len(result.Removed), len(result.Preserved)); err != nil {
		return err
	}
	for _, path := range result.Removed {
		if err := writef(w, "  [remove] %s\n", path); err != nil {
			return err
		}
	}
	for _, path := range result.Preserved {
		label, _, explanation := setup.DriftLabel(result.PreservedReasons[path])
		if err := writef(w, "  [preserve] %s — %s (%s)\n", path, explanation, label); err != nil {
			return err
		}
	}
	for _, path := range result.Restored {
		if err := writef(w, "  [restore] %s — content from before Takt was installed\n", path); err != nil {
			return err
		}
	}
	return nil
}

// renderResultText shows the result as text so users see what changed without parsing JSON.
func renderResultText(w io.Writer, command string, result any) error {
	switch r := result.(type) {
	case deploymentResult:
		return renderDeploymentResult(w, command, r)
	case setup.DeploymentResult:
		return renderDeployment(w, command, r)
	case setup.UninstallResult:
		return renderUninstall(w, r)
	default:
		return fmt.Errorf("render result: unsupported result type %T", result)
	}
}

func renderDeploymentResult(w io.Writer, command string, r deploymentResult) error {
	if err := renderDeployment(w, command, r.DeploymentResult); err != nil {
		return err
	}
	return renderVerificationText(w, r.Verify)
}

func renderUninstall(w io.Writer, r setup.UninstallResult) error {
	if err := writef(w, "Uninstall complete: %d removed, %d preserved.\n", len(r.Removed), len(r.Preserved)); err != nil {
		return err
	}
	return renderUninstallPaths(w, r)
}

func renderUninstallPaths(w io.Writer, r setup.UninstallResult) error {
	for _, item := range []struct {
		label string
		paths []string
	}{{"removed", r.Removed}, {"preserved", r.Preserved}, {"restored", r.Restored}} {
		for _, path := range item.paths {
			if err := writef(w, "  [%s] %s\n", item.label, path); err != nil {
				return err
			}
		}
	}
	return nil
}

// renderPartialResultText reports work that reached disk before a later step failed.
func renderPartialResultText(w io.Writer, command string, result any, cause error) error {
	if err := writef(w, "%s failed after applying partial work: %v\n", capitalize(command), cause); err != nil {
		return err
	}
	switch r := result.(type) {
	case deploymentResult:
		if err := renderDeploymentStatus(w, command, r.DeploymentResult, "partial"); err != nil {
			return err
		}
	case setup.DeploymentResult:
		if err := renderDeploymentStatus(w, command, r, "partial"); err != nil {
			return err
		}
	default:
		return renderResultText(w, command, result)
	}
	return nil
}

// renderDeployment prints changed files so users can confirm what was applied.
func renderDeployment(w io.Writer, command string, r setup.DeploymentResult) error {
	return renderDeploymentStatus(w, command, r, "complete")
}

func renderDeploymentStatus(w io.Writer, command string, r setup.DeploymentResult, status string) error {
	if err := writef(w, "%s %s: %d changed, %d unchanged.\n", capitalize(command), status, len(r.Changed), len(r.Unchanged)); err != nil {
		return err
	}
	for _, path := range r.Changed {
		if err := writef(w, "  [changed] %s\n", path); err != nil {
			return err
		}
	}
	for _, action := range r.Actions {
		if err := writef(w, "  [action] %s\n", action); err != nil {
			return err
		}
	}
	return nil
}

// renderVerificationText shows readiness separately so a not-ready system never looks like failure.
func renderVerificationText(w io.Writer, report *verify.Report) error {
	if report == nil {
		return nil
	}
	if err := writeln(w, "Verification:"); err != nil {
		return err
	}
	for _, check := range report.Checks {
		if err := writef(w, labeledLineFmt, string(check.State), check.ID, check.Explanation); err != nil {
			return err
		}
	}
	if report.Ready {
		if err := writeln(w, "  Ready: yes"); err != nil {
			return err
		}
	} else {
		if err := writeln(w, "  Not ready: some capabilities are not verified."); err != nil {
			return err
		}
	}
	return writef(w, "  %s\n", report.FinalNote)
}

// renderCancelledText explains a cancelled change so users know what stayed and how to finish.
func renderCancelledText(w io.Writer, command string, result lifecycle.LifecycleResult) error {
	if result.Outcome == lifecycle.OutcomeCancelledNothingApplied {
		return writef(w, "%s cancelled before any change was applied. Nothing was changed.\n", capitalize(command))
	}
	applied := slices.Concat(result.Changed, result.Removed, result.Restored)
	if err := writef(w, "%s cancelled after applying %d of %d changes.\n", capitalize(command), len(applied), len(applied)+len(result.NotApplied)); err != nil {
		return err
	}
	for _, path := range applied {
		if err := writef(w, "  [applied] %s\n", path); err != nil {
			return err
		}
	}
	if err := writef(w, "Not applied: %d. The applied changes stay in place.\n", len(result.NotApplied)); err != nil {
		return err
	}
	recovery := "Automatic rollback is not available."
	if result.BackupDir != "" {
		recovery += " Backup copies of replaced files are in " + result.BackupDir + "."
	}
	if err := writeln(w, recovery); err != nil {
		return err
	}
	return writef(w, "Run takt-ai setup %s again to finish the remaining changes; it checks the actual files first.\n", command)
}

func writef(w io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(w, format, args...)
	return err
}

func writeln(w io.Writer, args ...any) error {
	_, err := fmt.Fprintln(w, args...)
	return err
}

// capitalize uppercases the first letter so command names read well in sentences.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// blockOnConflicts stops on risky files so the CLI never guesses where the TUI must decide.
func blockOnConflicts(command string, conflicts []setup.ConflictEntry) ([]string, error) {
	var unrelated []string
	var blocking []setup.ConflictEntry
	for _, conflict := range conflicts {
		// A prior acceptance of this exact content is honored instead of
		// asked again; it never extends to new content or incompatibilities.
		if conflict.Impact == setup.ImpactUnrelated || (conflict.Accepted && conflict.Impact == setup.ImpactUncertain) {
			unrelated = append(unrelated, conflict.Path)
		} else {
			blocking = append(blocking, conflict)
		}
	}
	if len(blocking) == 0 {
		return unrelated, nil
	}
	var b strings.Builder
	if err := writef(&b, "setup %s: %d file(s) require a decision the CLI cannot make; open the TUI to resolve them:\n", command, len(blocking)); err != nil {
		return nil, err
	}
	for _, conflict := range blocking {
		label, _, explanation := setup.DriftLabel(conflict.Reason)
		if err := writef(&b, labeledLineFmt, label, conflict.Path, explanation); err != nil {
			return nil, err
		}
	}
	return nil, errors.New(strings.TrimRight(b.String(), "\n"))
}

// blockOnModifiedFiles stops uninstall on edited files so users never lose edits silently.
func blockOnModifiedFiles(preview setup.UninstallResult) error {
	var modified []string
	for _, path := range preview.Preserved {
		if preview.PreservedReasons[path] == "user-edited" {
			modified = append(modified, path)
		}
	}
	if len(modified) == 0 {
		return nil
	}
	var b strings.Builder
	if err := writef(&b, "setup uninstall: %d modified file(s) need a keep or remove decision the CLI cannot make; open the TUI to decide:\n", len(modified)); err != nil {
		return err
	}
	for _, path := range modified {
		if err := writef(&b, "  [modified] %s\n", path); err != nil {
			return err
		}
	}
	return errors.New(strings.TrimRight(b.String(), "\n"))
}
