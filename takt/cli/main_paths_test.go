package main_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/setup"
)

// requireEmptyDir requires that a command left dir without a single entry.
func requireEmptyDir(t *testing.T, dir string) {
	t.Helper()
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("%s entries = %v, %v; want untouched", dir, entries, err)
	}
}

// TestSetupRejectsBadInvocations verifies flag and command errors fail with usage before any file is touched.
func TestSetupRejectsBadInvocations(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	cases := map[string]struct {
		args []string
		want string
	}{
		"no command":       {[]string{"setup"}, "setup install|sync|uninstall"},
		"unknown command":  {[]string{"setup", "bogus"}, "setup install|sync|uninstall"},
		"help":             {[]string{"setup", "install", "-h"}, "setup install|sync|uninstall"},
		"unknown flag":     {[]string{"setup", "install", "--nope"}, "invalid usage"},
		"positional extra": {[]string{"setup", "install", "extra"}, `unexpected argument "extra"`},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			requireRefusal(t, s.run(tc.args...), tc.want)
		})
	}
}

// TestSetupFlagsReachTheCommand verifies --root, --input, --plan-only, --yes and --json take effect.
func TestSetupFlagsReachTheCommand(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	payload := installPayload(t)

	t.Run("plan-only prints the plan and leaves the root untouched", func(t *testing.T) {
		root := t.TempDir()
		res := s.runIn(payload, "setup", "install", "--root", root, "--plan-only")
		if res.code != 0 || !strings.Contains(res.stdout, "Plan for install") {
			t.Fatalf("exit %d, stdout %q, stderr %q; want the install plan", res.code, res.stdout, res.stderr)
		}
		requireEmptyDir(t, root)
	})

	t.Run("input file replaces stdin", func(t *testing.T) {
		root := t.TempDir()
		input := filepath.Join(t.TempDir(), "in.json")
		if err := os.WriteFile(input, []byte(payload), 0o600); err != nil {
			t.Fatal(err)
		}
		res := s.runIn("", "setup", "install", "--root", root, "--input", input, "--plan-only")
		if res.code != 0 || !strings.Contains(res.stdout, "Plan for install") {
			t.Fatalf("exit %d, stdout %q, stderr %q; want the install plan", res.code, res.stdout, res.stderr)
		}
	})

	t.Run("json prints one JSON value", func(t *testing.T) {
		res := s.runIn(payload, "setup", "install", "--root", t.TempDir(), "--plan-only", "--json")
		if res.code != 0 || !json.Valid([]byte(res.stdout)) {
			t.Fatalf("exit %d, stdout %q, stderr %q; want JSON", res.code, res.stdout, res.stderr)
		}
	})

	t.Run("changes need --yes", func(t *testing.T) {
		root := t.TempDir()
		requireRefusal(t, s.runIn(payload, "setup", "install", "--root", root), "without --yes")
		requireEmptyDir(t, root)
	})
}

// TestSetupInputErrors verifies unreadable and invalid requests are reported with context.
func TestSetupInputErrors(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	missing := filepath.Join(t.TempDir(), "missing.json")
	requireRefusal(t, s.run("setup", "install", "--root", t.TempDir(), "--input", missing), "open input")
	for name, input := range map[string]string{
		"unknown field":   `{"unknown":true}`,
		"multiple values": `{} {}`,
		"malformed":       `{`,
	} {
		t.Run(name, func(t *testing.T) {
			res := s.runIn(input, "setup", "install", "--root", t.TempDir(), "--plan-only")
			requireRefusal(t, res, "invalid setup request")
			requireRefusal(t, res, "--input FILE")
		})
	}
}

// TestSetupReportsAnAbruptStopWithoutBlocking verifies a leftover operation record warns, on stderr under --json,
// and that the home directory is the root when --root is omitted.
func TestSetupReportsAnAbruptStopWithoutBlocking(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	payload := installPayload(t)
	record, err := json.Marshal(setup.OperationRecord{Action: "install"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.home, setup.OperationRecordFilename), record, 0o600); err != nil {
		t.Fatal(err)
	}

	text := s.runIn(payload, "setup", "install", "--plan-only")
	if text.code != 0 || !strings.HasPrefix(text.stdout, "Warning: ") || strings.Contains(text.stderr, "Warning: ") {
		t.Fatalf("text: exit %d, stdout %q, stderr %q; want the warning on stdout", text.code, text.stdout, text.stderr)
	}

	asJSON := s.runIn(payload, "setup", "install", "--plan-only", "--json")
	if asJSON.code != 0 || !strings.HasPrefix(asJSON.stderr, "Warning: ") || !json.Valid([]byte(asJSON.stdout)) {
		t.Fatalf("json: exit %d, stdout %q, stderr %q; want the warning on stderr and JSON on stdout", asJSON.code, asJSON.stdout, asJSON.stderr)
	}
}

// TestRestoreErrors verifies restore usage errors and the missing-manifest failure.
func TestRestoreErrors(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	cases := map[string]struct {
		args []string
		want string
	}{
		"help":             {[]string{"-h"}, "setup install|sync|uninstall"},
		"unknown flag":     {[]string{"--nope"}, "invalid usage"},
		"positional extra": {[]string{"extra"}, `unexpected argument "extra"`},
		// A root that was never installed has no ownership manifest to restore from.
		"never installed": {[]string{"--root", t.TempDir()}, "load ownership manifest"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			res := s.run(append([]string{"restore"}, tc.args...)...)
			requireRefusal(t, res, tc.want)
			if res.stdout != "" {
				t.Fatalf("stdout = %q, want empty on failure", res.stdout)
			}
		})
	}
}

// TestDefaultRequestPrintsJSONAndValidatesArguments verifies setup default-request needs no terminal or input.
func TestDefaultRequestPrintsJSONAndValidatesArguments(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	res := s.run("setup", "default-request")
	if res.code != 0 || !json.Valid([]byte(res.stdout)) {
		t.Fatalf("exit %d, stdout %q, stderr %q; want JSON", res.code, res.stdout, res.stderr)
	}
	requireRefusal(t, s.run("setup", "default-request", "extra"), "usage: takt-ai setup default-request")
	requireRefusal(t, s.run("setup", "default-request", "--nope"), "invalid usage")
}

// TestUnknownCommandIsAUsageMistake verifies an unknown command is named on stderr with the usage exit status.
func TestUnknownCommandIsAUsageMistake(t *testing.T) {
	res := newSession(t, buildTaktAI(t)).run("bogus")
	if res.code != 2 || !strings.Contains(res.stderr, `unknown command "bogus"`) || res.stdout != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", res.code, res.stdout, res.stderr)
	}
}

// TestHelpPrintsPublicCommandsOnly verifies help is a request, not a mistake: it lists the public commands and exits 0.
func TestHelpPrintsPublicCommandsOnly(t *testing.T) {
	s := newSession(t, buildTaktAI(t))
	for _, arg := range []string{"help", "--help", "-h"} {
		t.Run(arg, func(t *testing.T) {
			res := s.run(arg)
			if res.code != 0 {
				t.Fatalf("exit %d, stderr %q", res.code, res.stderr)
			}
			for _, want := range []string{"setup install|sync|uninstall", "doctor", "restore"} {
				if !strings.Contains(res.stdout, want) {
					t.Errorf("output lacks %q:\n%s", want, res.stdout)
				}
			}
			for _, internal := range []string{"vfs", "obs ingest", "gc plan", "memory record"} {
				if strings.Contains(res.stdout, internal) {
					t.Errorf("output lists the internal command %q", internal)
				}
			}
		})
	}
}
