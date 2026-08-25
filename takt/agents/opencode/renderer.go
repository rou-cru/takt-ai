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
	"strconv"
	"strings"

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
}

// It returns a validation error when the request is invalid.
func RenderSubAgent(request SubAgentRequest) (Artifact, error) {
	if err := validateSubAgent(request); err != nil {
		return Artifact{}, err
	}

	var content bytes.Buffer
	content.WriteString("---\n")
	content.WriteString("mode: subagent\n")
	fmt.Fprintf(&content, "description: %s\n", strconv.Quote(request.Description))
	fmt.Fprintf(&content, "model: %s\n", request.Assignment.Model)
	content.WriteString("---\n\n")
	content.WriteString(artifacts.EnsureTrailingNewline(request.Instructions))

	return Artifact{
		Path:    ".config/opencode/agents/" + request.ID + ".md",
		Content: content.Bytes(),
	}, nil
}

// RenderSubAgents returns OpenCode agent artifacts sorted by deterministic path.
func RenderSubAgents(requests []SubAgentRequest) ([]Artifact, error) {
	rendered := make([]Artifact, 0, len(requests))
	for _, request := range requests {
		artifact, err := RenderSubAgent(request)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, artifact)
	}
	return artifacts.SortUniqueByPath(rendered, func(a Artifact) string { return a.Path }, "OpenCode")
}

// RenderConfig creates the OpenCode global configuration artifact for the assigned model. It returns an error when the model assignment is blank or the configuration cannot be serialized.
func RenderConfig(request ConfigRequest) (Artifact, error) {
	if strings.TrimSpace(request.Assignment.Model) == "" {
		return Artifact{}, fmt.Errorf("OpenCode config requires a model assignment")
	}
	config := map[string]string{
		"$schema": "https://opencode.ai/config.json",
		"model":   request.Assignment.Model,
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

// validateSubAgent validates the identifier, description, instructions, and model assignment for an OpenCode sub-agent.
// It returns an error identifying the first invalid or missing field.
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
