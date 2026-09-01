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

package shared

import (
	"fmt"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
)

// Artifact is a filesystem-free agent projection. Path is relative to the
// user's home directory and Content is ready for a later deployer to write.
// All three target adapters (Claude, Codex, OpenCode) share this identical
// definition.
type Artifact struct {
	Path    string
	Content []byte
}

// Context7RemoteURL is the canonical context7 remote MCP endpoint deployed
// into agent configs by the context7 component.
const Context7RemoteURL = "https://mcp.context7.com/mcp"

// RenderGlobalPrompt returns the native global prompt artifact for the given
// RenderGlobalPrompt creates a global prompt artifact with the specified path and content.
// It requires non-empty content and ensures the artifact content ends with a newline.
func RenderGlobalPrompt(target, path, content string) (Artifact, error) {
	if strings.TrimSpace(content) == "" {
		return Artifact{}, fmt.Errorf("%s global prompt is required", target)
	}
	return Artifact{
		Path:    path,
		Content: []byte(artifacts.EnsureTrailingNewline(content)),
	}, nil
}

// ValidateSubAgentBase validates the fields common to all target
// sub-agent requests: ID, description, instructions, and model assignment.
// ValidateSubAgentBase validates the required fields shared by sub-agent definitions and applies target-specific model validation. It returns the first validation error encountered.
func ValidateSubAgentBase(target, id, description, instructions, model string, validateModel func(string, string) error) error {
	if err := artifacts.ValidateID(target, id); err != nil {
		return err
	}
	if strings.TrimSpace(description) == "" {
		return fmt.Errorf("%s sub-agent %q description is required", target, id)
	}
	if strings.TrimSpace(instructions) == "" {
		return fmt.Errorf("%s sub-agent %q instructions are required", target, id)
	}
	if err := validateModel(id, model); err != nil {
		return err
	}
	return nil
}
