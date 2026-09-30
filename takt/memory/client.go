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

package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/rou-cru/takt-ai/takt/model"
)

const (
	defaultEngramURL           = model.DefaultEngramURL
	harnessTool                = "takt-harness"
	defaultHTTPTimeout         = 10 * time.Second
	defaultAvailabilityTimeout = 5 * time.Second
	defaultHealthPollInterval  = 100 * time.Millisecond
	defaultGitTimeout          = 2 * time.Second
	// maxErrorBodyBytes bounds diagnostic response reads from the memory service.
	maxErrorBodyBytes    = 1 << memoryErrorBodyShift
	memoryErrorBodyShift = 20
)

type engramClient struct {
	base                string
	http                *http.Client
	binary              string
	availabilityTimeout time.Duration
	pollInterval        time.Duration
	startServe          func(string) error
}

func newClient(cfg Config) *engramClient {
	base := cfg.EngramURL
	if base == "" {
		base = os.Getenv(model.EnvEngramURL)
	}
	if base == "" {
		base = defaultEngramURL
	}
	hc := cfg.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: defaultHTTPTimeout}
	}
	return &engramClient{base: strings.TrimRight(base, "/"), http: hc, binary: cfg.EngramBinary, availabilityTimeout: defaultAvailabilityTimeout, pollInterval: defaultHealthPollInterval, startServe: startDetachedServe}
}

// call sends body as JSON and decodes a 2xx response into out; non-2xx returns status with an error.
func (c *engramClient) call(ctx context.Context, method, path string, body, out any) (status int, err error) {
	reader, err := requestBody(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return 0, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("engram %s %s: %w", method, path, err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBodyBytes))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("engram %s %s: read response: %w", method, path, err)
	}
	if err := decodeResponse(data, resp.StatusCode, method, path, out); err != nil {
		return resp.StatusCode, err
	}
	return resp.StatusCode, nil
}

func requestBody(body any) (io.Reader, error) {
	if body == nil {
		return nil, nil
	}
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func decodeResponse(data []byte, status int, method, path string, out any) error {
	if status < 200 || status > 299 {
		return fmt.Errorf("engram %s %s: status %d: %s", method, path, status, strings.TrimSpace(string(data)))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("engram %s %s: decode response: %w", method, path, err)
	}
	return nil
}

func (c *engramClient) healthy(ctx context.Context) bool {
	_, err := c.call(ctx, http.MethodGet, "/health", nil, nil)
	return err == nil && ctx.Err() == nil
}

func (c *engramClient) ensureServe(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, c.availabilityTimeout)
	defer cancel()
	if err := waitCtx.Err(); err != nil {
		return c.availabilityError(ctx, err)
	}
	if c.healthy(waitCtx) {
		return nil
	}
	if err := waitCtx.Err(); err != nil {
		return c.availabilityError(ctx, err)
	}
	if err := c.startIfConfigured(); err != nil {
		return err
	}
	if err := c.waitForHealthy(waitCtx); err != nil {
		return c.availabilityError(ctx, err)
	}
	return nil
}

func (c *engramClient) startIfConfigured() error {
	if c.binary == "" {
		return fmt.Errorf("engram is not reachable at %s and no engram binary is configured to start it", c.base)
	}
	return c.startServe(c.binary)
}

func (c *engramClient) waitForHealthy(ctx context.Context) error {
	ticker := time.NewTicker(c.pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := ctx.Err(); err != nil {
				return err
			}
			if c.healthy(ctx) {
				return nil
			}
		}
	}
}

func (c *engramClient) availabilityError(caller context.Context, cause error) error {
	if err := caller.Err(); err != nil {
		return err
	}
	return fmt.Errorf("engram serve did not become healthy at %s within %s: %w", c.base, c.availabilityTimeout, cause)
}

func startDetachedServe(binary string) error {
	cmd := exec.Command(binary, "serve")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start engram serve: %w", err)
	}
	if err := cmd.Process.Release(); err != nil {
		return fmt.Errorf("release engram serve process: %w", err)
	}
	return nil
}

// ResolveProject reports the Engram project name for dir, the same
// resolution Record/Continue/Close use — exported for read-only callers
// (e.g. doctor) that need it without writing anything.
func ResolveProject(ctx context.Context, cfg Config, dir string) string {
	return newClient(cfg).project(ctx, dir)
}

// project prefers Engram's own detection when the server honours ?cwd=, else mirrors its core cases.
func (c *engramClient) project(ctx context.Context, dir string) string {
	var cur struct {
		Project string `json:"project"`
		Cwd     string `json:"cwd"`
	}
	if _, err := c.call(ctx, http.MethodGet, "/project/current?cwd="+url.QueryEscape(dir), nil, &cur); err == nil &&
		cur.Cwd == dir && strings.TrimSpace(cur.Project) != "" {
		return normalizeProject(cur.Project)
	}
	return detectProject(ctx, dir)
}

// detectProject resolves the project name from the Git remote, falling back to
// the repository root and then the directory's base name; Engram's
// .engram/config.json and child-repo cases are served by /project/current.
func detectProject(ctx context.Context, dir string) string {
	if out, err := git(ctx, dir, "remote", "get-url", "origin"); err == nil {
		parts := strings.FieldsFunc(strings.TrimSuffix(out, ".git"), func(r rune) bool { return r == '/' || r == ':' })
		if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) != "" {
			return normalizeProject(parts[len(parts)-1])
		}
	}
	if out, err := git(ctx, dir, "rev-parse", "--show-toplevel"); err == nil && out != "" {
		return normalizeProject(filepath.Base(out))
	}
	base := filepath.Base(dir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "unknown"
	}
	return normalizeProject(base)
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultGitTimeout)
	defer cancel()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, gitPath, append([]string{"-C", dir}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

var collapseRepeatedSeparators = regexp.MustCompile(`-+|_+`)

func normalizeProject(p string) string {
	n := collapseRepeatedSeparators.ReplaceAllStringFunc(strings.TrimSpace(strings.ToLower(p)), func(run string) string {
		return run[:1]
	})
	if n == "" {
		return "unknown"
	}
	return n
}

type observation struct {
	SessionID string `json:"session_id"`
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	ToolName  string `json:"tool_name"`
	Project   string `json:"project"`
	Scope     string `json:"scope"`
}

func (c *engramClient) addObservation(ctx context.Context, o observation) (int64, error) {
	var out struct {
		ID int64 `json:"id"`
	}
	if _, err := c.call(ctx, http.MethodPost, "/observations", o, &out); err != nil {
		return 0, err
	}
	if out.ID == 0 {
		return 0, fmt.Errorf("engram POST /observations: response carried no id")
	}
	return out.ID, nil
}

func (c *engramClient) link(ctx context.Context, a, b int64, relation, reasoning, model string) error {
	_, err := c.call(ctx, http.MethodPost, "/conflicts/compare", map[string]any{
		"memory_id_a": a,
		"memory_id_b": b,
		"relation":    relation,
		"confidence":  1,
		"reasoning":   reasoning,
		"model":       model,
	}, nil)
	return err
}
