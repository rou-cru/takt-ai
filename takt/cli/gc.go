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
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/rou-cru/takt-ai/takt/codegraph"
	"github.com/rou-cru/takt-ai/takt/gc"
	"github.com/rou-cru/takt-ai/takt/obs"
	"github.com/rou-cru/takt-ai/takt/vfs"
)

// usageGC lists valid gc commands so callers can recover after a mistake.
const usageGC = "usage: takt-ai gc plan|findings|refute|acceptance --workspace <dir> --state <private-dir> --session <id> --cycle <id> --mandate dead-code|complexity|duplication|documentation|analyzer-integrity; refute also takes --finding <id> --class <evidence-class> --evidence <text> --instance <id>; acceptance takes --cycle <id> --result pass|regress"

// gcVerbs is the closed set of gc subcommands.
var gcVerbs = []string{"plan", "findings", "refute", "acceptance"}

// harnessAgent acts for the records a cycle produces on its own.
const harnessAgent = "harness"

// runGC serves the workspace GC cycle declaration and findings.
func runGC(args []string, stdout, stderr io.Writer) (err error) {
	if len(args) > 0 && args[0] == "coordinate" {
		return runGCCoordinate(args[1:], stdout, stderr)
	}
	opts, err := parseGCOptions(args, stderr)
	if err != nil {
		return err
	}
	return runGCCommand(opts, stdout)
}

type gcOptions struct {
	verb       string
	workspace  string
	state      string
	root       string
	result     string
	request    gc.Request
	refutation gc.Refutation
}

func parseGCOptions(args []string, stderr io.Writer) (gcOptions, error) {
	if len(args) == 0 || !slices.Contains(gcVerbs, args[0]) {
		return gcOptions{}, errors.New(usageGC)
	}
	opts := gcOptions{verb: args[0]}
	flags := flag.NewFlagSet("gc "+opts.verb, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&opts.workspace, "workspace", "", "workspace governed by the store")
	flags.StringVar(&opts.state, "state", "", "existing private VFS state directory")
	flags.StringVar(&opts.request.SessionID, "session", "", "session whose delta the cycle covers")
	flags.StringVar(&opts.request.CycleID, "cycle", "", "identifier of the cycle being declared")
	flags.StringVar((*string)(&opts.request.Mandate), "mandate", "", "the single mandate class of the cycle")
	flags.StringVar(&opts.root, "root", "", "install root where setup placed codegraph (default: home)")
	flags.StringVar(&opts.refutation.FindingID, "finding", "", "refute: ID of the finding the investigation refuted")
	flags.StringVar((*string)(&opts.refutation.Class), "class", "", "refute: what the analysis could not see")
	flags.StringVar(&opts.refutation.Evidence, "evidence", "", "refute: where the missed use was found")
	flags.StringVar(&opts.refutation.Instance, "instance", "", "refute: the refuting specialist instance")
	flags.StringVar(&opts.result, "result", "", "acceptance: how the checks stood, pass or regress")
	if err := flags.Parse(args[1:]); err != nil {
		return gcOptions{}, err
	}
	if flags.NArg() != 0 || opts.workspace == "" || opts.state == "" {
		return gcOptions{}, errors.New("gc: --workspace and --state are required; no positional arguments")
	}
	return opts, nil
}

func runGCCommand(opts gcOptions, stdout io.Writer) (err error) {
	// Planning reads evidence only; like offline journal export it must not create a store on a typo.
	if _, err := os.Stat(filepath.Join(opts.state, "vfs.sqlite")); err != nil {
		return fmt.Errorf("gc: existing store required: %w", err)
	}
	fs, err := vfs.Open(opts.workspace, opts.state)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, fs.Close()) }()
	entries := journalEntries(fs, opts.request.SessionID)
	// Closing a cycle needs neither the code graph nor the findings: what the
	// cycle consolidated is already on disk and already retained.
	if opts.verb == "acceptance" {
		return runGCAcceptanceCommand(fs, opts, stdout, entries)
	}
	plan, err := runGCPlan(opts, entries)
	if err != nil {
		return err
	}
	if opts.verb == "plan" {
		return json.NewEncoder(stdout).Encode(plan)
	}
	return runGCFindings(opts, stdout, plan)
}

func runGCPlan(opts gcOptions, entries []vfs.JournalEntry) (gc.Plan, error) {
	var reach gc.Reach
	var run gc.Runner
	// codegraph is installed under the install root (home by default), never inside the governed workspace.
	installRoot, err := resolveRoot(opts.root)
	if err != nil {
		return gc.Plan{}, err
	}
	if binary, ok := codegraph.Resolve(installRoot); ok {
		run = gc.ExecRunner(binary, opts.workspace)
		reach = gc.NewCodegraph(run).Dependents
	}
	return gc.Declare(context.Background(), entries, opts.request, reach)
}

func runGCFindings(opts gcOptions, stdout io.Writer, plan gc.Plan) error {
	prepared, err := gc.LoadPreparation(opts.workspace, opts.state)
	if err != nil {
		return err
	}
	if prepared.SessionID != opts.request.SessionID {
		return errors.New("gc: preparation belongs to another session")
	}
	report, err := gc.AnalyzePrepared(context.Background(), opts.workspace, plan, prepared)
	if err != nil {
		return err
	}
	// Refutations attach to the cycle in private state, beside the verdicts: like
	// findings they never enter the content-free journal. The workspace lock held
	// by fs serializes concurrent writers.
	refs, err := gc.LoadRefutations(opts.state, plan.CycleID)
	if err != nil {
		return err
	}
	if refs == nil {
		refs = []gc.Refutation{}
	}
	if opts.verb == "refute" {
		refs, err = runGCRefute(opts, plan, report, refs)
		if err != nil {
			return err
		}
	}
	return json.NewEncoder(stdout).Encode(struct {
		Plan        gc.Plan         `json:"plan"`
		Report      gc.Report       `json:"report"`
		Refutations []gc.Refutation `json:"refutations"`
		Actionable  []gc.Finding    `json:"actionable"`
	}{plan, report, refs, gc.Actionable(report.Findings, refs)})
}

func runGCRefute(opts gcOptions, plan gc.Plan, report gc.Report, refs []gc.Refutation) ([]gc.Refutation, error) {
	refutation := opts.refutation
	refutation.At = time.Now().UTC()
	refs, err := gc.Refute(plan, report, refs, refutation)
	if err != nil {
		return nil, err
	}
	if err = gc.SaveRefutations(opts.state, plan.CycleID, refs); err != nil {
		return nil, err
	}
	return refs, nil
}

func runGCAcceptanceCommand(fs *vfs.FS, opts gcOptions, stdout io.Writer, entries []vfs.JournalEntry) error {
	coordinated, err := gc.LoadCoordinator(opts.state)
	if err != nil {
		return err
	}
	if coordinated.Cycle != nil {
		return errors.New("gc: managed cycles require independent coordinator acceptance evidence")
	}
	return runGCAcceptance(fs, opts.workspace, stdout, opts.request.CycleID, opts.result, entries)
}

// journalEntries reads the whole journal for sessionID.
func journalEntries(fs *vfs.FS, sessionID string) []vfs.JournalEntry {
	var entries []vfs.JournalEntry
	for after := -1; ; {
		page := fs.JournalPage(sessionID, "", after, vfs.MaxJournalPageSize)
		entries = append(entries, page...)
		if len(page) < vfs.MaxJournalPageSize {
			break
		}
		after = page[len(page)-1].Seq
	}
	return entries
}

// runGCAcceptance closes a cycle once the acceptance checks have run.
func runGCAcceptance(fs *vfs.FS, workspace string, stdout io.Writer, cycleID, result string, entries []vfs.JournalEntry) error {
	if cycleID == "" {
		return errors.New("gc acceptance: --cycle is required")
	}
	outcome, err := gc.ParseAcceptanceResult(result)
	if err != nil {
		return err
	}
	mandate, session := cycleFacts(entries, cycleID)
	closure := gc.Closure{CycleID: cycleID, Mandate: mandate, Result: outcome}
	if closure.Discarded() {
		if closure.Restored, err = fs.DiscardCycle(cycleID); err != nil {
			return err
		}
		closure.Reason = gc.ReasonAcceptanceRegression
	} else if err = fs.CompleteCycle(cycleID); err != nil {
		return err
	}
	record := closure.ControlRecord(harnessAgent)
	record.Correlations = obs.CorrelationIDs{SessionID: session}
	if err = recordControlAction(workspace, record); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(struct {
		Closure gc.Closure        `json:"closure"`
		Record  obs.ControlRecord `json:"record"`
	}{closure, record})
}

// recordControlAction lands the decision on the control bus.
func recordControlAction(workspace string, record obs.ControlRecord) error {
	store, err := obs.OpenStore(workspace)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, store.Close()) }()
	_, err = store.AppendAction(record)
	return err
}

// cycleFacts reads the mandate class and session from the cycle's own journal entries.
func cycleFacts(entries []vfs.JournalEntry, cycleID string) (gc.MandateClass, string) {
	var mandate gc.MandateClass
	var session string
	for _, e := range entries {
		if e.CycleID != cycleID {
			continue
		}
		if mandate == "" && e.MandateClass != "" {
			mandate = gc.MandateClass(e.MandateClass)
		}
		if session == "" && e.SessionID != "" {
			session = e.SessionID
		}
	}
	return mandate, session
}
