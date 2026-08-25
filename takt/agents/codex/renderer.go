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
	"strconv"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
	"github.com/rou-cru/takt-ai/takt/model"
)

// Artifact is a filesystem-free Codex projection. Path is relative to the
// user's home directory and Content is ready for a later deployer to write.
type Artifact struct {
	Path    string
	Content []byte
}

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

// ValidSandboxMode reports whether mode is a native Codex sandbox mode.
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
}

// RenderSubAgent returns one deterministic Codex TOML custom-agent file.
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
	rendered := make([]Artifact, 0, len(requests))
	for _, request := range requests {
		artifact, err := RenderSubAgent(request)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, artifact)
	}
	return artifacts.SortUniqueByPath(rendered, func(a Artifact) string { return a.Path }, "Codex")
}

// RenderGlobalPrompt returns the native Codex global prompt artifact.
func RenderGlobalPrompt(content string) (Artifact, error) {
	if strings.TrimSpace(content) == "" {
		return Artifact{}, fmt.Errorf("Codex global prompt is required")
	}
	return Artifact{
		Path:    ".codex/AGENTS.md",
		Content: []byte(artifacts.EnsureTrailingNewline(content)),
	}, nil
}

// RenderConfig returns the native Codex config.toml projection.
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
	fmt.Fprintf(&content, "web_search = %s\n\n", tomlString(string(request.WebSearch)))
	content.WriteString("[features]\n")
	fmt.Fprintf(&content, "multi_agent = %t\n\n", request.MultiAgent)
	content.WriteString("[agents]\n")
	fmt.Fprintf(&content, "max_threads = %d\n", request.MaxThreads)
	fmt.Fprintf(&content, "max_depth = %d\n", request.MaxDepth)

	return Artifact{
		Path:    ".codex/config.toml",
		Content: content.Bytes(),
	}, nil
}

func validateSubAgent(request SubAgentRequest) error {
	if err := artifacts.ValidateID("Codex", request.ID); err != nil {
		return err
	}
	if strings.TrimSpace(request.Description) == "" {
		return fmt.Errorf("Codex sub-agent %q description is required", request.ID)
	}
	if strings.TrimSpace(request.Instructions) == "" {
		return fmt.Errorf("Codex sub-agent %q instructions are required", request.ID)
	}
	if _, err := model.ValidateCodexModelAssignment(request.ID, request.Assignment); err != nil {
		return err
	}
	if err := validateSandboxMode(request.SandboxMode); err != nil {
		return err
	}
	return validateWebSearch(request.WebSearch)
}

func validateSandboxMode(mode SandboxMode) error {
	if !ValidSandboxMode(mode) {
		return fmt.Errorf("invalid Codex sandbox mode %q", mode)
	}
	return nil
}

func validateWebSearch(search WebSearch) error {
	if !ValidWebSearch(search) {
		return fmt.Errorf("invalid Codex web search %q", search)
	}
	return nil
}

func tomlString(value string) string {
	return strconv.Quote(value)
}

func ensureTrailingNewline(content string) string {
	if strings.HasSuffix(content, "\n") {
		return content
	}
	return content + "\n"
}
