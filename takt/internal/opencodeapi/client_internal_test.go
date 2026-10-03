// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencodeapi

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithEnvCopiesOverridesAndWithDirSetsWorkingDirectory(t *testing.T) {
	env := []string{"OPENCODE_TEST_VALUE=first"}
	client := New(WithEnv(env), WithDir(t.TempDir()), WithRunner(func(context.Context, []string) ([]byte, []byte, error) {
		return nil, nil, nil
	}))
	env[0] = "OPENCODE_TEST_VALUE=changed"

	if got := client.env; len(got) != 1 || got[0] != "OPENCODE_TEST_VALUE=first" {
		t.Fatalf("client env = %v, want copied original override", got)
	}
	if client.dir == "" {
		t.Fatal("WithDir() left the working directory empty")
	}
}

func TestExecRunnerUsesEnvironmentAndDirectoryAndReturnsProcessErrors(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf '%s|%s' \"$PWD\" \"$OPENCODE_TEST_VALUE\"\nprintf 'diagnostic' >&2\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_TEST_VALUE", "ambient")
	run := execRunner(binary, []string{"OPENCODE_TEST_VALUE=override"}, dir)
	stdout, stderr, err := run(context.Background(), []string{"version"})
	if err != nil {
		t.Fatalf("execRunner() error = %v", err)
	}
	physicalDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(stdout), physicalDir+"|override"; got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
	if got := string(stderr); got != "diagnostic" {
		t.Errorf("stderr = %q, want diagnostic", got)
	}

	if err := os.WriteFile(binary, []byte("#!/bin/sh\nprintf 'partial'\nprintf 'failed' >&2\nexit 8\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, err = run(context.Background(), nil)
	if err == nil {
		t.Fatal("execRunner() did not report a non-zero exit")
	}
	if string(stdout) != "partial" || string(stderr) != "failed" {
		t.Fatalf("failed process output = %q / %q", stdout, stderr)
	}
}

// opencode truncates bodies written to a pipe, so the runner must hand it a
// regular file as stdout.
func TestExecRunnerGivesStdoutAsRegularFile(t *testing.T) {
	dir := t.TempDir()
	binary := filepath.Join(dir, "opencode")
	if err := os.WriteFile(binary, []byte("#!/bin/sh\nif [ -p /dev/stdout ]; then printf pipe; else printf file; fi\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := execRunner(binary, nil, dir)(context.Background(), nil)
	if err != nil {
		t.Fatalf("execRunner() error = %v", err)
	}
	if string(stdout) != "file" {
		t.Fatalf("opencode stdout is a %q, want a regular file", stdout)
	}
}

func TestExecRunnerReportsMissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, _, err := execRunner("opencode-binary-that-does-not-exist", nil, "")(context.Background(), nil)
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("execRunner() error = %v, want exec.ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), `looking up "opencode-binary-that-does-not-exist"`) {
		t.Fatalf("lookup error = %v", err)
	}
}
