// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencodeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// MinimumMajor is the first OpenCode major release whose V2 API Takt targets;
// it mirrors opencode.MinimumVersion to keep this adapter decoupled.
const MinimumMajor = 2

// Info returns the exact version of the server the CLI resolves. It is the
// cheapest call and doubles as the availability probe of the handshake.
//
// Route: GET /api/info (server.info).
func (c *Client) Info(ctx context.Context) (Info, error) {
	raw, err := c.do(ctx, "GET", "/api/info", nil)
	if err != nil {
		return Info{}, err
	}
	var wire struct {
		Version string `json:"version"`
	}
	if err := decodeJSON(raw, &wire); err != nil {
		return Info{}, err
	}
	if wire.Version == "" {
		return Info{}, fmt.Errorf("%w: /api/info has no version field", ErrInvalidResponse)
	}
	return Info{Version: wire.Version}, nil
}

// Models returns the models the server currently exposes, in API order. An
// empty list is returned as-is, no error: deciding whether "no models" is
// fatal belongs to the consumer.
func (c *Client) Models(ctx context.Context) ([]Model, error) {
	return list(ctx, c, "/api/model", wireModel.toModel)
}

// DefaultModel returns the model used when a session has no explicit model
// selection, or nil when the server has no default configured. The nil is a
// meaningful answer, not an error.
func (c *Client) DefaultModel(ctx context.Context) (*Model, error) {
	raw, err := c.do(ctx, "GET", "/api/model/default", nil)
	if err != nil {
		return nil, err
	}
	var wire envelope[*wireModel]
	if err := decodeJSON(raw, &wire); err != nil {
		return nil, err
	}
	if wire.Data == nil {
		return nil, nil
	}
	model, err := wire.Data.toModel()
	if err != nil {
		return nil, fmt.Errorf("%w: /api/model/default: %s", ErrInvalidResponse, redactSensitiveText(err.Error()))
	}
	return &model, nil
}

// MCPStatus returns every configured MCP server with the connection state
// the server actually observes; failed or needs-auth servers never count as
// verified.
//
// Route: GET /api/mcp (mcp.list).
func (c *Client) MCPStatus(ctx context.Context) ([]MCPServer, error) {
	return list(ctx, c, "/api/mcp", wireMCPServer.toMCPServer)
}

// Agents returns the agents the server currently registered, in registration
// order; one written to disk but not registered never counts as ready.
//
// Route: GET /api/agent (agent.list).
func (c *Client) Agents(ctx context.Context) ([]Agent, error) {
	return list(ctx, c, "/api/agent", wireAgent.toAgent)
}

// Skills returns the skills the server discovered and can load on demand.
//
// Route: GET /api/skill (skill.list).
func (c *Client) Skills(ctx context.Context) ([]Skill, error) {
	return list(ctx, c, "/api/skill", wireSkill.toSkill)
}

// Plugins returns the enabled server plugins and whether each one loaded.
//
// Route: GET /api/plugin (plugin.list).
func (c *Client) Plugins(ctx context.Context) ([]Plugin, error) {
	return list(ctx, c, "/api/plugin", wirePlugin.toPlugin)
}

// list preserves API order and leaves endpoint-specific validation to convert.
func list[W, T any](ctx context.Context, c *Client, path string, convert func(W) (T, error)) ([]T, error) {
	raw, err := c.do(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var wire envelope[[]W]
	if err := decodeJSON(raw, &wire); err != nil {
		return nil, err
	}
	items := make([]T, 0, len(wire.Data))
	for i, entry := range wire.Data {
		item, err := convert(entry)
		if err != nil {
			return nil, fmt.Errorf("%w: %s entry %d: %s", ErrInvalidResponse, path, i, redactSensitiveText(err.Error()))
		}
		items = append(items, item)
	}
	return items, nil
}

// Reload restarts the background server, then asks it to rebuild every loaded
// location; a failure is an error, never a silent success. A location reload
// alone is not enough after a deployment: a server that loaded plugins before
// their dependencies were installed keeps failing to resolve them until its
// process is replaced.
func (c *Client) Reload(ctx context.Context) error {
	if _, err := c.invoke(ctx, "service restart", []string{"service", "restart"}); err != nil {
		return err
	}
	_, err := c.do(ctx, "POST", "/api/location/reload", nil)
	return err
}

// Handshake proves the local OpenCode is functional for Takt, walking the
// failure ladder from binary to API to version to model routes; the error
// type names the layer that failed.
func (c *Client) Handshake(ctx context.Context) (Handshake, error) {
	info, err := c.Info(ctx)
	if err != nil {
		return Handshake{}, err
	}
	major, err := parseMajor(info.Version)
	if err != nil {
		return Handshake{}, fmt.Errorf("%w: %s", ErrInvalidResponse, redactSensitiveText(err.Error()))
	}
	if major < MinimumMajor {
		return Handshake{}, fmt.Errorf(
			"%w: server reports %q but Takt needs OpenCode v%d or newer for the V2 API",
			ErrVersion, info.Version, MinimumMajor,
		)
	}
	// The model routes are the V2 mechanism Takt needs right now (R1/R2).
	// Any failure there — unreachable, 503, drift — collapses into
	// ErrUnavailable so consumers see one story: "not functional yet".
	if _, err := c.Models(ctx); err != nil {
		return Handshake{}, fmt.Errorf("%w: model routes probe failed: %s", ErrUnavailable, redactSensitiveText(err.Error()))
	}
	return Handshake{Version: info.Version, Major: major}, nil
}

// parseMajor reads the leading integer of the first version-like token, so a
// prefix such as "opencode " or a build suffix never changes the answer.
func parseMajor(version string) (int, error) {
	for field := range strings.FieldsSeq(version) {
		digits := strings.TrimPrefix(field, "v")
		major, _, _ := strings.Cut(digits, ".")
		if n, err := strconv.Atoi(major); err == nil {
			return n, nil
		}
	}
	return 0, fmt.Errorf("no version found in %q", redactSensitiveText(strings.TrimSpace(version)))
}

// envelope is the location wrapper the experimental /api/* routes put around
// their payloads: {"location": {"directory": ...}, "data": <payload>}.
type envelope[T any] struct {
	Data T `json:"data"`
}

// decodeJSON unmarshals leniently on purpose: unknown fields are ignored so
// additive changes in OpenCode never break Takt. Strictness is reserved for
// the fields consumers actually branch on, validated in the wire types.
func decodeJSON(raw []byte, target any) error {
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("%w: empty response body", ErrInvalidResponse)
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	return nil
}
