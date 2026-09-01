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

	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/internal/artifacts"
	"github.com/rou-cru/takt-ai/takt/model"
)

// Artifact is a filesystem-free Claude projection. Path is relative to the
// user's home directory and Content is ready for a later deployer to write.
type Artifact = shared.Artifact

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
	Settings    map[string]any         `json:"settings"`
	Hooks       map[string][]HookGroup `json:"hooks"`
	Permissions *Permissions           `json:"permissions,omitempty"`
}

// RenderSubAgent returns one Claude Markdown agent file with YAML frontmatter.
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
	return shared.RenderSorted(requests, RenderSubAgent, func(a Artifact) string { return a.Path }, "Claude")
}

// RenderGlobalPrompt returns the native Claude global prompt artifact.
func RenderGlobalPrompt(content string) (Artifact, error) {
	return shared.RenderGlobalPrompt("claude", ".claude/CLAUDE.md", content)
}

// RenderSettings returns the native Claude settings.json artifact.
func RenderSettings(request SettingsRequest) (Artifact, error) {
	settings := make(map[string]any, len(request.Settings)+1)
	for key, value := range request.Settings {
		if strings.TrimSpace(key) == "" {
			return Artifact{}, fmt.Errorf("claude setting key is required")
		}
		settings[key] = value
	}
	if request.Hooks != nil {
		if _, exists := settings["hooks"]; exists {
			return Artifact{}, fmt.Errorf("claude hooks must be provided through SettingsRequest.Hooks")
		}
		if err := validateHooks(request.Hooks); err != nil {
			return Artifact{}, err
		}
		settings["hooks"] = request.Hooks
	}
	if request.Permissions != nil {
		if _, exists := settings["permissions"]; exists {
			return Artifact{}, fmt.Errorf("claude permissions must be provided through SettingsRequest.Permissions")
		}
		settings["permissions"] = request.Permissions
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

// validateSubAgent validates a Claude sub-agent request.
func validateSubAgent(request SubAgentRequest) error {
	return shared.ValidateSubAgentBase("Claude", request.ID, request.Description, request.Instructions, request.Assignment.Model, func(id, m string) error {
		_, err := model.ValidateClaudeModelAssignment(id, request.Assignment)
		return err
	})
}

// validateHooks validates Claude hook events and groups.
func validateHooks(hooks map[string][]HookGroup) error {
	for event, groups := range hooks {
		if strings.TrimSpace(event) == "" {
			return fmt.Errorf("claude hook event is required")
		}
		if err := validateHookGroups(event, groups); err != nil {
			return err
		}
	}
	return nil
}

func validateHookGroups(event string, groups []HookGroup) error {
	for index, group := range groups {
		if len(group.Hooks) == 0 {
			return fmt.Errorf("claude hook event %q group %d has no hooks", event, index)
		}
		if err := validateGroupHooks(event, index, group.Hooks); err != nil {
			return err
		}
	}
	return nil
}

func validateGroupHooks(event string, groupIndex int, hooks []Hook) error {
	for hookIndex, hook := range hooks {
		if strings.TrimSpace(hook.Type) == "" || strings.TrimSpace(hook.Command) == "" {
			return fmt.Errorf("claude hook event %q group %d hook %d is incomplete", event, groupIndex, hookIndex)
		}
	}
	return nil
}
