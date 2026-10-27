// Copyright (C) 2025 Takt AI Contributors
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package setup

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/internal/filemerge"
)

// Config preservation: when Takt deploys a config artifact onto a file the
// user already had before any Takt install (no ownership-manifest entry), the
// deployed content is merged over the existing bytes instead of replacing
// them. Files Takt owns keep replace semantics.

// isMergeableConfig reports whether a pre-existing unmanaged file is merged
// instead of replaced. OpenCode only deploys JSON configs, so JSON objects are
// the only merge format.
func isMergeableConfig(path string) bool {
	return strings.HasSuffix(path, ".json")
}

// carryInjectedMCPServers copies the MCP servers already injected on disk into
// a rendered opencode.json artifact. The rendered content never holds them, so
// without this a partial redeploy (e.g. a model reassignment) would silently
// drop memory and codegraph until the next full install.
func carryInjectedMCPServers(rootDir string, artifact Artifact) (Artifact, error) {
	if artifact.Path != opencode.ConfigPath() {
		return artifact, nil
	}
	existing, err := filemerge.ReadRegularFile(filepath.Join(rootDir, filepath.FromSlash(artifact.Path)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return artifact, nil
		}
		return Artifact{}, fmt.Errorf("read existing config %q: %w", artifact.Path, err)
	}
	var current struct {
		MCP struct {
			Servers map[string]json.RawMessage `json:"servers"`
		} `json:"mcp"`
	}
	if json.Unmarshal(existing, &current) != nil {
		return artifact, nil
	}
	carried := map[string]any{}
	for _, name := range injectedMCPServers {
		if server, ok := current.MCP.Servers[name]; ok {
			carried[name] = server
		}
	}
	if len(carried) == 0 {
		return artifact, nil
	}
	overlay, err := json.Marshal(map[string]any{"mcp": map[string]any{"servers": carried}})
	if err != nil {
		return Artifact{}, err
	}
	merged, err := filemerge.MergeJSONObjects(artifact.Content, overlay)
	if err != nil {
		return Artifact{}, fmt.Errorf("carry injected MCP servers into %q: %w", artifact.Path, err)
	}
	artifact.Content = merged
	return artifact, nil
}

// patchAgentModels returns existing with only the model of each listed agent
// replaced by the one in rendered (or removed when rendered carries none).
// Every other value in existing is kept, so a reassignment never discards what
// the user added; the file is re-encoded, so its comments and key order are
// not preserved (the caller backs the edited bytes up first). Comments and
// trailing commas in existing parse as in MergeJSONObjects. It fails, leaving the caller
// to keep the file as is, when either side is not a JSON object or an agent is
// missing from existing or null.
func patchAgentModels(existing, rendered []byte, ids []string) ([]byte, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(filemerge.NormalizeJSON(existing), &top); err != nil {
		return nil, fmt.Errorf("parse existing config: %w", err)
	}
	disk, err := agentEntries(top)
	if err != nil {
		return nil, fmt.Errorf("existing config: %w", err)
	}
	var renderedTop map[string]json.RawMessage
	if err := json.Unmarshal(rendered, &renderedTop); err != nil {
		return nil, fmt.Errorf("parse rendered config: %w", err)
	}
	fresh, err := agentEntries(renderedTop)
	if err != nil {
		return nil, fmt.Errorf("rendered config: %w", err)
	}
	for _, id := range ids {
		agent, ok := disk[id]
		if !ok || agent == nil {
			return nil, fmt.Errorf("agent %q is not an object in the existing config", id)
		}
		if value, ok := fresh[id]["model"]; ok {
			agent["model"] = value
		} else {
			delete(agent, "model")
		}
	}
	agents, err := json.Marshal(disk)
	if err != nil {
		return nil, fmt.Errorf("encode agents: %w", err)
	}
	top["agents"] = agents
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false) // keep user strings byte-faithful
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(top); err != nil {
		return nil, fmt.Errorf("encode config: %w", err)
	}
	return out.Bytes(), nil
}

// agentEntries decodes the "agents" object of a config into per-agent raw fields.
func agentEntries(top map[string]json.RawMessage) (map[string]map[string]json.RawMessage, error) {
	raw, ok := top["agents"]
	if !ok {
		return nil, errors.New(`no "agents" object`)
	}
	var agents map[string]map[string]json.RawMessage
	if err := json.Unmarshal(raw, &agents); err != nil {
		return nil, fmt.Errorf(`parse "agents": %w`, err)
	}
	return agents, nil
}

// configPatch is a reassignment applied surgically to a config the user edited.
type configPatch struct {
	path    string
	mode    os.FileMode
	current []byte
	content []byte
}

// planEditedConfigPatch returns the surgical patch for the managed OpenCode
// config when it no longer matches what Takt last deployed (the user edited
// it), or nil when the normal redeploy is safe: the file is untouched, missing
// or unmanaged. It reads only, so a failure aborts before anything is applied.
func planEditedConfigPatch(rootDir string, after []TargetPlan, changed map[string]bool, ids []string) (*configPatch, error) {
	path := opencode.ConfigPath()
	if !changed[path] {
		return nil, nil
	}
	manifest, err := loadOrCreateManifest(rootDir)
	if err != nil {
		return nil, err
	}
	entry, managed := manifest.Entries[path]
	if !managed {
		return nil, nil
	}
	full := filepath.Join(rootDir, filepath.FromSlash(path))
	current, err := filemerge.ReadRegularFile(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read existing config %q: %w", path, err)
	}
	if hashOf(current) == entry.SHA256 {
		return nil, nil
	}
	var rendered []byte
	for _, plan := range after {
		for _, artifact := range plan.Artifacts {
			if artifact.Path == path {
				rendered = artifact.Content
			}
		}
	}
	if rendered == nil {
		return nil, fmt.Errorf("reassign models in edited %q: the deployment renders no such file", path)
	}
	content, err := patchAgentModels(current, rendered, ids)
	if err != nil {
		return nil, fmt.Errorf("reassign models in edited %q (file left untouched): %w", path, err)
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, fmt.Errorf("inspect existing config %q: %w", path, err)
	}
	return &configPatch{path: path, mode: info.Mode().Perm(), current: current, content: content}, nil
}

// apply backs up the edited bytes and writes the patch atomically. The
// ownership entry is left alone so the file stays flagged as user-edited for
// install, sync and any other flow that must ask before replacing it.
func (p *configPatch) apply(rootDir string) error {
	if _, err := backupContent(rootDir, p.path, p.current, p.mode); err != nil {
		return err
	}
	_, err := filemerge.WriteFileAtomic(filepath.Join(rootDir, filepath.FromSlash(p.path)), p.content, p.mode)
	return err
}

// mergePreexistingConfig returns artifact with its content merged over an
// existing unmanaged file beneath rootDir, or unchanged when there is nothing
// to merge.
func mergePreexistingConfig(rootDir string, artifact Artifact, manifest *OwnershipManifest) (Artifact, error) {
	if !isMergeableConfig(artifact.Path) {
		return artifact, nil
	}
	if _, managed := manifest.Entries[artifact.Path]; managed {
		return artifact, nil
	}
	existing, err := filemerge.ReadRegularFile(filepath.Join(rootDir, filepath.FromSlash(artifact.Path)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return artifact, nil
		}
		return Artifact{}, fmt.Errorf("read existing config %q: %w", artifact.Path, err)
	}
	if bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(artifact.Content)) || len(bytes.TrimSpace(existing)) == 0 {
		return artifact, nil
	}
	merged, err := filemerge.MergeJSONObjects(existing, artifact.Content)
	var invalid *filemerge.InvalidJSONError
	if errors.As(err, &invalid) {
		// The user chose to replace a file Takt cannot merge into; the
		// takeover backup keeps its original bytes.
		return artifact, nil
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("merge config %q: %w", artifact.Path, err)
	}
	artifact.Content = merged
	return artifact, nil
}
