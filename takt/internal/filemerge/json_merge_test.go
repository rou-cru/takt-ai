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

package filemerge

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMergeJSONObjectsRecursively(t *testing.T) {
	base := []byte(`{"plugins":["a"],"settings":{"theme":"default","flags":{"x":true}}}`)
	overlay := []byte(`{"settings":{"theme":"takt","flags":{"y":true}},"extra":1}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged json error = %v", err)
	}

	settings := got["settings"].(map[string]any)
	flags := settings["flags"].(map[string]any)

	if settings["theme"] != "takt" {
		t.Fatalf("theme = %v", settings["theme"])
	}

	if flags["x"] != true || flags["y"] != true {
		t.Fatalf("flags = %#v", flags)
	}

	plugins := got["plugins"].([]any)
	if len(plugins) != 1 || plugins[0] != "a" {
		t.Fatalf("plugins = %#v", plugins)
	}
}

func TestMergeJSONObjectsSupportsJSONCBase(t *testing.T) {
	base := []byte(`{
	  // VS Code-style comments and trailing commas
	  "editor.fontSize": 14,
	  "files.exclude": {
	    "**/.git": true,
	  },
	}`)
	overlay := []byte(`{"chat.tools.autoApprove": true}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged json error = %v", err)
	}

	autoApprove, ok := got["chat.tools.autoApprove"].(bool)
	if !ok || !autoApprove {
		t.Fatalf("chat.tools.autoApprove = %#v", got["chat.tools.autoApprove"])
	}

	if got["editor.fontSize"] != float64(14) {
		t.Fatalf("editor.fontSize = %v", got["editor.fontSize"])
	}
}

func TestMergeJSONObjectsMalformedBaseReturnsOverlayOnly(t *testing.T) {
	// Real user machines may have a malformed MCP config file.
	// The installer should recover by treating the broken base as {} and continuing.
	tests := []struct {
		name    string
		base    []byte
		overlay []byte
		wantKey string
	}{
		{
			name:    "base starting with letter",
			base:    []byte(`allow: all`),
			overlay: []byte(`{"mcpServers": {"context7": {"type": "remote"}}}`),
			wantKey: "mcpServers",
		},
		{
			name:    "unclosed json object",
			base:    []byte(`{"ok": true`),
			overlay: []byte(`{"chat.tools.autoApprove": true}`),
			wantKey: "chat.tools.autoApprove",
		},
		{
			name:    "arbitrary text",
			base:    []byte(`a`),
			overlay: []byte(`{"servers": {"engram": {"command": "engram"}}}`),
			wantKey: "servers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			merged, err := MergeJSONObjects(tt.base, tt.overlay)
			if err != nil {
				t.Fatalf("MergeJSONObjects() error = %v; want nil (malformed base should be treated as {})", err)
			}

			var got map[string]any
			if err := json.Unmarshal(merged, &got); err != nil {
				t.Fatalf("merged result is not valid JSON: %v", err)
			}

			if _, ok := got[tt.wantKey]; !ok {
				t.Fatalf("merged result missing key %q from overlay; got keys: %v", tt.wantKey, got)
			}
		})
	}
}

// ─── __replace__ sentinel tests ───────────────────────────────────────────────

func TestMergeJSONObjectsReplaceSentinelErasesBaseKeys(t *testing.T) {
	base := []byte(`{"mcp":{"engram":{"command":"/opt/homebrew/bin/engram","args":["mcp","--tools=agent"],"type":"local"}}}`)
	overlay := []byte(`{"mcp":{"engram":{"__replace__":{"command":["/opt/homebrew/bin/engram","mcp","--tools=agent"],"type":"local"}}}}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged error = %v", err)
	}

	mcp := got["mcp"].(map[string]any)
	eng := mcp["engram"].(map[string]any)

	// args must be gone
	if _, ok := eng["args"]; ok {
		t.Fatalf("engram still has 'args' after __replace__; got: %v", eng)
	}
	// command must be an array
	cmd, ok := eng["command"].([]any)
	if !ok {
		t.Fatalf("engram command is not an array; got: %T = %v", eng["command"], eng["command"])
	}
	if len(cmd) != 3 {
		t.Fatalf("engram command has %d elements, want 3", len(cmd))
	}
	// __replace__ must not appear in output
	if _, ok := eng["__replace__"]; ok {
		t.Fatal("__replace__ sentinel leaked into output")
	}
}

func TestMergeJSONObjectsReplaceSentinelNoBaseKey(t *testing.T) {
	base := []byte(`{}`)
	overlay := []byte(`{"a":{"__replace__":{"z":3}}}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged error = %v", err)
	}

	a, ok := got["a"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'a' to be a map; got: %T = %v", got["a"], got["a"])
	}
	if a["z"] != float64(3) {
		t.Fatalf("a.z = %v, want 3", a["z"])
	}
	if _, ok := a["__replace__"]; ok {
		t.Fatal("__replace__ sentinel leaked into output")
	}
}

func TestMergeJSONObjectsReplaceSentinelPreservesOtherKeys(t *testing.T) {
	base := []byte(`{"a":{"old":1},"b":"keep"}`)
	overlay := []byte(`{"a":{"__replace__":{"new":2}}}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged error = %v", err)
	}

	// "b" must survive untouched
	if got["b"] != "keep" {
		t.Fatalf("sibling key 'b' lost; got: %v", got)
	}

	a := got["a"].(map[string]any)
	// "old" must be gone (replaced atomically)
	if _, ok := a["old"]; ok {
		t.Fatalf("'a.old' survived __replace__; got: %v", a)
	}
	if a["new"] != float64(2) {
		t.Fatalf("a.new = %v, want 2", a["new"])
	}
}

func TestMergeJSONObjectsReplaceSentinelNotInOutput(t *testing.T) {
	base := []byte(`{"x":{"y":1}}`)
	overlay := []byte(`{"x":{"__replace__":{"z":2}}}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	if !json.Valid(merged) {
		t.Fatalf("merged output is not valid JSON: %s", string(merged))
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal error = %v", err)
	}

	// Walk all nested maps and ensure __replace__ never appears as a key
	var walk func(m map[string]any)
	walk = func(m map[string]any) {
		for k, v := range m {
			if k == "__replace__" {
				t.Fatalf("sentinel '__replace__' leaked into output: %v", m)
			}
			if sub, ok := v.(map[string]any); ok {
				walk(sub)
			}
		}
	}
	walk(got)
}

// ─── Issue #278: deep merge preserves stale wildcard permissions ──────────────

// TestMergeJSONObjects_Issue278_WildcardSurvivesDeepMerge proves that when an
// existing opencode.json contains the old "takt-*": "allow" wildcard and the
// overlay supplies an explicit allowlist, deep merge keeps BOTH — the wildcard
// is never removed. This is the core of issue #278.
func TestMergeJSONObjects_Issue278_WildcardSurvivesDeepMerge(t *testing.T) {
	// Simulates an existing user's opencode.json (installed before the fix).
	base := []byte(`{
  "agent": {
    "takt-orchestrator": {
      "permission": {
        "task": {
          "*": "deny",
          "takt-*": "allow"
        }
      }
    }
  }
}`)

	// Simulates the NEW overlay with explicit allowlist (the fix).
	overlay := []byte(`{
  "agent": {
    "takt-orchestrator": {
      "permission": {
        "task": {
          "*": "deny",
          "analyst": "allow",
          "takt-explore": "allow",
          "takt-propose": "allow",
          "spec": "allow",
          "takt-design": "allow",
          "takt-tasks": "allow",
          "takt-apply": "allow",
          "verify": "allow"
        }
      }
    }
  }
}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged error = %v", err)
	}

	agent := got["agent"].(map[string]any)
	orch := agent["takt-orchestrator"].(map[string]any)
	perm := orch["permission"].(map[string]any)
	task := perm["task"].(map[string]any)

	// The critical assertion: "takt-*" SURVIVES the deep merge.
	// This proves existing users keep the wildcard even after syncing
	// with the new explicit overlay — the bug persists without __replace__.
	if _, hasWildcard := task["takt-*"]; !hasWildcard {
		t.Fatal("UNEXPECTED: 'takt-*' was removed by deep merge — this contradicts the merge algorithm")
	}

	// Verify the new explicit entries are also present (merge adds them).
	for _, phase := range []string{"analyst", "takt-explore", "takt-propose", "spec", "takt-design", "takt-tasks", "takt-apply", "verify"} {
		if _, ok := task[phase]; !ok {
			t.Fatalf("explicit permission %q missing from merged result", phase)
		}
	}

	t.Logf("CONFIRMED: deep merge produces %d task keys (wildcard + explicit coexist)", len(task))
	t.Logf("Merged task block: %v", task)
}

// TestMergeJSONObjects_Issue278_ReplaceSentinelFixesWildcard proves that
// wrapping the task block in __replace__ DOES remove the old wildcard,
// which is the proposed fix for issue #278.
func TestMergeJSONObjects_Issue278_ReplaceSentinelFixesWildcard(t *testing.T) {
	// Same base: existing user with old wildcard.
	base := []byte(`{
  "agent": {
    "takt-orchestrator": {
      "permission": {
        "task": {
          "*": "deny",
          "takt-*": "allow"
        }
      }
    }
  }
}`)

	// Overlay using __replace__ sentinel on the task block.
	overlay := []byte(`{
  "agent": {
    "takt-orchestrator": {
      "permission": {
        "task": {
          "__replace__": {
            "*": "deny",
            "analyst": "allow",
            "takt-explore": "allow",
            "takt-propose": "allow",
            "spec": "allow",
            "takt-design": "allow",
            "takt-tasks": "allow",
            "takt-apply": "allow",
            "verify": "allow"
          }
        }
      }
    }
  }
}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged error = %v", err)
	}

	agent := got["agent"].(map[string]any)
	orch := agent["takt-orchestrator"].(map[string]any)
	perm := orch["permission"].(map[string]any)
	task := perm["task"].(map[string]any)

	// The wildcard MUST be gone.
	if _, hasWildcard := task["takt-*"]; hasWildcard {
		t.Fatal("'takt-*' survived __replace__ — sentinel is broken")
	}

	// __replace__ must NOT leak into output.
	if _, leaked := task["__replace__"]; leaked {
		t.Fatal("__replace__ sentinel leaked into output")
	}

	// All explicit entries must be present.
	expected := []string{"*", "analyst", "takt-explore", "takt-propose", "spec", "takt-design", "takt-tasks", "takt-apply", "verify"}
	for _, key := range expected {
		if _, ok := task[key]; !ok {
			t.Fatalf("expected key %q missing from task block after __replace__", key)
		}
	}

	if len(task) != len(expected) {
		t.Fatalf("task block has %d keys, want %d; got: %v", len(task), len(expected), task)
	}

	t.Logf("CONFIRMED: __replace__ produces exactly %d task keys (no wildcard)", len(task))
}

// ─── R4: union of plugins/skills arrays (Ola 0) ───────────────────────────────

// TestMergeJSONObjectsUnionsPluginsFromUserCLIConfig proves that a preexisting
// cli.json with the user's own plugins survives a Takt install: Takt's overlay
// (TaktCLIArtifact emits plugins: ["./tui-plugins/takt-logo.tsx"]) ADDS its
// entry without dropping or reordering user entries.
func TestMergeJSONObjectsUnionsPluginsFromUserCLIConfig(t *testing.T) {
	// Simulates a user's existing cli.json with two of their own plugins.
	base := []byte(`{
  "$schema": "https://opencode.ai/v2/cli.json",
  "theme": {"name": "custom"},
  "plugins": ["/home/user/.opencode/plugins/my-plugin.ts", "./local/extra.tsx"]
}`)

	// Simulates Takt's overlay (mirrors TaktCLIArtifact).
	overlay := []byte(`{
  "$schema": "https://opencode.ai/v2/cli.json",
  "theme": {"name": "takt"},
  "plugins": ["./tui-plugins/takt-logo.tsx"]
}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged error = %v", err)
	}

	want := []any{"/home/user/.opencode/plugins/my-plugin.ts", "./local/extra.tsx", "./tui-plugins/takt-logo.tsx"}
	plugins, ok := got["plugins"].([]any)
	if !ok {
		t.Fatalf("plugins is not an array; got: %T = %v", got["plugins"], got["plugins"])
	}
	if !reflect.DeepEqual(plugins, want) {
		t.Fatalf("plugins = %#v, want %#v (user entries first, overlay entry appended)", plugins, want)
	}

	// The theme conflict keeps overlay-wins semantics (scalar object, not an array).
	if theme := got["theme"].(map[string]any)["name"]; theme != "takt" {
		t.Fatalf("theme.name = %v, want takt (overlay must win on non-array keys)", theme)
	}
}

// TestMergeJSONObjectsPluginsUnionIsIdempotent proves that applying the merge
// twice produces the same result — a repeated Takt install must never grow the
// plugins array with duplicates.
func TestMergeJSONObjectsPluginsUnionIsIdempotent(t *testing.T) {
	base := []byte(`{"plugins":["user-a.ts"]}`)
	overlay := []byte(`{"plugins":["user-a.ts","./tui-plugins/takt-logo.tsx"]}`)

	once, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	twice, err := MergeJSONObjects(once, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() second pass error = %v", err)
	}

	var first, second map[string]any
	if err := json.Unmarshal(once, &first); err != nil {
		t.Fatalf("Unmarshal first pass error = %v", err)
	}
	if err := json.Unmarshal(twice, &second); err != nil {
		t.Fatalf("Unmarshal second pass error = %v", err)
	}

	firstPlugins := first["plugins"].([]any)
	secondPlugins := second["plugins"].([]any)
	if !reflect.DeepEqual(firstPlugins, secondPlugins) {
		t.Fatalf("second merge changed the array: first = %#v, second = %#v", firstPlugins, secondPlugins)
	}

	if duplicates := countDuplicateStrings(secondPlugins); duplicates != 0 {
		t.Fatalf("merged plugins contains %d duplicate entries: %#v", duplicates, secondPlugins)
	}
}

// TestMergeJSONObjectsUnionsSkillsArray covers the "skills" key emitted into
// opencode.json: same union semantics as plugins — user skill paths survive
// and Takt's paths are appended only when missing.
func TestMergeJSONObjectsUnionsSkillsArray(t *testing.T) {
	base := []byte(`{
  "skills": ["~/.config/takt-ai/skills", "/home/user/my-skills"],
  "theme": "dracula"
}`)
	overlay := []byte(`{
  "skills": ["~/.config/takt-ai/skills", "~/.config/takt-ai/skills/default"],
  "theme": "takt"
}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged error = %v", err)
	}

	want := []any{"~/.config/takt-ai/skills", "/home/user/my-skills", "~/.config/takt-ai/skills/default"}
	skills, ok := got["skills"].([]any)
	if !ok {
		t.Fatalf("skills is not an array; got: %T = %v", got["skills"], got["skills"])
	}
	if !reflect.DeepEqual(skills, want) {
		t.Fatalf("skills = %#v, want %#v", skills, want)
	}

	if got["theme"] != "takt" {
		t.Fatalf("theme = %v, want takt", got["theme"])
	}
}

// TestMergeJSONObjectsNonUnionArraysStillReplace locks in the regression
// boundary: ONLY plugins and skills get union semantics. Every other array
// key keeps the historical overlay-wins behavior, and nested objects and
// scalars merge exactly as before.
func TestMergeJSONObjectsNonUnionArraysStillReplace(t *testing.T) {
	base := []byte(`{
  "mcp": {"engram": {"args": ["mcp","--old"]}},
  "customArray": ["keep-me","me-too"],
  "settings": {"flags": {"x": true}},
  "plugins": ["user-a.ts"],
  "scalar": "old"
}`)
	overlay := []byte(`{
  "mcp": {"engram": {"args": ["mcp","--new"]}},
  "customArray": ["only-overlay"],
  "settings": {"flags": {"y": true}},
  "plugins": ["./tui-plugins/takt-logo.tsx"],
  "scalar": "new"
}`)

	merged, err := MergeJSONObjects(base, overlay)
	if err != nil {
		t.Fatalf("MergeJSONObjects() error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(merged, &got); err != nil {
		t.Fatalf("Unmarshal merged error = %v", err)
	}

	// A non-union array key must be REPLACED by the overlay (current behavior).
	customArray, ok := got["customArray"].([]any)
	if !ok {
		t.Fatalf("customArray is not an array; got: %T = %v", got["customArray"], got["customArray"])
	}
	if !reflect.DeepEqual(customArray, []any{"only-overlay"}) {
		t.Fatalf("customArray = %#v, want [only-overlay] (non-union arrays must be replaced)", customArray)
	}

	// Nested arrays inside objects must also keep replacement semantics.
	args := got["mcp"].(map[string]any)["engram"].(map[string]any)["args"].([]any)
	if !reflect.DeepEqual(args, []any{"mcp", "--new"}) {
		t.Fatalf("nested args = %#v, want [mcp --new] (nested arrays must be replaced)", args)
	}

	// Nested objects keep deep-merge behavior.
	flags := got["settings"].(map[string]any)["flags"].(map[string]any)
	if flags["x"] != true || flags["y"] != true {
		t.Fatalf("flags = %#v, want both x and y (deep merge unchanged)", flags)
	}

	// Scalars keep overlay-wins.
	if got["scalar"] != "new" {
		t.Fatalf("scalar = %v, want new", got["scalar"])
	}

	// plugins keeps union semantics even in this mixed scenario.
	plugins := got["plugins"].([]any)
	if !reflect.DeepEqual(plugins, []any{"user-a.ts", "./tui-plugins/takt-logo.tsx"}) {
		t.Fatalf("plugins = %#v, want union of user and overlay entries", plugins)
	}
}

// countDuplicateStrings returns how many entries appear more than once.
func countDuplicateStrings(entries []any) int {
	seen := map[any]int{}
	for _, entry := range entries {
		seen[entry]++
	}

	duplicates := 0
	for _, count := range seen {
		if count > 1 {
			duplicates += count - 1
		}
	}
	return duplicates
}
