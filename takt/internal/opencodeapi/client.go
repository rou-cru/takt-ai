// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package opencodeapi is Takt's single adapter to the OpenCode v2 HTTP API.
// That API is experimental (routes under /api/*, response shapes that can gain
// fields), so model listing, setup verification and configuration reload all
// go through here and Takt moves in exactly one place when OpenCode does.
//
// The adapter shells out to the installed "opencode" binary ("opencode api")
// rather than speaking HTTP directly, reusing OpenCode's own server discovery,
// authentication and startup instead of duplicating and drifting from it.
//
// Errors are typed so consumers fail visibly and differently (PR-ART-2: never
// mutate on a broken foundation): ErrBinaryMissing (not installed),
// ErrUnavailable (not verifiable), ErrVersion (too old; abort before mutating)
// and ErrInvalidResponse (drift).
package opencodeapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"time"
)

// runner executes the opencode binary and returns its output streams. It is
// injectable so tests can stub the CLI without shipping a fake binary.
type runner func(ctx context.Context, args []string) (stdout []byte, stderr []byte, err error)

// Typed failures of the adapter. Consumers branch on these with errors.Is;
// the wrapped causes only matter for humans reading the message.
var (
	// ErrBinaryMissing means no opencode binary could be located.
	ErrBinaryMissing = errors.New("opencode binary not found")

	// ErrUnavailable means the binary exists but the OpenCode API did not
	// answer as expected: server down, unreachable, call failed or timed out.
	ErrUnavailable = errors.New("opencode API is not available")

	// ErrVersion means the server answered but its version predates the V2
	// API surface Takt requires. Consumers must abort before mutating.
	ErrVersion = errors.New("opencode version is too old for Takt")

	// ErrInvalidResponse means the API answered but the body could not be
	// interpreted as the documented V2 shape. This signals drift between
	// OpenCode and this adapter, not a broken installation.
	ErrInvalidResponse = errors.New("opencode API returned an unexpected response")
)

// DefaultTimeout bounds every single CLI call, mirroring the 10s budget used
// for "opencode models" in takt/agents/opencode.
const DefaultTimeout = 10 * time.Second

// opencodeArgumentCapacity reserves the usual method/path argument slots.
const opencodeArgumentCapacity = 8

// Client is the entry point of the adapter. It is safe to create once and
// share: it holds no mutable state, only configuration.
type Client struct {
	run     runner
	timeout time.Duration
	env     []string
	dir     string
}

// Option customises a Client at construction time.
type Option func(*Client)

// WithRunner replaces the process execution layer. It is a test-support seam
// for callers that need to stub the CLI without shipping a fake binary.
func WithRunner(r runner) Option {
	return func(c *Client) {
		if r != nil {
			c.run = r
		}
	}
}

// WithEnv adds environment overrides to every CLI call. The adapter keeps the
// caller's environment and replaces only variables named here.
func WithEnv(env []string) Option {
	return func(c *Client) { c.env = append([]string(nil), env...) }
}

// WithDir runs the CLI from dir so workspace-local OpenCode locations are
// discovered alongside the environment overrides.
func WithDir(dir string) Option {
	return func(c *Client) { c.dir = dir }
}

// New builds a Client that delegates to the "opencode" binary in PATH with
// the default per-call timeout.
func New(options ...Option) *Client {
	c := &Client{
		timeout: DefaultTimeout,
	}
	for _, option := range options {
		option(c)
	}
	if c.run == nil {
		c.run = execRunner("opencode", c.env, c.dir)
	}
	return c
}

// execRunner runs the real binary. It performs the PATH lookup itself so the
// not-found case surfaces as exec.ErrNotFound and gets mapped to
// ErrBinaryMissing like any other lookup failure.
func execRunner(binary string, env []string, dir string) runner {
	return func(ctx context.Context, args []string) ([]byte, []byte, error) {
		if _, err := exec.LookPath(binary); err != nil {
			return nil, nil, fmt.Errorf("looking up %q: %w", binary, err)
		}
		// stdout goes to a file, not a pipe: opencode exits before draining a
		// pipe, truncating large bodies at a 64 KiB boundary (GET /api/skill
		// returns ~1 MiB), which surfaced as "unexpected end of JSON input".
		stdout, err := os.CreateTemp("", "takt-opencode-api-*")
		if err != nil {
			return nil, nil, fmt.Errorf("create stdout capture: %w", err)
		}
		defer func() {
			_ = stdout.Close()
			_ = os.Remove(stdout.Name())
		}()
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append(os.Environ(), env...)
		cmd.Dir = dir
		var stderr bytes.Buffer
		cmd.Stdout = stdout
		cmd.Stderr = &stderr
		runErr := cmd.Run()
		body, err := os.ReadFile(stdout.Name())
		if err != nil {
			return nil, stderr.Bytes(), fmt.Errorf("read stdout capture: %w", err)
		}
		return body, stderr.Bytes(), runErr
	}
}

// do executes one "opencode api" invocation and returns the raw response body.
//
// Non-zero exits and timeouts both mean the API did not produce an answer, so
// they collapse into ErrUnavailable; a missing binary is kept distinct
// because it describes a different installation state. The 10s-style timeout
// is applied here so every endpoint gets the same budget.
func (c *Client) do(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	// Flag order mirrors the CLI's own help: connection flags, request flags,
	// then the "METHOD path" pair. The CLI accepts raw "METHOD /path" input,
	// so no OpenAPI operation lookup is involved.
	args := make([]string, 0, opencodeArgumentCapacity)
	args = append(args, "api")
	if body != nil {
		args = append(args, "--data", string(body))
	}
	args = append(args, method, path)
	return c.invoke(ctx, method+" "+path, args)
}

// invoke runs one opencode CLI command under the per-call timeout, mapping
// its failures onto the adapter's typed errors; label names the call in them.
func (c *Client) invoke(ctx context.Context, label string, args []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	stdout, stderr, err := c.run(ctx, args)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %w", ErrBinaryMissing, err)
		}
		stderrText := redactSensitiveText(string(stderr))
		if stderrText == "" {
			stderrText = redactSensitiveText(err.Error())
		}
		return nil, fmt.Errorf("%w: opencode %s: %s", ErrUnavailable, label, stderrText)
	}
	return stdout, nil
}
