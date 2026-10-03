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

// Package main runs the takt-ai command so users can drive setup from a terminal or scripts.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/rou-cru/takt-ai/takt/doctor"
	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	"github.com/rou-cru/takt-ai/takt/tui"
	"github.com/rou-cru/takt-ai/takt/tui/runtime"
	"github.com/rou-cru/takt-ai/takt/verify"
)

const cancelledExitStatus = 130

// usage is the public help: the commands a person runs, grouped. Commands
// Takt's OpenCode plugins call (memory, vfs, obs, gc, dispatch, dag,
// codegraph, setup image) are internal and documented with their callers.
const usage = `Takt AI — meta harness for agent teams, on top of OpenCode v2.

Usage:
  takt-ai                  open the interactive setup
  takt-ai <command> [flags]

Commands:
  setup install|sync|uninstall   apply Takt to OpenCode without the TUI
      --plan-only                show what would change; change nothing
      --yes                      apply the change (required unless --plan-only)
      --input FILE               setup request JSON, or - for stdin
                                 (default on a terminal: the recommended setup)
      --root DIR                 home directory to act on (default: $HOME)
      --json                     machine-readable output
  setup default-request          print the recommended setup request
  restore [--root DIR]           put back the files Takt replaced
  doctor                         check tools, configuration and capabilities
  version                        print the version
  help                           show this help`

// usageExitStatus is the exit status for a command line that names no command.
const usageExitStatus = 2

// publicCommands are the commands a person runs; anything else that is not an
// internal command is a usage mistake.
var publicCommands = map[string]bool{"setup": true, "restore": true, "doctor": true, "version": true, "--version": true, "-v": true}

// version holds the release version so users can report what they run.
var version = "dev"

// runTUI runs the interactive UI so tests can replace it without a terminal.
var runTUI = tui.Run

// isInteractive checks both streams so the TUI never starts where no one can see it.
// isTerminal reports whether r is a terminal, so a missing --input on a
// terminal means the recommended setup rather than a request to read.
var isTerminal = func(r io.Reader) bool {
	file, ok := r.(*os.File)
	return ok && term.IsTerminal(file.Fd())
}

var isInteractive = func(in io.Reader, out io.Writer) bool {
	inFile, inOK := in.(*os.File)
	outFile, outOK := out.(*os.File)
	return inOK && outOK && term.IsTerminal(inFile.Fd()) && term.IsTerminal(outFile.Fd())
}

// buildInfoReader reads build metadata so tests can fake a release version.
var buildInfoReader = debug.ReadBuildInfo

// interruptContext listens for Ctrl+C so a setup change stops safely instead of leaving broken files.
var interruptContext = func() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}

var runLifecycle = func(ctx context.Context, command, root string, request setup.PlanRequest, preserve ...string) (lifecycle.LifecycleResult, error) {
	return lifecycle.NewRuntime().Run(ctx, command, root, request, preserve...)
}

// errCancelled marks user cancellation so main can exit with status 130.
var errCancelled = errors.New("setup cancelled")

// resolveVersion picks the release version so version output stays correct in dev and release builds.
func resolveVersion(ldflagsVersion string) string {
	if ldflagsVersion != "dev" {
		return ldflagsVersion
	}
	info, ok := buildInfoReader()
	if !ok {
		return "dev"
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return strings.TrimPrefix(v, "v")
}

// main runs the CLI.
func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		if errors.Is(err, errCancelled) {
			os.Exit(cancelledExitStatus)
		}
		var code exitCode
		if errors.As(err, &code) {
			os.Exit(int(code))
		}
		os.Exit(1)
	}
}

// run handles every command.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	err := dispatch(args, stdin, stdout, stderr)
	var code exitCode
	if err != nil && !errors.Is(err, errCancelled) && !errors.As(err, &code) {
		if _, writeErr := fmt.Fprintln(stderr, err); writeErr != nil {
			return errors.Join(err, writeErr)
		}
	}
	return err
}

// dispatch routes args to their command.
func dispatch(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	// The installed record stores the version whose definitions it applied.
	setup.BuildVersion = resolveVersion(version)
	if len(args) == 0 {
		return dispatchInteractive(stdin, stdout)
	}
	if args[0] == "setup" && len(args) >= 2 && args[1] == "default-request" {
		return runDefaultRequest(args[2:], stdout)
	}
	return dispatchCommand(args, stdin, stdout, stderr)
}

func dispatchInteractive(stdin io.Reader, stdout io.Writer) error {
	if !isInteractive(stdin, stdout) {
		return errors.New(usage + "\ninteractive TUI requires a terminal; use `takt-ai setup ...` for non-interactive use")
	}
	return runTUI(stdin, stdout)
}

func dispatchCommand(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	switch args[0] {
	case "help", "--help", "-h":
		_, err := fmt.Fprintln(stdout, usage)
		return err
	case "version", "--version", "-v":
		_, err := fmt.Fprintf(stdout, "takt-ai %s\n", resolveVersion(version))
		return err
	case "doctor":
		if err := doctor.Run(stdout); errors.Is(err, doctor.ErrUnhealthy) {
			// The report already says what failed; the status lets scripts act.
			return exitCode(1)
		} else if err != nil {
			return err
		}
		return nil
	case "restore":
		return runRestore(args[1:], stdout)
	case "vfs":
		return runVFS(args[1:], stdin, stdout, stderr)
	case "obs":
		return runObs(args[1:], stdin, stdout, stderr)
	case "gc":
		return runGC(args[1:], stdout, stderr)
	case "dispatch":
		return runDispatch(args[1:], stdout, stderr)
	case "dag":
		return runDag(args[1:], stdout, stderr)
	case "memory":
		return runMemory(args[1:], stdin, stdout)
	case "codegraph":
		return runCodegraph(args[1:])
	}
	if !publicCommands[args[0]] {
		if _, err := fmt.Fprintf(stderr, "unknown command %q; run `takt-ai help` to see the commands\n", args[0]); err != nil {
			return err
		}
		return exitCode(usageExitStatus)
	}
	return runSetup(args, stdin, stdout, stderr)
}

// runSetup validates, previews, and applies a setup request.
func runSetup(args []string, stdin io.Reader, stdout, stderr io.Writer) (err error) {
	invocation, err := parseSetupInvocation(args)
	if err != nil {
		return err
	}
	// Run by hand without --input, stdin is the terminal, not a request: the
	// recommended setup is what a person means.
	if invocation.inputPath == "-" && !invocation.inputSet && isTerminal(stdin) {
		request, err := setup.DefaultPlanRequest()
		if err != nil {
			return err
		}
		return continueSetup(invocation, request, stdout, stderr)
	}
	input, closeInput, err := openRequestInput(invocation.inputPath, stdin)
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer func() { err = errors.Join(err, closeInput()) }()
	request, err := decodeStrict[setup.PlanRequest](input)
	if err != nil {
		return fmt.Errorf("invalid setup request: %w (pass --input FILE, or omit it on a terminal for the recommended setup)", err)
	}
	return continueSetup(invocation, request, stdout, stderr)
}

func continueSetup(invocation setupInvocation, request setup.PlanRequest, stdout, stderr io.Writer) error {
	// A previous abrupt stop is reported but never blocks: every command
	// re-checks the actual files before acting.
	if err := emitIncompleteNotice(invocation, stdout, stderr); err != nil {
		return err
	}
	if invocation.planOnly {
		return planSetup(invocation, request, stdout)
	}
	if !invocation.yes {
		return fmt.Errorf("setup %s: refusing to change the environment without --yes (use --plan-only to preview first)", invocation.command)
	}
	preserve, err := requireNoOpenDecisions(invocation, request)
	if err != nil {
		return err
	}
	if len(preserve) > 0 {
		if _, err := fmt.Fprintf(invocation.notices(stdout, stderr), "Left as is (not affected by this operation, or kept earlier in the TUI): %s\n", strings.Join(preserve, ", ")); err != nil {
			return fmt.Errorf("write notice: %w", err)
		}
	}
	return applySetup(invocation, request, preserve, stdout, stderr)
}

func emitIncompleteNotice(invocation setupInvocation, stdout, stderr io.Writer) error {
	record, found := setup.IncompleteOperation(invocation.root)
	if !found {
		return nil
	}
	for _, line := range record.Notice(invocation.root) {
		if _, err := fmt.Fprintln(invocation.notices(stdout, stderr), "Warning: "+line); err != nil {
			return fmt.Errorf("write warning: %w", err)
		}
	}
	return nil
}

func planSetup(invocation setupInvocation, request setup.PlanRequest, stdout io.Writer) error {
	plan, err := previewSetup(invocation, request)
	if err != nil {
		return err
	}
	return emit(invocation, stdout, plan, func() error {
		if preview, ok := plan.(lifecycle.InstallPreview); ok {
			summary, err := runtime.Summarize(invocation.root, request, preview)
			if err != nil {
				return err
			}
			return renderInstallPlanText(stdout, invocation.command, preview, summary)
		}
		return renderPlanText(stdout, invocation.command, plan)
	})
}

// previewSetup plans the operation without touching the environment.
func previewSetup(invocation setupInvocation, request setup.PlanRequest) (any, error) {
	plan, err := lifecycle.PreviewLifecycle(invocation.command, invocation.root, request)
	if err != nil {
		return nil, fmt.Errorf("plan %s: %w", invocation.command, err)
	}
	return plan, nil
}

// requireNoOpenDecisions returns the paths to leave as is, or an error when a decision is required.
func requireNoOpenDecisions(invocation setupInvocation, request setup.PlanRequest) ([]string, error) {
	if invocation.command != "install" && invocation.command != "uninstall" {
		return nil, nil
	}
	preview, err := previewSetup(invocation, request)
	if err != nil {
		return nil, err
	}
	var preserve []string
	if installPreview, ok := preview.(lifecycle.InstallPreview); ok && len(installPreview.Conflicts) > 0 {
		if preserve, err = blockOnConflicts(invocation.command, installPreview.Conflicts); err != nil {
			return nil, err
		}
	}
	if uninstallPreview, ok := preview.(setup.UninstallResult); ok {
		if err := blockOnModifiedFiles(uninstallPreview); err != nil {
			return nil, err
		}
	}
	return preserve, nil
}

// applySetup executes the authorized operation and emits its outcome.
func applySetup(invocation setupInvocation, request setup.PlanRequest, preserve []string, stdout, stderr io.Writer) error {
	ctx, stop := interruptContext()
	defer stop()
	result, outcome, err := executeSetup(ctx, invocation.command, invocation.root, request, preserve...)
	if err != nil {
		if hasAppliedResult(outcome) {
			if emitErr := emit(invocation, stdout, result, func() error {
				return renderPartialResultText(stdout, invocation.command, result, err)
			}); emitErr != nil {
				return emitErr
			}
		}
		return fmt.Errorf("setup %s: %w", invocation.command, err)
	}
	if outcome.Outcome != lifecycle.OutcomeCompleted {
		if err := emit(invocation, stdout, result, func() error { return nil }); err != nil {
			return err
		}
		if err := renderCancelledText(invocation.notices(stdout, stderr), invocation.command, outcome); err != nil {
			return err
		}
		return errCancelled
	}
	return emit(invocation, stdout, result, func() error { return renderResultText(stdout, invocation.command, result) })
}

// emit writes value as one JSON value, or through text when JSON was not asked for.
func emit(invocation setupInvocation, stdout io.Writer, value any, text func() error) error {
	if !invocation.json {
		return text()
	}
	if err := json.NewEncoder(stdout).Encode(value); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// notices is where human-readable diagnostics go: stderr under --json, so
// stdout stays a single JSON value.
func (i setupInvocation) notices(stdout, stderr io.Writer) io.Writer {
	if i.json {
		return stderr
	}
	return stdout
}

// runRestore restores pre-existing content so a deploy can be undone safely.
func runRestore(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("restore", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errors.New(usage)
		}
		return fmt.Errorf("invalid usage: %w", err)
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("invalid usage: unexpected argument %q", flags.Arg(0))
	}
	resolvedRoot, err := resolveRoot(*root)
	if err != nil {
		return err
	}
	manifest, err := setup.LoadOwnershipManifest(resolvedRoot)
	if err != nil {
		return fmt.Errorf("load ownership manifest: %w", err)
	}
	result, err := setup.Restore(resolvedRoot, manifest)
	if err != nil {
		return fmt.Errorf("restore: %w", err)
	}
	return json.NewEncoder(stdout).Encode(result)
}

// setupCommands lists valid setup actions so invalid commands fail with usage.
var setupCommands = map[string]bool{"install": true, "sync": true, "uninstall": true, "image": true}

// setupInvocation carries one validated setup request so execution never rechecks flags.
type setupInvocation struct {
	command   string
	root      string
	inputPath string
	// inputSet records an explicit --input, so `--input -` still reads stdin.
	inputSet bool
	planOnly bool
	yes      bool
	json     bool
}

// parseSetupInvocation validates setup flags so bad input fails before touching files.
func parseSetupInvocation(args []string) (setupInvocation, error) {
	if len(args) < 2 || args[0] != "setup" || !setupCommands[args[1]] {
		return setupInvocation{}, errors.New(usage)
	}
	flags := flag.NewFlagSet("setup "+args[1], flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("root", "", "")
	inputPath := flags.String("input", "-", "")
	planOnly := flags.Bool("plan-only", false, "")
	yes := flags.Bool("yes", false, "")
	jsonOutput := flags.Bool("json", false, "")
	if err := flags.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return setupInvocation{}, errors.New(usage)
		}
		return setupInvocation{}, fmt.Errorf("invalid usage: %w", err)
	}
	if flags.NArg() != 0 {
		return setupInvocation{}, fmt.Errorf("invalid usage: unexpected argument %q", flags.Arg(0))
	}
	resolvedRoot, err := resolveRoot(*root)
	if err != nil {
		return setupInvocation{}, err
	}
	inputSet := false
	flags.Visit(func(f *flag.Flag) { inputSet = inputSet || f.Name == "input" })
	return setupInvocation{
		command:   args[1],
		root:      resolvedRoot,
		inputPath: *inputPath,
		inputSet:  inputSet,
		planOnly:  *planOnly,
		yes:       *yes,
		json:      *jsonOutput,
	}, nil
}

// resolveRoot defaults to home so users can skip --root for personal installs.
func resolveRoot(root string) (string, error) {
	if root != "" {
		return root, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return home, nil
}

// openRequestInput opens stdin or a file so requests work in pipes and scripts.
func openRequestInput(inputPath string, stdin io.Reader) (io.Reader, func() error, error) {
	if inputPath == "-" {
		return stdin, func() error { return nil }, nil
	}
	file, err := os.Open(inputPath)
	if err != nil {
		return nil, nil, err
	}
	return file, file.Close, nil
}

// executeSetup applies one setup change so completion and readiness stay separate.
func executeSetup(ctx context.Context, command, root string, request setup.PlanRequest, preserve ...string) (any, lifecycle.LifecycleResult, error) {
	if command == "image" {
		// Image assembly applies the normal managed artifacts, without probing or
		// reloading an OpenCode process that is not running during a build.
		result, err := (lifecycle.Runtime{}).Run(ctx, "install", root, request, preserve...)
		deployed := setup.DeploymentResult{Changed: result.Changed, Unchanged: result.Unchanged, NotApplied: result.NotApplied}
		return deployed, result, err
	}
	result, err := runLifecycle(ctx, command, root, request, preserve...)
	if command == "uninstall" {
		out := setup.UninstallResult{Removed: result.Removed, Preserved: result.Preserved, Restored: result.Restored, NotApplied: result.NotApplied}
		return out, result, err
	}
	deployed := setup.DeploymentResult{Changed: result.Changed, Unchanged: result.Unchanged, NotApplied: result.NotApplied}
	if err != nil {
		return deployed, result, err
	}
	if result.Outcome != lifecycle.OutcomeCompleted {
		return deployed, result, nil
	}
	report := verify.CollectWithReload(ctx, root, verify.ReloadStatus{
		Attempted: result.ReloadAttempted,
		Err:       result.ReloadError,
	})
	return deploymentResult{DeploymentResult: deployed, Verify: &report}, result, nil
}

func hasAppliedResult(result lifecycle.LifecycleResult) bool {
	return len(result.Changed)+len(result.Removed)+len(result.Restored)+len(result.Actions) > 0
}

// decodeStrict reads exactly one JSON value into T, rejecting unknown fields.
func decodeStrict[T any](input io.Reader) (T, error) {
	var request, zero T
	decoder := json.NewDecoder(input)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		return zero, err
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return zero, errors.New("multiple JSON values")
		}
		return zero, err
	}
	return request, nil
}
