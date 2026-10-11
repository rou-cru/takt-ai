// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencodeapi

import "time"

// Info is the identity of the running OpenCode server; only the fields the
// handshake needs are surfaced from the API payload.
type Info struct {
	Version string
}

// ModelRef names a model the way OpenCode does everywhere: provider plus
// model id, optionally pinned to a reasoning variant ("provider/model#variant").
type ModelRef struct {
	ProviderID string
	ModelID    string
	Variant    string // empty when the model is not pinned to a variant
}

// String renders the reference in OpenCode's canonical "provider/model" or
// "provider/model#variant" form, matching what "opencode models" prints and
// what configuration files accept.
func (r ModelRef) String() string {
	if r.Variant == "" {
		return r.ProviderID + "/" + r.ModelID
	}
	return r.ProviderID + "/" + r.ModelID + "#" + r.Variant
}

// Model is the structured replacement for parsing "opencode models" text:
// the identity plus the metadata the API states about the model. Anything the
// API leaves out stays zero or empty; nothing here is derived.
type Model struct {
	Ref ModelRef
	// Name is the human label OpenCode shows ("GPT-6 Luna Fast").
	Name string
	// Cost lists the price tiers per million tokens; empty when the API
	// reports no pricing for the model.
	Cost []CostTier
	// ContextLimit and OutputLimit are the token limits; InputLimit is zero
	// when the API gives no separate input cap.
	ContextLimit, InputLimit, OutputLimit int
	// Input names the modalities the model accepts ("text", "image", ...).
	Input []string
	// Variants names the reasoning variants the model offers, in API order.
	Variants []string
	// Released is the model's release date; zero when the API omits it.
	Released time.Time
}

// CostTier is one price tier in dollars per million tokens. AboveContext is
// zero for the base tier and the context size the tier starts at otherwise.
type CostTier struct {
	AboveContext                         int
	Input, Output, CacheRead, CacheWrite float64
}

// Provider is a model provider the server registered; Name is its display label.
type Provider struct {
	ID, Name string
}

// MCPState is the connection state OpenCode reports for an MCP server. An
// unknown state fails parsing so verification never mistakes a half-connected
// server for a working one.
type MCPState string

// MCP states as reported by GET /api/mcp.
const (
	// MCPConnected means the server responded to OpenCode.
	MCPConnected MCPState = "connected"
	// MCPPending means the server is still being brought up.
	MCPPending MCPState = "pending"
	// MCPDisabled means configuration turned the server off.
	MCPDisabled MCPState = "disabled"
	// MCPFailed means the server could not start or respond.
	MCPFailed MCPState = "failed"
	// MCPNeedsAuth means the server requires credentials before it connects.
	MCPNeedsAuth MCPState = "needs_auth"
)

// MCPServer is one configured MCP server with the connection state OpenCode
// actually observes — not what configuration files claim.
type MCPServer struct {
	Name  string
	State MCPState
	Error string // why the server failed or needs auth; empty when healthy
}

// Agent is one agent OpenCode has registered.
type Agent struct {
	ID     string
	Mode   string // "subagent", "primary" or "all"
	Hidden bool
}

// Skill is one skill OpenCode has discovered and made loadable.
type Skill struct {
	ID string
}

// Plugin is one server plugin with its loading result. Status is a decision
// input for verification, so an unknown status fails parsing instead of being
// mapped to something optimistic.
type Plugin struct {
	ID     string
	Type   string // "builtin", "package", "local" or "sdk"
	Status string // "active" or "failed"
	Error  string // why the plugin failed; empty when active
}

// Handshake is the outcome of checking that the local OpenCode is functional
// for Takt before anything is installed or mutated.
type Handshake struct {
	Version string
	Major   int
}
