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

package codex

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/model"
)

// Artifact is a filesystem-free Codex projection. Path is relative to the
// user's home directory and Content is ready for a later deployer to write.
type Artifact = shared.Artifact

// SandboxMode is the native Codex sandbox setting.
type SandboxMode string

const (
	SandboxReadOnly       SandboxMode = "read-only"
	SandboxWorkspaceWrite SandboxMode = "workspace-write"
)

// WebSearch is the native Codex web-search setting.
type WebSearch string

const (
	WebSearchDisabled WebSearch = "disabled"
	WebSearchCached   WebSearch = "cached"
	WebSearchLive     WebSearch = "live"
)

// ValidSandboxMode reports whether mode is a supported native Codex sandbox mode.
func ValidSandboxMode(mode SandboxMode) bool {
	return mode == SandboxReadOnly || mode == SandboxWorkspaceWrite
}

// ValidWebSearch reports whether search is a native Codex web-search setting.
func ValidWebSearch(search WebSearch) bool {
	return search == WebSearchDisabled || search == WebSearchCached || search == WebSearchLive
}

// SubAgentRequest contains the native data needed for one Codex agent file.
type SubAgentRequest struct {
	ID           string
	Description  string
	Instructions string
	Assignment   model.ModelAssignment
	SandboxMode  SandboxMode
	WebSearch    WebSearch
}

// ConfigRequest contains the native Codex global configuration projection.
type ConfigRequest struct {
	Assignment  model.ModelAssignment
	SandboxMode SandboxMode
	WebSearch   WebSearch
	MultiAgent  bool
	MaxThreads  int
	MaxDepth    int
	// Context7 adds the canonical context7 remote MCP server block.
	Context7 bool
	// Permissions adds the canonical takt-dev permission profile.
	Permissions bool
}

// RenderSubAgent returns one Codex TOML custom-agent file.
func RenderSubAgent(request SubAgentRequest) (Artifact, error) {
	if err := validateSubAgent(request); err != nil {
		return Artifact{}, err
	}

	var content bytes.Buffer
	fmt.Fprintf(&content, "name = %s\n", tomlString(request.ID))
	fmt.Fprintf(&content, "description = %s\n", tomlString(request.Description))
	fmt.Fprintf(&content, "model = %s\n", tomlString(request.Assignment.Model))
	fmt.Fprintf(&content, "model_reasoning_effort = %s\n", tomlString(request.Assignment.Effort))
	fmt.Fprintf(&content, "sandbox_mode = %s\n", tomlString(string(request.SandboxMode)))
	fmt.Fprintf(&content, "web_search = %s\n", tomlString(string(request.WebSearch)))
	fmt.Fprintf(&content, "developer_instructions = %s\n", tomlString(request.Instructions))

	return Artifact{
		Path:    ".codex/agents/" + request.ID + ".toml",
		Content: content.Bytes(),
	}, nil
}

// RenderSubAgents returns Codex agent artifacts sorted by deterministic path.
func RenderSubAgents(requests []SubAgentRequest) ([]Artifact, error) {
	return shared.RenderSorted(requests, RenderSubAgent, func(a Artifact) string { return a.Path }, "Codex")
}

// RenderGlobalPrompt returns the native Codex global prompt artifact.
func RenderGlobalPrompt(content string) (Artifact, error) {
	return shared.RenderGlobalPrompt("Codex", ".codex/AGENTS.md", content)
}

// RenderConfig returns the native Codex config.toml artifact.
func RenderConfig(request ConfigRequest) (Artifact, error) {
	if _, err := model.ValidateCodexModelAssignment("config", request.Assignment); err != nil {
		return Artifact{}, err
	}
	if err := validateSandboxMode(request.SandboxMode); err != nil {
		return Artifact{}, err
	}
	if err := validateWebSearch(request.WebSearch); err != nil {
		return Artifact{}, err
	}
	if request.MaxThreads <= 0 || request.MaxDepth <= 0 {
		return Artifact{}, fmt.Errorf("Codex agent limits must be positive")
	}

	var content bytes.Buffer
	fmt.Fprintf(&content, "model = %s\n", tomlString(request.Assignment.Model))
	fmt.Fprintf(&content, "model_reasoning_effort = %s\n", tomlString(request.Assignment.Effort))
	fmt.Fprintf(&content, "sandbox_mode = %s\n", tomlString(string(request.SandboxMode)))
	fmt.Fprintf(&content, "web_search = %s\n", tomlString(string(request.WebSearch)))
	if request.Permissions {
		content.WriteString("approval_policy = \"on-request\"\n")
		content.WriteString("default_permissions = " + tomlString(permissionsProfileName) + "\n")
	}
	content.WriteString("\n[features]\n")
	fmt.Fprintf(&content, "multi_agent = %t\n\n", request.MultiAgent)
	content.WriteString("[agents]\n")
	fmt.Fprintf(&content, "max_threads = %d\n", request.MaxThreads)
	fmt.Fprintf(&content, "max_depth = %d\n", request.MaxDepth)
	if request.Context7 {
		appendContext7Server(&content)
	}
	if request.Permissions {
		appendPermissionsProfile(&content)
	}

	return Artifact{
		Path:    ".codex/config.toml",
		Content: content.Bytes(),
	}, nil
}

// validateSubAgent validates the fields and Codex-specific settings in a sub-agent request.
func validateSubAgent(request SubAgentRequest) error {
	if err := shared.ValidateSubAgentBase("Codex", request.ID, request.Description, request.Instructions, request.Assignment.Model, func(id, m string) error {
		_, err := model.ValidateCodexModelAssignment(id, request.Assignment)
		return err
	}); err != nil {
		return err
	}
	if err := validateSandboxMode(request.SandboxMode); err != nil {
		return err
	}
	return validateWebSearch(request.WebSearch)
}

// validateSandboxMode validates a Codex sandbox mode.
func validateSandboxMode(mode SandboxMode) error {
	if !ValidSandboxMode(mode) {
		return fmt.Errorf("invalid Codex sandbox mode %q", mode)
	}
	return nil
}

// validateWebSearch validates a Codex web-search setting.
func validateWebSearch(search WebSearch) error {
	if !ValidWebSearch(search) {
		return fmt.Errorf("invalid Codex web search %q", search)
	}
	return nil
}

// tomlString quotes a string as a TOML basic string. TOML escaping differs
// from Go's strconv.Quote: only the escapes in the TOML spec are allowed
// (notably \xNN is invalid in TOML and must use \uXXXX instead).
func tomlString(value string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\b':
			b.WriteString(`\b`)
		case '\t':
			b.WriteString(`\t`)
		case '\n':
			b.WriteString(`\n`)
		case '\f':
			b.WriteString(`\f`)
		case '\r':
			b.WriteString(`\r`)
		default:
			// TOML requires control characters (U+0000-U+001F and U+007F-U+009F)
			// to be escaped; \uXXXX (not Go's \xNN) is the TOML form.
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
