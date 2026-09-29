// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

// Package opencodeapi is Takt's single adapter to the OpenCode v2 HTTP API.
//
// That API is experimental: its routes live under /api/*, response shapes can
// gain fields at any release, and even the CLI entry point may change how it
// resolves the server. Every consumer of that surface — model listing (R1),
// setup verification (R2) and configuration reload (R3) — goes through this
// package, so when OpenCode moves, Takt moves in exactly one place.
//
// The adapter shells out to the installed "opencode" binary instead of
// speaking HTTP directly. The binary knows how to find, authenticate against
// and start the right server (background service, --server URL, or a private
// --standalone instance); re-implementing that resolution in Takt would
// duplicate OpenCode's own connection logic and drift away from it. The
// subcommand used is "opencode api", which takes an HTTP method and a path,
// forwards them to the resolved server and prints the raw response body.
//
// Errors are typed because consumers must fail visibly and differently
// depending on the situation (PR-ART-2: never mutate on a broken foundation):
//
//   - ErrBinaryMissing: no usable opencode binary — installation cannot even
//     start, so callers should report "not installed" rather than "broken".
//   - ErrUnavailable: the binary exists but the API did not answer — the
//     server is down, unreachable, or the call failed; callers must treat
//     this as "not verifiable", never as "verified".
//   - ErrVersion: the server answered but is too old for the V2 surface
//     Takt requires — callers must abort before mutating anything.
//   - ErrInvalidResponse: the API answered but not with the expected JSON —
//     evidence of drift between OpenCode and this adapter.
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
		cmd := exec.CommandContext(ctx, binary, args...)
		cmd.Env = append(os.Environ(), env...)
		cmd.Dir = dir
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err := cmd.Run()
		return stdout.Bytes(), stderr.Bytes(), err
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
