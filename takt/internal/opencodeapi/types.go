// Copyright (C) 2025 Takt AI Contributors
// SPDX-License-Identifier: AGPL-3.0-or-later

package opencodeapi

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

// ModelCost is one price tier of a model, in USD per million tokens. A model
// can report several tiers; TierType is empty when the entry is untiered.
type ModelCost struct {
	TierType   string
	TierSize   int
	Input      float64
	Output     float64
	CacheRead  float64
	CacheWrite float64
}

// Model is the structured replacement for parsing "opencode models" text:
// identity, lifecycle status, tool support, limits and cost. Unknown API
// fields are ignored by design.
type Model struct {
	Ref          ModelRef
	Name         string
	Status       string // "alpha", "beta", "deprecated" or "active"
	Enabled      bool
	Tools        bool
	InputTypes   []string // e.g. ["text", "image"]
	OutputTypes  []string // e.g. ["text"]
	ContextLimit int
	OutputLimit  int
	ReleasedAt   int64 // milliseconds since the Unix epoch, as reported by the API
	Cost         []ModelCost
}

// MCPState is the connection state OpenCode reports for an MCP server. An
// unknown state fails parsing so verification never mistakes a half-connected
// server for a working one.
type MCPState string

// MCP states as reported by GET /api/mcp.
const (
	MCPConnected MCPState = "connected"
	MCPPending   MCPState = "pending"
	MCPDisabled  MCPState = "disabled"
	MCPFailed    MCPState = "failed"
	MCPNeedsAuth MCPState = "needs_auth"
)

// MCPServer is one configured MCP server with the connection state OpenCode
// actually observes — not what configuration files claim.
type MCPServer struct {
	Name          string
	State         MCPState
	Error         string // why the server failed or needs auth; empty when healthy
	IntegrationID string // set for remote servers registered as OAuth integrations
}

// Agent is one agent OpenCode has registered, including the model reference
// it is pinned to (nil when it inherits the session or default model).
type Agent struct {
	ID          string
	Name        string
	Description string
	Mode        string // "subagent", "primary" or "all"
	Hidden      bool
	Model       *ModelRef
}

// Skill is one skill OpenCode has discovered and made loadable.
type Skill struct {
	ID          string
	Name        string
	Description string
	Path        string
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
	// ModelRoutes reports that the experimental model routes answered — the
	// V2 mechanism Takt relies on. A v2 binary whose model routes fail is
	// installed but not functional for Takt, and that distinction is exactly
	// what the handshake exists to make.
	ModelRoutes bool
}
