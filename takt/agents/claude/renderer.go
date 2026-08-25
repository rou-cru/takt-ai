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

package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
	"github.com/rou-cru/takt-ai/takt/model"
)

// Artifact is a filesystem-free Claude projection. Path is relative to the
// user's home directory and Content is ready for a later deployer to write.
type Artifact struct {
	Path    string
	Content []byte
}

// SubAgentRequest contains the native data needed for one Claude agent file.
type SubAgentRequest struct {
	ID           string
	Description  string
	Instructions string
	Tools        []string
	Assignment   model.ModelAssignment
}

// Hook is one command in a Claude settings hook group.
type Hook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// HookGroup is one matcher and its ordered native hooks.
type HookGroup struct {
	Matcher string `json:"matcher,omitempty"`
	Hooks   []Hook `json:"hooks"`
}

// SettingsRequest contains caller-supplied native settings and hooks.
type SettingsRequest struct {
	Settings map[string]any         `json:"settings"`
	Hooks    map[string][]HookGroup `json:"hooks"`
}

// RenderSubAgent returns one deterministic Claude Markdown/frontmatter agent.
func RenderSubAgent(request SubAgentRequest) (Artifact, error) {
	if err := validateSubAgent(request); err != nil {
		return Artifact{}, err
	}

	var content bytes.Buffer
	content.WriteString("---\n")
	fmt.Fprintf(&content, "name: %s\n", request.ID)
	fmt.Fprintf(&content, "description: %s\n", strconv.Quote(request.Description))
	fmt.Fprintf(&content, "model: %s\n", request.Assignment.Model)
	if request.Assignment.Effort != "" {
		fmt.Fprintf(&content, "effort: %s\n", request.Assignment.Effort)
	}
	if len(request.Tools) > 0 {
		tools := append([]string(nil), request.Tools...)
		sort.Strings(tools)
		fmt.Fprintf(&content, "tools: %s\n", strings.Join(tools, ", "))
	}
	content.WriteString("---\n\n")
	content.WriteString(artifacts.EnsureTrailingNewline(request.Instructions))

	return Artifact{
		Path:    ".claude/agents/" + request.ID + ".md",
		Content: content.Bytes(),
	}, nil
}

// RenderSubAgents returns Claude agent artifacts sorted by deterministic path.
func RenderSubAgents(requests []SubAgentRequest) ([]Artifact, error) {
	rendered := make([]Artifact, 0, len(requests))
	for _, request := range requests {
		artifact, err := RenderSubAgent(request)
		if err != nil {
			return nil, err
		}
		rendered = append(rendered, artifact)
	}
	return artifacts.SortUniqueByPath(rendered, func(a Artifact) string { return a.Path }, "Claude")
}

// RenderGlobalPrompt returns the native Claude global prompt artifact.
func RenderGlobalPrompt(content string) (Artifact, error) {
	if strings.TrimSpace(content) == "" {
		return Artifact{}, fmt.Errorf("Claude global prompt is required")
	}
	return Artifact{
		Path:    ".claude/CLAUDE.md",
		Content: []byte(artifacts.EnsureTrailingNewline(content)),
	}, nil
}

// RenderSettings returns deterministic native Claude settings JSON.
func RenderSettings(request SettingsRequest) (Artifact, error) {
	settings := make(map[string]any, len(request.Settings)+1)
	for key, value := range request.Settings {
		if strings.TrimSpace(key) == "" {
			return Artifact{}, fmt.Errorf("Claude setting key is required")
		}
		settings[key] = value
	}
	if request.Hooks != nil {
		if _, exists := settings["hooks"]; exists {
			return Artifact{}, fmt.Errorf("Claude hooks must be provided through SettingsRequest.Hooks")
		}
		if err := validateHooks(request.Hooks); err != nil {
			return Artifact{}, err
		}
		settings["hooks"] = request.Hooks
	}

	content, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return Artifact{}, fmt.Errorf("marshal Claude settings: %w", err)
	}
	return Artifact{
		Path:    ".claude/settings.json",
		Content: append(content, '\n'),
	}, nil
}

func validateSubAgent(request SubAgentRequest) error {
	if err := artifacts.ValidateID("Claude", request.ID); err != nil {
		return err
	}
	if strings.TrimSpace(request.Description) == "" {
		return fmt.Errorf("Claude sub-agent %q description is required", request.ID)
	}
	if strings.TrimSpace(request.Instructions) == "" {
		return fmt.Errorf("Claude sub-agent %q instructions are required", request.ID)
	}
	if _, err := model.ValidateClaudeModelAssignment(request.ID, request.Assignment); err != nil {
		return err
	}
	return nil
}

func validateHooks(hooks map[string][]HookGroup) error {
	for event, groups := range hooks {
		if strings.TrimSpace(event) == "" {
			return fmt.Errorf("Claude hook event is required")
		}
		for index, group := range groups {
			if len(group.Hooks) == 0 {
				return fmt.Errorf("Claude hook event %q group %d has no hooks", event, index)
			}
			for hookIndex, hook := range group.Hooks {
				if strings.TrimSpace(hook.Type) == "" || strings.TrimSpace(hook.Command) == "" {
					return fmt.Errorf("Claude hook event %q group %d hook %d is incomplete", event, index, hookIndex)
				}
			}
		}
	}
	return nil
}
