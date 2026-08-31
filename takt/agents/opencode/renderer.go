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

package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
	"github.com/rou-cru/takt-ai/takt/model"
)

// Artifact is a filesystem-free OpenCode projection. Path is relative to the
// user's home directory and Content is ready for a later deployer to write.
type Artifact struct {
	Path    string
	Content []byte
}

// SubAgentRequest contains the native data needed for one OpenCode agent file.
type SubAgentRequest struct {
	ID           string
	Description  string
	Instructions string
	Assignment   model.ModelAssignment
}

// ConfigRequest contains the native OpenCode global configuration projection.
type ConfigRequest struct {
	Assignment model.ModelAssignment
	// Context7 adds the canonical context7 remote MCP server entry.
	Context7 bool
	// Permissions adds the canonical bash/read permission rules.
	Permissions bool
	// Theme sets the OpenCode theme when non-empty.
	Theme string
}

// RenderSubAgent returns one OpenCode Markdown agent file with frontmatter.
func RenderSubAgent(request SubAgentRequest) (Artifact, error) {
	if err := validateSubAgent(request); err != nil {
		return Artifact{}, err
	}

	var content bytes.Buffer
	content.WriteString("---\n")
	content.WriteString("mode: subagent\n")
	fmt.Fprintf(&content, "description: %s\n", yamlScalar(request.Description))
	fmt.Fprintf(&content, "model: %s\n", yamlScalar(request.Assignment.Model))
	content.WriteString("---\n\n")
	content.WriteString(artifacts.EnsureTrailingNewline(request.Instructions))

	return Artifact{
		Path:    ".config/opencode/agents/" + request.ID + ".md",
		Content: content.Bytes(),
	}, nil
}

// RenderSubAgents returns OpenCode agent artifacts sorted by deterministic path.
func RenderSubAgents(requests []SubAgentRequest) ([]Artifact, error) {
	return shared.RenderSorted(requests, RenderSubAgent, func(a Artifact) string { return a.Path }, "OpenCode")
}

// RenderConfig returns the native OpenCode opencode.json artifact.
func RenderConfig(request ConfigRequest) (Artifact, error) {
	if strings.TrimSpace(request.Assignment.Model) == "" {
		return Artifact{}, fmt.Errorf("OpenCode config requires a model assignment")
	}
	config := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"model":   request.Assignment.Model,
	}
	if request.Context7 {
		config["mcp"] = context7Config()
	}
	if request.Permissions {
		config["permission"] = permissionsConfig()
	}
	if request.Theme != "" {
		config["theme"] = request.Theme
	}
	content, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return Artifact{}, fmt.Errorf("marshal OpenCode config: %w", err)
	}
	return Artifact{
		Path:    ".config/opencode/opencode.json",
		Content: append(content, '\n'),
	}, nil
}

// yamlScalar returns s as a YAML double-quoted scalar. Double-quoted style is
// always safe: every special character (":", "#", leading/trailing spaces,
// double quotes, backslashes, control characters and newlines) is escaped so
// the value round-trips to the identical string when parsed back.
func yamlScalar(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case 0:
			b.WriteString(`\0`)
		default:
			if r < 0x20 {
				// Other control characters are not representable literally in a
				// double-quoted scalar, so emit the YAML \xNN escape.
				fmt.Fprintf(&b, `\x%02x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// validateSubAgent validates an OpenCode sub-agent request.
func validateSubAgent(request SubAgentRequest) error {
	if err := artifacts.ValidateID("OpenCode", request.ID); err != nil {
		return err
	}
	if strings.TrimSpace(request.Description) == "" {
		return fmt.Errorf("OpenCode sub-agent %q description is required", request.ID)
	}
	if strings.TrimSpace(request.Instructions) == "" {
		return fmt.Errorf("OpenCode sub-agent %q instructions are required", request.ID)
	}
	if strings.TrimSpace(request.Assignment.Model) == "" {
		return fmt.Errorf("OpenCode sub-agent %q has no model assignment", request.ID)
	}
	return nil
}
