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

// Package prompts is the single canonical registry of runtime prompts,
// tool descriptions, output directives, and agent-facing refusal guidance
// injected by the Takt harness.
package prompts

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"sync"

	"gopkg.in/yaml.v2"
)

//go:embed assets/*.yaml
var assetsFS embed.FS

// AssetFS exposes the embedded harness prompt YAML files without the assets/ prefix.
func AssetFS() fs.FS {
	sub, _ := fs.Sub(assetsFS, "assets")
	return sub
}

// Surface classifies the runtime mechanism through which a harness prompt reaches an LLM.
type Surface string

const (
	SurfaceSystemClause        Surface = "system_clause"
	SurfaceContextInjection    Surface = "context_injection"
	SurfaceInterlocutorTurn    Surface = "interlocutor_turn"
	SurfaceToolContract        Surface = "tool_contract"
	SurfaceToolOutputDirective Surface = "tool_output_directive"
	SurfaceRefusalGuidance     Surface = "refusal_guidance"
)

var validSurfaces = []Surface{
	SurfaceSystemClause,
	SurfaceContextInjection,
	SurfaceInterlocutorTurn,
	SurfaceToolContract,
	SurfaceToolOutputDirective,
	SurfaceRefusalGuidance,
}

// ValidOwners lists the harness components permitted to own prompt entries.
var ValidOwners = []string{
	"opencode/layout",
	"cli/dispatch",
	"memory/mcp",
	"plugin/takt-vfs",
	"plugin/takt-memory",
	"plugin/takt-dag",
}

// Entry is one runtime prompt, directive, or refusal template.
type Entry struct {
	ID             string   `yaml:"id"`
	Surface        Surface  `yaml:"surface"`
	Owner          string   `yaml:"owner"`
	Audience       []string `yaml:"audience"`
	CorrelatesWith []string `yaml:"correlates_with"`
	Vars           []string `yaml:"vars,omitempty"`
	Text           string   `yaml:"text"`
}

// ToolSpec groups a harness-registered tool's description and parameter descriptions.
type ToolSpec struct {
	Name           string            `yaml:"name"`
	Owner          string            `yaml:"owner"`
	Audience       []string          `yaml:"audience"`
	CorrelatesWith []string          `yaml:"correlates_with"`
	Vars           []string          `yaml:"vars,omitempty"`
	Description    string            `yaml:"description,omitempty"`
	Params         map[string]string `yaml:"params,omitempty"`
}

// Registry holds all validated harness prompt entries and tool specs.
type Registry struct {
	Entries []Entry
	Tools   []ToolSpec

	byID   map[string]Entry
	byTool map[string]ToolSpec
}

type assetFile struct {
	Entries []Entry    `yaml:"entries,omitempty"`
	Tools   []ToolSpec `yaml:"tools,omitempty"`
}

var (
	placeholderPattern = regexp.MustCompile(`\{\{([a-z0-9_]+)\}\}`)
	varNamePattern     = regexp.MustCompile(`^[a-z0-9_]+$`)
	entryIDPattern     = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)+$`)
	toolNamePattern    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
)

var (
	defaultOnce sync.Once
	defaultReg  Registry
	defaultErr  error
)

// Default returns the validated embedded harness prompt registry.
func Default() Registry {
	defaultOnce.Do(func() {
		defaultReg, defaultErr = Load()
	})
	if defaultErr != nil {
		panic("load embedded Takt harness prompts: " + defaultErr.Error())
	}
	return defaultReg
}

// Load reads and validates the embedded harness prompt YAML files.
func Load() (Registry, error) {
	return LoadFS(AssetFS())
}

// LoadFS reads and validates all *.yaml files from fsys.
func LoadFS(fsys fs.FS) (Registry, error) {
	dirEntries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return Registry{}, fmt.Errorf("read prompts dir: %w", err)
	}
	reg := Registry{
		byID:   make(map[string]Entry),
		byTool: make(map[string]ToolSpec),
	}
	yamlCount := 0
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".yaml") {
			continue
		}
		yamlCount++
		raw, err := fs.ReadFile(fsys, de.Name())
		if err != nil {
			return Registry{}, fmt.Errorf("read %s: %w", de.Name(), err)
		}
		var doc assetFile
		if err := yaml.UnmarshalStrict(raw, &doc); err != nil {
			return Registry{}, fmt.Errorf("parse %s: %w", de.Name(), err)
		}
		if len(doc.Entries) == 0 && len(doc.Tools) == 0 {
			return Registry{}, fmt.Errorf("%s: file declares no entries or tools", de.Name())
		}
		for _, e := range doc.Entries {
			if err := validateEntry(e); err != nil {
				return Registry{}, fmt.Errorf("%s: %w", de.Name(), err)
			}
			if _, dup := reg.byID[e.ID]; dup {
				return Registry{}, fmt.Errorf("%s: duplicate prompt id %q", de.Name(), e.ID)
			}
			reg.byID[e.ID] = e
			reg.Entries = append(reg.Entries, e)
		}
		for _, t := range doc.Tools {
			if err := validateTool(t); err != nil {
				return Registry{}, fmt.Errorf("%s: %w", de.Name(), err)
			}
			if _, dup := reg.byTool[t.Name]; dup {
				return Registry{}, fmt.Errorf("%s: duplicate tool spec %q", de.Name(), t.Name)
			}
			reg.byTool[t.Name] = t
			reg.Tools = append(reg.Tools, t)
		}
	}
	if yamlCount == 0 {
		return Registry{}, fmt.Errorf("no prompt YAML files found")
	}
	return reg, nil
}

func validateEntry(e Entry) error {
	if !entryIDPattern.MatchString(e.ID) {
		return fmt.Errorf("invalid prompt id %q", e.ID)
	}
	if !slices.Contains(validSurfaces, e.Surface) {
		return fmt.Errorf("prompt %q: invalid surface %q", e.ID, e.Surface)
	}
	if !slices.Contains(ValidOwners, e.Owner) {
		return fmt.Errorf("prompt %q: invalid owner %q", e.ID, e.Owner)
	}
	if err := validateMetadata(e.ID, e.Audience, e.CorrelatesWith); err != nil {
		return err
	}
	if e.Text == "" {
		return fmt.Errorf("prompt %q: empty text", e.ID)
	}
	return validateVars(e.ID, e.Vars, []string{e.Text})
}

func validateTool(t ToolSpec) error {
	if !toolNamePattern.MatchString(t.Name) {
		return fmt.Errorf("invalid tool name %q", t.Name)
	}
	if !slices.Contains(ValidOwners, t.Owner) {
		return fmt.Errorf("tool %q: invalid owner %q", t.Name, t.Owner)
	}
	if err := validateMetadata("tool "+t.Name, t.Audience, t.CorrelatesWith); err != nil {
		return err
	}
	if t.Description == "" && len(t.Params) == 0 {
		return fmt.Errorf("tool %q: must declare description or params", t.Name)
	}
	texts := make([]string, 0, 1+len(t.Params))
	if t.Description != "" {
		texts = append(texts, t.Description)
	}
	for param, desc := range t.Params {
		if strings.TrimSpace(param) == "" || desc == "" {
			return fmt.Errorf("tool %q: param %q has empty name or description", t.Name, param)
		}
		texts = append(texts, desc)
	}
	return validateVars("tool "+t.Name, t.Vars, texts)
}

func validateMetadata(label string, audience, correlates []string) error {
	if len(audience) == 0 {
		return fmt.Errorf("%s: audience must not be empty", label)
	}
	for _, a := range audience {
		if strings.TrimSpace(a) == "" {
			return fmt.Errorf("%s: blank audience entry", label)
		}
	}
	if len(correlates) == 0 {
		return fmt.Errorf("%s: correlates_with must not be empty", label)
	}
	for _, p := range correlates {
		if !fs.ValidPath(p) || !strings.HasSuffix(p, ".md") {
			return fmt.Errorf("%s: invalid correlates_with path %q", label, p)
		}
	}
	return nil
}

func validateVars(label string, declared []string, texts []string) error {
	declSet := make(map[string]bool, len(declared))
	for _, v := range declared {
		if !varNamePattern.MatchString(v) {
			return fmt.Errorf("%s: invalid var name %q", label, v)
		}
		if declSet[v] {
			return fmt.Errorf("%s: duplicate var %q", label, v)
		}
		declSet[v] = true
	}
	usedSet := make(map[string]bool)
	for _, text := range texts {
		for _, m := range placeholderPattern.FindAllStringSubmatch(text, -1) {
			name := m[1]
			if !declSet[name] {
				return fmt.Errorf("%s: undeclared placeholder {{%s}}", label, name)
			}
			usedSet[name] = true
		}
	}
	for _, v := range declared {
		if !usedSet[v] {
			return fmt.Errorf("%s: declared var %q is not used", label, v)
		}
	}
	return nil
}

// Entry looks up a prompt entry by ID.
func (r Registry) Entry(id string) (Entry, bool) {
	e, ok := r.byID[id]
	return e, ok
}

// Tool looks up a tool specification by name.
func (r Registry) Tool(name string) (ToolSpec, bool) {
	t, ok := r.byTool[name]
	return t, ok
}

// Render interpolates vars into the entry identified by id. It fails if id is
// unknown, if any declared variable is missing, or if an undeclared variable is passed.
func (r Registry) Render(id string, vars map[string]string) (string, error) {
	e, ok := r.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown prompt id %q", id)
	}
	return renderTemplate(id, e.Text, e.Vars, vars)
}

// MustRender is like Render but panics on error. Use only with compile-time known IDs.
func (r Registry) MustRender(id string, vars map[string]string) string {
	out, err := r.Render(id, vars)
	if err != nil {
		panic("render prompt " + id + ": " + err.Error())
	}
	return out
}

// RenderToolDescription interpolates vars into a tool's description.
func (r Registry) RenderToolDescription(name string, vars map[string]string) (string, error) {
	t, ok := r.byTool[name]
	if !ok {
		return "", fmt.Errorf("unknown tool %q", name)
	}
	if t.Description == "" {
		return "", fmt.Errorf("tool %q has no description", name)
	}
	return renderTemplate("tool "+name, t.Description, extractVars(t.Description), vars)
}

func extractVars(text string) []string {
	var out []string
	for _, m := range placeholderPattern.FindAllStringSubmatch(text, -1) {
		if !slices.Contains(out, m[1]) {
			out = append(out, m[1])
		}
	}
	return out
}

func renderTemplate(label, text string, expected []string, vars map[string]string) (string, error) {
	for k := range vars {
		if !slices.Contains(expected, k) {
			return "", fmt.Errorf("%s: unexpected variable %q", label, k)
		}
	}
	out := text
	for _, k := range expected {
		val, ok := vars[k]
		if !ok {
			return "", fmt.Errorf("%s: missing variable %q", label, k)
		}
		out = strings.ReplaceAll(out, "{{"+k+"}}", val)
	}
	return out, nil
}

// ToolDescriptionKey returns the flat bundle key for a tool's description.
func ToolDescriptionKey(tool string) string {
	return "tool." + tool + ".description"
}

// ToolParamKey returns the flat bundle key for a tool parameter's description.
func ToolParamKey(tool, param string) string {
	return "tool." + tool + ".param." + param
}

// BundleForOwner exports every prompt template owned by owner as a flat map
// suitable for JSON injection into an OpenCode plugin.
func (r Registry) BundleForOwner(owner string) map[string]string {
	bundle := make(map[string]string)
	for _, e := range r.Entries {
		if e.Owner == owner {
			bundle[e.ID] = e.Text
		}
	}
	for _, t := range r.Tools {
		if t.Owner != owner {
			continue
		}
		if t.Description != "" {
			bundle[ToolDescriptionKey(t.Name)] = t.Description
		}
		for param, desc := range t.Params {
			bundle[ToolParamKey(t.Name, param)] = desc
		}
	}
	return bundle
}
