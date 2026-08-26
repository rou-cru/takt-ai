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

package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/codex"
	"github.com/rou-cru/takt-ai/takt/model"
)

// NativeSubAgentContent is target-neutral content plus target-native options.
// Model assignments are deliberately absent; they come from the semantic
// catalog when JoinNativeContent is called.
type NativeSubAgentContent struct {
	ID               string            `json:"id"`
	Description      string            `json:"description"`
	Instructions     string            `json:"instructions"`
	ClaudeTools      []string          `json:"claude_tools"`
	CodexSandboxMode codex.SandboxMode `json:"codex_sandbox_mode"`
	CodexWebSearch   codex.WebSearch   `json:"codex_web_search"`
}

// NativeSubAgent is validated native content joined to its catalog assignment.
type NativeSubAgent struct {
	ID               string
	Description      string
	Instructions     string
	ClaudeTools      []string
	CodexSandboxMode codex.SandboxMode
	CodexWebSearch   codex.WebSearch
	Assignments      map[model.AgentID]model.ModelAssignment
}

// JoinNativeContentForTargets validates native options for each target and joins
// content to catalog assignments in catalog order. The default sub-agent entry is
// intentionally not renderable.
func JoinNativeContentForTargets(catalog []model.CanonicalSubAgent, content []NativeSubAgentContent, targets []model.AgentID) ([]NativeSubAgent, error) {
	if len(catalog) == 0 {
		return nil, fmt.Errorf("catalog is empty")
	}
	selected, err := validateNativeTargets(targets)
	if err != nil {
		return nil, err
	}
	catalogByID, err := indexCatalogDefinitions(catalog)
	if err != nil {
		return nil, err
	}
	contentByID, err := indexNativeContent(content, selected, catalogByID)
	if err != nil {
		return nil, err
	}
	return joinNativeContent(catalog, contentByID)
}

func indexCatalogDefinitions(catalog []model.CanonicalSubAgent) (map[string]model.CanonicalSubAgent, error) {
	catalogByID := make(map[string]model.CanonicalSubAgent, len(catalog))
	for _, definition := range catalog {
		if strings.TrimSpace(definition.Name) == "" {
			return nil, fmt.Errorf("catalog entry has empty id")
		}
		if _, exists := catalogByID[definition.Name]; exists {
			return nil, fmt.Errorf("duplicate catalog id %q", definition.Name)
		}
		if _, ok := definition.Assignments[model.AgentClaudeCode]; !ok {
			return nil, fmt.Errorf("catalog entry %q has no Claude assignment", definition.Name)
		}
		if _, ok := definition.Assignments[model.AgentCodex]; !ok {
			return nil, fmt.Errorf("catalog entry %q has no Codex assignment", definition.Name)
		}
		catalogByID[definition.Name] = definition
	}
	return catalogByID, nil
}

func indexNativeContent(content []NativeSubAgentContent, selected map[model.AgentID]struct{}, catalogByID map[string]model.CanonicalSubAgent) (map[string]NativeSubAgentContent, error) {
	contentByID := make(map[string]NativeSubAgentContent, len(content))
	for _, entry := range content {
		if err := validateNativeContent(entry, selected); err != nil {
			return nil, err
		}
		if entry.ID == defaultSubAgent {
			return nil, fmt.Errorf("native content for %q is not renderable", defaultSubAgent)
		}
		if _, ok := catalogByID[entry.ID]; !ok {
			return nil, fmt.Errorf("native content id %q is not in the catalog", entry.ID)
		}
		if _, exists := contentByID[entry.ID]; exists {
			return nil, fmt.Errorf("duplicate native content id %q", entry.ID)
		}
		entry.ClaudeTools = append([]string(nil), entry.ClaudeTools...)
		sort.Strings(entry.ClaudeTools)
		contentByID[entry.ID] = entry
	}
	return contentByID, nil
}

func joinNativeContent(catalog []model.CanonicalSubAgent, contentByID map[string]NativeSubAgentContent) ([]NativeSubAgent, error) {
	joined := make([]NativeSubAgent, 0, len(catalog)-1)
	for _, definition := range catalog {
		if definition.Name == defaultSubAgent {
			continue
		}
		entry, ok := contentByID[definition.Name]
		if !ok {
			return nil, fmt.Errorf("missing native content for catalog id %q", definition.Name)
		}
		joined = append(joined, NativeSubAgent{
			ID:               entry.ID,
			Description:      entry.Description,
			Instructions:     entry.Instructions,
			ClaudeTools:      append([]string(nil), entry.ClaudeTools...),
			CodexSandboxMode: entry.CodexSandboxMode,
			CodexWebSearch:   entry.CodexWebSearch,
			Assignments:      cloneAssignments(definition.Assignments),
		})
	}
	return joined, nil
}

// validateNativeTargets validates the requested native targets and returns them as a set.
func validateNativeTargets(targets []model.AgentID) (map[model.AgentID]struct{}, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("at least one native target is required")
	}
	selected := make(map[model.AgentID]struct{}, len(targets))
	for _, target := range targets {
		if target != model.AgentClaudeCode && target != model.AgentCodex && target != model.AgentOpenCode {
			return nil, fmt.Errorf("unsupported native target %q", target)
		}
		if _, exists := selected[target]; exists {
			return nil, fmt.Errorf("duplicate native target %q", target)
		}
		selected[target] = struct{}{}
	}
	return selected, nil
}

// validateNativeContent validates native sub-agent content and target-specific Claude Code and Codex options.
func validateNativeContent(entry NativeSubAgentContent, targets map[model.AgentID]struct{}) error {
	if entry.ID == "" || entry.ID != strings.TrimSpace(entry.ID) {
		return fmt.Errorf("native content id must be non-empty and trimmed: %q", entry.ID)
	}
	if strings.TrimSpace(entry.Description) == "" {
		return fmt.Errorf("native content %q description is required", entry.ID)
	}
	if strings.TrimSpace(entry.Instructions) == "" {
		return fmt.Errorf("native content %q instructions are required", entry.ID)
	}
	if _, selected := targets[model.AgentClaudeCode]; selected {
		if err := validateClaudeTools(entry.ID, entry.ClaudeTools); err != nil {
			return err
		}
	}
	if _, selected := targets[model.AgentCodex]; selected {
		if !codex.ValidSandboxMode(entry.CodexSandboxMode) {
			return fmt.Errorf("native content %q has invalid Codex sandbox %q", entry.ID, entry.CodexSandboxMode)
		}
		if !codex.ValidWebSearch(entry.CodexWebSearch) {
			return fmt.Errorf("native content %q has invalid Codex web search %q", entry.ID, entry.CodexWebSearch)
		}
	}
	return nil
}

func validateClaudeTools(id string, tools []string) error {
	seenTools := make(map[string]struct{}, len(tools))
	for _, tool := range tools {
		if tool == "" || tool != strings.TrimSpace(tool) {
			return fmt.Errorf("native content %q has an invalid Claude tool", id)
		}
		if _, exists := seenTools[tool]; exists {
			return fmt.Errorf("native content %q has duplicate Claude tool %q", id, tool)
		}
		seenTools[tool] = struct{}{}
	}
	return nil
}

// cloneAssignments creates a shallow copy of a model-assignment map.
func cloneAssignments(assignments map[model.AgentID]model.ModelAssignment) map[model.AgentID]model.ModelAssignment {
	clone := make(map[model.AgentID]model.ModelAssignment, len(assignments))
	for agent, assignment := range assignments {
		clone[agent] = assignment
	}
	return clone
}
