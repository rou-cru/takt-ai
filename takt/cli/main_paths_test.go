package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/lifecycle"
	"github.com/rou-cru/takt-ai/takt/setup"
	setuputil "github.com/rou-cru/takt-ai/takt/setup/testutil"
)

// TestParseSetupInvocation verifies flags map to the invocation and bad input fails before touching files.
func TestParseSetupInvocation(t *testing.T) {
	got, err := parseSetupInvocation([]string{"setup", "sync", "--root", "/r", "--input", "in.json", "--plan-only", "--yes", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	want := setupInvocation{command: "sync", root: "/r", inputPath: "in.json", inputSet: true, planOnly: true, yes: true, json: true}
	if got != want {
		t.Fatalf("parseSetupInvocation() = %+v, want %+v", got, want)
	}

	for name, args := range map[string][]string{
		"no command":       {"setup"},
		"unknown command":  {"setup", "bogus"},
		"help":             {"setup", "install", "-h"},
		"unknown flag":     {"setup", "install", "--nope"},
		"positional extra": {"setup", "install", "extra"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseSetupInvocation(args); err == nil {
				t.Fatalf("parseSetupInvocation(%v) error = nil", args)
			}
		})
	}
	if _, err := parseSetupInvocation([]string{"setup", "install", "-h"}); err == nil || err.Error() != usage {
		t.Fatalf("help error = %v, want usage", err)
	}
}

// TestResolveRootDefaultsToHome verifies an empty root falls back to the home directory.
func TestResolveRootDefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got, err := resolveRoot(""); err != nil || got != home {
		t.Fatalf("resolveRoot(\"\") = %q, %v; want %q", got, err, home)
	}
	if got, err := resolveRoot("/x"); err != nil || got != "/x" {
		t.Fatalf("resolveRoot(/x) = %q, %v", got, err)
	}
}

// TestOpenRequestInput verifies dash reads stdin, a path opens the file, and a missing file errors.
func TestOpenRequestInput(t *testing.T) {
	stdin := strings.NewReader("{}")
	reader, closeInput, err := openRequestInput("-", stdin)
	if err != nil || reader != stdin || closeInput() != nil {
		t.Fatalf("openRequestInput(-) = %v, %v", reader, err)
	}

	path := filepath.Join(t.TempDir(), "in.json")
	if err := os.WriteFile(path, []byte(`{"a":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	reader, closeInput, err = openRequestInput(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(reader); err != nil || buf.String() != `{"a":1}` {
		t.Fatalf("file contents = %q, %v", buf.String(), err)
	}
	if err := closeInput(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := openRequestInput(filepath.Join(t.TempDir(), "missing.json"), nil); err == nil {
		t.Fatal("openRequestInput(missing) error = nil")
	}
}

// TestDecodeStrictRejectsTrailingAndMalformedValues verifies exactly one JSON value is accepted.
func TestDecodeStrictRejectsTrailingAndMalformedValues(t *testing.T) {
	if _, err := decodeStrict[map[string]int](strings.NewReader(`{"a":1} {"b":2}`)); err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("two values error = %v", err)
	}
	if _, err := decodeStrict[map[string]int](strings.NewReader(`{"a":1} {`)); err == nil {
		t.Fatal("malformed trailing value error = nil")
	}
	if got, err := decodeStrict[map[string]int](strings.NewReader(`{"a":1}`)); err != nil || got["a"] != 1 {
		t.Fatalf("decodeStrict() = %v, %v", got, err)
	}
}

// TestHasAppliedResult verifies only applied work counts as a partial result.
func TestHasAppliedResult(t *testing.T) {
	if hasAppliedResult(lifecycle.LifecycleResult{Unchanged: []string{"a"}, NotApplied: []string{"b"}}) {
		t.Error("unchanged/not-applied work reported as applied")
	}
	for name, r := range map[string]lifecycle.LifecycleResult{
		"changed":  {Changed: []string{"a"}},
		"removed":  {Removed: []string{"a"}},
		"restored": {Restored: []string{"a"}},
		"actions":  {Actions: []string{"a"}},
	} {
		if !hasAppliedResult(r) {
			t.Errorf("%s not reported as applied", name)
		}
	}
}

// TestNoticesGoToStderrUnderJSON verifies diagnostics never pollute the JSON stdout.
func TestNoticesGoToStderrUnderJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if got := (setupInvocation{json: true}).notices(&stdout, &stderr); got != &stderr {
		t.Error("json notices did not go to stderr")
	}
	if got := (setupInvocation{}).notices(&stdout, &stderr); got != &stdout {
		t.Error("text notices did not go to stdout")
	}
}

// TestEmitWritesJSONOrText verifies emit picks the output form and reports write failures.
func TestEmitWritesJSONOrText(t *testing.T) {
	var out bytes.Buffer
	if err := emit(setupInvocation{json: true}, &out, map[string]int{"n": 1}, func() error { t.Fatal("text called"); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != `{"n":1}` {
		t.Fatalf("json output = %q", out.String())
	}
	called := false
	if err := emit(setupInvocation{}, &out, nil, func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("text emit err = %v, called = %v", err, called)
	}
	if err := emit(setupInvocation{json: true}, failingWriter{}, 1, nil); !errors.Is(err, errWriteFailed) {
		t.Fatalf("emit() error = %v, want write failure", err)
	}
}

// TestEmitIncompleteNoticeReportsAbruptStop verifies a leftover operation record warns without blocking.
func TestEmitIncompleteNoticeReportsAbruptStop(t *testing.T) {
	root := t.TempDir()
	var stdout, stderr bytes.Buffer
	inv := setupInvocation{root: root, command: "install"}
	if err := emitIncompleteNotice(inv, &stdout, &stderr); err != nil || stdout.Len() != 0 {
		t.Fatalf("clean root notice = %q, %v", stdout.String(), err)
	}

	record, err := json.Marshal(setup.OperationRecord{Action: "install"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, setup.OperationRecordFilename), record, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := emitIncompleteNotice(inv, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(stdout.String(), "Warning: ") || stderr.Len() != 0 {
		t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	inv.json = true
	if err := emitIncompleteNotice(inv, &stdout, &stderr); err != nil || stdout.Len() != 0 || !strings.HasPrefix(stderr.String(), "Warning: ") {
		t.Fatalf("json notice stdout = %q, stderr = %q, err = %v", stdout.String(), stderr.String(), err)
	}
	if err := emitIncompleteNotice(inv, failingWriter{}, failingWriter{}); !errors.Is(err, errWriteFailed) {
		t.Fatalf("write failure error = %v", err)
	}
}

// TestRunSetupPlanOnlyPrintsPlanWithoutChanges verifies --plan-only previews and leaves the root untouched.
func TestRunSetupPlanOnlyPrintsPlanWithoutChanges(t *testing.T) {
	fakeOpenCodeLifecycle(t)
	root := t.TempDir()
	payload, err := json.Marshal(setuputil.TestPlanRequest())
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"setup", "install", "--root", root, "--plan-only"}, bytes.NewReader(payload), &stdout, &stderr); err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Plan for install") {
		t.Fatalf("stdout = %q, want install plan", stdout.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("root entries = %v, %v; want untouched", entries, err)
	}
}

// TestRunSetupInputErrors verifies unreadable and invalid requests are reported with context.
func TestRunSetupInputErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run([]string{"setup", "install", "--root", t.TempDir(), "--input", filepath.Join(t.TempDir(), "missing.json")}, strings.NewReader(""), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "open input") || !strings.Contains(stderr.String(), "open input") {
		t.Fatalf("missing input error = %v, stderr = %q", err, stderr.String())
	}
	err = run([]string{"setup", "install", "--root", t.TempDir()}, strings.NewReader(`{"unknown":true}`), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "invalid setup request") || !strings.Contains(err.Error(), "--input FILE") {
		t.Fatalf("invalid input error = %v", err)
	}
}

// TestContinueSetupRefusesWithoutYes verifies changes need explicit consent.
func TestContinueSetupRefusesWithoutYes(t *testing.T) {
	err := continueSetup(setupInvocation{command: "install", root: t.TempDir()}, setup.PlanRequest{}, &bytes.Buffer{}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "without --yes") {
		t.Fatalf("continueSetup() error = %v", err)
	}
}

// stubLifecycle replaces runLifecycle for the test and restores it afterwards.
func stubLifecycle(t *testing.T, result lifecycle.LifecycleResult, err error) {
	t.Helper()
	original := runLifecycle
	t.Cleanup(func() { runLifecycle = original })
	runLifecycle = func(context.Context, string, string, setup.PlanRequest, ...string) (lifecycle.LifecycleResult, error) {
		return result, err
	}
}

// TestApplySetupOutcomes verifies failure, cancellation, and completion each produce their own output and error.
func TestApplySetupOutcomes(t *testing.T) {
	inv := setupInvocation{command: "uninstall", root: t.TempDir()}

	t.Run("failure after partial work reports it", func(t *testing.T) {
		stubLifecycle(t, lifecycle.LifecycleResult{Removed: []string{"a.json"}}, errors.New("boom"))
		var stdout, stderr bytes.Buffer
		err := applySetup(inv, setup.PlanRequest{}, nil, &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "setup uninstall: boom") {
			t.Fatalf("applySetup() error = %v", err)
		}
		if !strings.Contains(stdout.String(), "failed after applying partial work") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("failure with nothing applied stays quiet", func(t *testing.T) {
		stubLifecycle(t, lifecycle.LifecycleResult{}, errors.New("boom"))
		var stdout bytes.Buffer
		if err := applySetup(inv, setup.PlanRequest{}, nil, &stdout, &bytes.Buffer{}); err == nil {
			t.Fatal("error = nil")
		}
		if stdout.Len() != 0 {
			t.Fatalf("stdout = %q, want empty", stdout.String())
		}
	})

	t.Run("cancellation returns errCancelled", func(t *testing.T) {
		stubLifecycle(t, lifecycle.LifecycleResult{Outcome: lifecycle.OutcomeCancelledNothingApplied}, nil)
		var stdout, stderr bytes.Buffer
		err := applySetup(inv, setup.PlanRequest{}, nil, &stdout, &stderr)
		if !errors.Is(err, errCancelled) {
			t.Fatalf("applySetup() error = %v, want errCancelled", err)
		}
		if !strings.Contains(stdout.String(), "cancelled before any change") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})

	t.Run("completed uninstall renders result", func(t *testing.T) {
		stubLifecycle(t, lifecycle.LifecycleResult{Outcome: lifecycle.OutcomeCompleted, Removed: []string{"a.json"}}, nil)
		var stdout bytes.Buffer
		if err := applySetup(inv, setup.PlanRequest{}, nil, &stdout, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(stdout.String(), "Uninstall complete: 1 removed") {
			t.Fatalf("stdout = %q", stdout.String())
		}
	})
}

// TestRequireNoOpenDecisionsSkipsOtherCommands verifies only install and uninstall gate on decisions.
func TestRequireNoOpenDecisionsSkipsOtherCommands(t *testing.T) {
	preserve, err := requireNoOpenDecisions(setupInvocation{command: "sync"}, setup.PlanRequest{})
	if preserve != nil || err != nil {
		t.Fatalf("requireNoOpenDecisions(sync) = %v, %v", preserve, err)
	}
}

// TestRunRestore verifies restore usage errors and the missing-manifest failure.
func TestRunRestore(t *testing.T) {
	var stdout bytes.Buffer
	if err := runRestore([]string{"-h"}, &stdout); err == nil || err.Error() != usage {
		t.Fatalf("help error = %v, want usage", err)
	}
	if err := runRestore([]string{"--nope"}, &stdout); err == nil || !strings.Contains(err.Error(), "invalid usage") {
		t.Fatalf("unknown flag error = %v", err)
	}
	if err := runRestore([]string{"extra"}, &stdout); err == nil || !strings.Contains(err.Error(), `unexpected argument "extra"`) {
		t.Fatalf("extra arg error = %v", err)
	}
	// A root that was never installed has no ownership manifest to restore from.
	if err := runRestore([]string{"--root", t.TempDir()}, &stdout); err == nil || !strings.Contains(err.Error(), "load ownership manifest") {
		t.Fatalf("empty root error = %v", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty on failure", stdout.String())
	}
}

// TestDispatchRoutesDefaultRequestAndRejectsBadArgs verifies setup default-request prints JSON and validates arguments.
func TestDispatchRoutesDefaultRequestAndRejectsBadArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"setup", "default-request", "extra"}, strings.NewReader(""), &stdout, &stderr); err == nil {
		t.Fatal("extra argument error = nil")
	}
	stdout.Reset()
	if err := run([]string{"setup", "default-request"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !json.Valid(stdout.Bytes()) {
		t.Fatalf("default-request output = %q", stdout.String())
	}
	if err := run([]string{"setup", "default-request", "--nope"}, strings.NewReader(""), &stdout, &stderr); err == nil {
		t.Fatal("unknown flag error = nil")
	}
	if err := run([]string{"gc"}, strings.NewReader(""), &stdout, &stderr); err == nil {
		t.Fatal("gc without subcommand error = nil")
	}
}

// TestRunPrintsErrorAndSkipsSilentOnes verifies plain errors reach stderr while cancellation and exit codes stay quiet.
func TestRunPrintsErrorAndSkipsSilentOnes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	var code exitCode
	if err := run([]string{"bogus"}, strings.NewReader(""), &stdout, &stderr); !errors.As(err, &code) || code != usageExitStatus || !strings.Contains(stderr.String(), `unknown command "bogus"`) {
		t.Fatalf("bogus error = %v, stderr = %q", err, stderr.String())
	}
	if err := run([]string{"bogus"}, strings.NewReader(""), &stdout, failingWriter{}); !errors.Is(err, errWriteFailed) {
		t.Fatalf("stderr write failure error = %v", err)
	}
}

// Help is a request, not a mistake: it prints the public commands and exits 0.
func TestHelpPrintsPublicCommandsOnly(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		var stdout, stderr bytes.Buffer
		if err := run([]string{arg}, strings.NewReader(""), &stdout, &stderr); err != nil {
			t.Fatalf("%s error = %v", arg, err)
		}
		for _, want := range []string{"setup install|sync|uninstall", "doctor", "restore"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("%s output lacks %q:\n%s", arg, want, stdout.String())
			}
		}
		for _, internal := range []string{"vfs", "obs ingest", "gc plan", "memory record"} {
			if strings.Contains(stdout.String(), internal) {
				t.Errorf("%s output lists the internal command %q", arg, internal)
			}
		}
	}
}

// By hand on a terminal, setup without --input means the recommended setup.
func TestSetupOnATerminalUsesTheRecommendedRequest(t *testing.T) {
	original := isTerminal
	isTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { isTerminal = original })
	var stdout, stderr bytes.Buffer
	if err := run([]string{"setup", "install", "--root", t.TempDir(), "--plan-only"}, strings.NewReader(""), &stdout, &stderr); err != nil {
		t.Fatalf("setup on a terminal error = %v, stderr = %q", err, stderr.String())
	}
}

// On a terminal, setup without --input preserves the recorded installation's
// choices instead of reintroducing what the user omitted.
func TestSetupOnATerminalPreservesTheRecordedInstallation(t *testing.T) {
	original := isTerminal
	isTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { isTerminal = original })
	root := t.TempDir()
	request, err := setup.DefaultPlanRequest()
	if err != nil {
		t.Fatal(err)
	}
	request.Components = []string{}
	if err := setup.SaveInstalledConfig(root, request); err != nil {
		t.Fatal(err)
	}
	var recorded, fresh bytes.Buffer
	if err := run([]string{"setup", "sync", "--root", root, "--plan-only", "--json"}, strings.NewReader(""), &recorded, io.Discard); err != nil {
		t.Fatalf("setup sync with a record error = %v", err)
	}
	if err := run([]string{"setup", "sync", "--root", t.TempDir(), "--plan-only", "--json"}, strings.NewReader(""), &fresh, io.Discard); err != nil {
		t.Fatalf("setup sync without a record error = %v", err)
	}
	if recorded.String() == fresh.String() {
		t.Fatal("the recorded exclusions did not change the plan: sync reintroduced what the user omitted")
	}
}
