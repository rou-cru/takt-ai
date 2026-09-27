// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package verify collects functional evidence without invoking agent tools or authentication.
package verify

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// State distinguishes evidence from an inability to obtain evidence.
type State string

// Check states: verified means evidence was obtained, not verified means the
// check ran and found nothing to confirm, not verifiable means no evidence
// could be collected at all.
const (
	Verified      State = "verified"
	NotVerified   State = "not verified"
	NotVerifiable State = "not verifiable"
)

// CheckResult identifies a capability and explains the evidence obtained.
type CheckResult struct {
	ID          string
	State       State
	Explanation string
}

// Report describes functional availability, independently of installation success.
type Report struct {
	Checks    []CheckResult
	Ready     bool
	FinalNote string
}

// ReloadStatus carries the OpenCode handoff result into functional
// verification. Deployment can succeed while reload evidence is negative.
type ReloadStatus struct {
	Attempted bool
	Err       error
}

// Collect checks managed integrations from their installed configuration and
// OpenCode's native orchestrator inventory. It installs nothing and requires
// no LLM credentials.
func Collect(ctx context.Context, rootDir string) Report {
	return CollectWithReload(ctx, rootDir, ReloadStatus{})
}

// CollectWithReload extends Collect with the result of the post-deployment
// OpenCode reload. It keeps the existing no-reload API for diagnostics.
func CollectWithReload(ctx context.Context, rootDir string, reload ReloadStatus) Report {
	manifest, err := setup.LoadOwnershipManifest(rootDir)
	if err != nil {
		return summarize([]CheckResult{{"installation", NotVerifiable, fmt.Sprintf("Cannot identify managed installation: %v", err)}})
	}
	targets := ownedTargets(manifest)
	produce := func() []CheckResult { return produceChecks(ctx, rootDir, targets) }
	var checks []CheckResult
	if reload.Attempted && reload.Err == nil {
		// Reload's POST /api/location/reload returns as soon as the HTTP
		// response arrives, before OpenCode finishes connecting managed MCP
		// servers or evaluating plugins, so a single sample here races those
		// async effects. Give it a bounded window to settle instead.
		checks = settleAfterReload(ctx, produce)
	} else {
		checks = produce()
	}
	checks = append(checks, reloadCheck(reload)...)
	return summarize(checks)
}

// ownedTargets indexes the ownership manifest's targets for a quick lookup
// of whether an integration was installed.
func ownedTargets(manifest *setup.OwnershipManifest) map[setup.OwnershipTarget]bool {
	targets := make(map[setup.OwnershipTarget]bool)
	for _, entry := range manifest.Entries {
		for _, target := range entry.Targets {
			targets[target] = true
		}
	}
	return targets
}

// produceChecks runs the managed-MCP and native-orchestrator checks for
// every installed target, in place of an unbuilt one.
func produceChecks(ctx context.Context, rootDir string, targets map[setup.OwnershipTarget]bool) []CheckResult {
	var checks []CheckResult
	for _, target := range []setup.OwnershipTarget{setup.TargetOpenCode} {
		if targets[target] {
			checks = append(checks, managedMCPChecks(ctx, rootDir, target)...)
		}
	}
	if targets[setup.TargetOpenCode] {
		checks = append(checks, nativeOpenCodeChecks(ctx, rootDir)...)
	} else {
		checks = append(checks, CheckResult{"orchestrator:opencode:takt", NotVerifiable, "The selectable orchestrator requires an installed OpenCode target; no other native projection is provided yet."})
	}
	return checks
}

// reloadCheck reports the post-deployment OpenCode reload outcome, or no
// check at all when no reload was attempted.
func reloadCheck(reload ReloadStatus) []CheckResult {
	if !reload.Attempted {
		return nil
	}
	if reload.Err != nil {
		return []CheckResult{{"reload:opencode", NotVerified, fmt.Sprintf("OpenCode configuration reload failed: %v", reload.Err)}}
	}
	return []CheckResult{{"reload:opencode", Verified, "OpenCode configuration reload completed after deployment."}}
}

// reloadSettleTimeout and reloadSettleInterval bound settleAfterReload's wait
// for OpenCode to finish connecting managed MCP servers and evaluating
// plugins after a reload, mirroring the order of magnitude of
// takt/memory/client.go's defaultAvailabilityTimeout/defaultHealthPollInterval
// for the same "just asked a server to come up" wait. Package vars, not
// exported configuration: tests shrink them, production never tunes them.
var (
	reloadSettleTimeout  = 5 * time.Second
	reloadSettleInterval = 250 * time.Millisecond
)

// settleAfterReload retries produce on a bounded schedule until no check
// comes back NotVerified or the settle window elapses, then returns the last
// checks observed. It never blocks past reloadSettleTimeout and never errors.
func settleAfterReload(ctx context.Context, produce func() []CheckResult) []CheckResult {
	checks := produce()
	if !anyNotVerified(checks) {
		return checks
	}
	deadline, cancel := context.WithTimeout(ctx, reloadSettleTimeout)
	defer cancel()
	ticker := time.NewTicker(reloadSettleInterval)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.Done():
			return checks
		case <-ticker.C:
			checks = produce()
			if !anyNotVerified(checks) {
				return checks
			}
		}
	}
}

// anyNotVerified reports whether any check is NotVerified, the transient
// state settleAfterReload retries. NotVerifiable is a hard query error that a
// fast retry will not fix, so it is left alone.
func anyNotVerified(checks []CheckResult) bool {
	for _, check := range checks {
		if check.State == NotVerified {
			return true
		}
	}
	return false
}

func summarize(checks []CheckResult) Report {
	slices.SortFunc(checks, func(a, b CheckResult) int { return strings.Compare(a.ID, b.ID) })
	report := Report{Checks: checks, Ready: len(checks) > 0}
	for _, check := range checks {
		report.Ready = report.Ready && check.State == Verified
	}
	report.FinalNote = "Functional availability is not verified. Review the capability checks; installation success is a separate result."
	if report.Ready {
		report.FinalNote = "Managed MCP connections and orchestrator selection verified. LLM execution and Takt runtime enforcement were not tested."
	}
	return report
}
