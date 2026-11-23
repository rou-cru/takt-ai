package prompts_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rou-cru/takt-ai/takt/prompts"
)

func TestLoadEmbeddedRegistry(t *testing.T) {
	reg, err := prompts.Load()
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if len(reg.Entries) == 0 {
		t.Fatal("expected embedded entries, got none")
	}
	if len(reg.Tools) == 0 {
		t.Fatal("expected embedded tools, got none")
	}
	if _, ok := reg.Entry("context.memory_clause"); !ok {
		t.Fatal("missing context.memory_clause")
	}
	if _, ok := reg.Tool("claim_assign"); !ok {
		t.Fatal("missing claim_assign tool spec")
	}
	// Default() returns the same validated registry.
	def := prompts.Default()
	if len(def.Entries) != len(reg.Entries) || len(def.Tools) != len(reg.Tools) {
		t.Fatalf("Default() mismatch: got (%d, %d), want (%d, %d)", len(def.Entries), len(def.Tools), len(reg.Entries), len(reg.Tools))
	}
}

func TestRenderAndToolDescription(t *testing.T) {
	reg := prompts.Default()
	got, err := reg.Render("context.memory_clause", map[string]string{
		"contract_skill": "skills/takt-memory-contract/SKILL.md",
		"memory_skill":   "skills/takt-memory-dev/SKILL.md",
	})
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}
	if !strings.HasPrefix(got, "## Memory\n\n") || !strings.HasSuffix(got, "apply to you.\n") {
		t.Fatalf("unexpected memory clause:\n%q", got)
	}
	literal, err := reg.Render("context.memory_clause", map[string]string{
		"contract_skill": "{{memory_skill}}",
		"memory_skill":   "skills/takt-memory-dev/SKILL.md",
	})
	if err != nil {
		t.Fatalf("Render with placeholder-like value failed: %v", err)
	}
	if !strings.Contains(literal, "{{memory_skill}}") {
		t.Fatalf("expected value to stay literal, got:\n%q", literal)
	}
	if _, err := reg.Render("unknown.id", nil); err == nil {
		t.Fatal("expected error for unknown id")
	}
	if _, err := reg.Render("context.memory_clause", map[string]string{"contract_skill": "a"}); err == nil {
		t.Fatal("expected error for missing variable")
	}
	if _, err := reg.Render("context.memory_clause", map[string]string{
		"contract_skill": "a", "memory_skill": "b", "extra": "c",
	}); err == nil {
		t.Fatal("expected error for unexpected variable")
	}

	rootOnly := reg.MustRender("tool.memory.root_only", nil)
	desc, err := reg.RenderToolDescription("close_session", map[string]string{"root_only": rootOnly})
	if err != nil {
		t.Fatalf("RenderToolDescription failed: %v", err)
	}
	if !strings.HasSuffix(desc, rootOnly) {
		t.Fatalf("expected close_session description to end with root_only, got %q", desc)
	}
	if _, err := reg.RenderToolDescription("no_such_tool", nil); err == nil {
		t.Fatal("expected error for unknown tool")
	}
	if _, err := reg.RenderToolDescription("subagent", nil); err == nil {
		t.Fatal("expected error for tool without description")
	}
}

func TestBundleForOwner(t *testing.T) {
	reg := prompts.Default()
	vfsBundle := reg.BundleForOwner("plugin/takt-vfs")
	if vfsBundle["context.author_notice"] == "" {
		t.Fatal("expected context.author_notice in plugin/takt-vfs bundle")
	}
	if vfsBundle[prompts.ToolDescriptionKey("claim_assign")] == "" {
		t.Fatal("expected claim_assign description in plugin/takt-vfs bundle")
	}
	if vfsBundle[prompts.ToolParamKey("subagent", "sessionID")] == "" {
		t.Fatal("expected subagent.sessionID param in plugin/takt-vfs bundle")
	}
	if _, leaked := vfsBundle["context.memory_clause"]; leaked {
		t.Fatal("opencode/layout entry leaked into plugin/takt-vfs bundle")
	}
	dagBundle := reg.BundleForOwner("plugin/takt-dag")
	if len(dagBundle) != 1 || dagBundle["interlocutor.abort_stop_failed"] == "" {
		t.Fatalf("unexpected plugin/takt-dag bundle: %v", dagBundle)
	}
}

func TestLoadFSRejectsInvalidFiles(t *testing.T) {
	cases := []struct {
		name string
		fsys fstest.MapFS
		want string
	}{
		{
			name: "no yaml files",
			fsys: fstest.MapFS{"readme.txt": {Data: []byte("hi")}},
			want: "no prompt YAML files",
		},
		{
			name: "empty document",
			fsys: fstest.MapFS{"a.yaml": {Data: []byte("{}\n")}},
			want: "no entries or tools",
		},
		{
			name: "invalid id",
			fsys: fstest.MapFS{"a.yaml": {Data: []byte("entries:\n  - id: bad\n    surface: context_injection\n    owner: plugin/takt-vfs\n    audience: [all]\n    correlates_with: [shared/BASELINE.md]\n    text: hi\n")}},
			want: "invalid prompt id",
		},
		{
			name: "invalid surface",
			fsys: fstest.MapFS{"a.yaml": {Data: []byte("entries:\n  - id: a.b\n    surface: bogus\n    owner: plugin/takt-vfs\n    audience: [all]\n    correlates_with: [shared/BASELINE.md]\n    text: hi\n")}},
			want: "invalid surface",
		},
		{
			name: "invalid owner",
			fsys: fstest.MapFS{"a.yaml": {Data: []byte("entries:\n  - id: a.b\n    surface: context_injection\n    owner: bogus\n    audience: [all]\n    correlates_with: [shared/BASELINE.md]\n    text: hi\n")}},
			want: "invalid owner",
		},
		{
			name: "undeclared placeholder",
			fsys: fstest.MapFS{"a.yaml": {Data: []byte("entries:\n  - id: a.b\n    surface: context_injection\n    owner: plugin/takt-vfs\n    audience: [all]\n    correlates_with: [shared/BASELINE.md]\n    text: 'hello {{who}}'\n")}},
			want: "undeclared placeholder",
		},
		{
			name: "unused var",
			fsys: fstest.MapFS{"a.yaml": {Data: []byte("entries:\n  - id: a.b\n    surface: context_injection\n    owner: plugin/takt-vfs\n    audience: [all]\n    correlates_with: [shared/BASELINE.md]\n    vars: [who]\n    text: 'hello'\n")}},
			want: "not used",
		},
		{
			name: "duplicate id across files",
			fsys: fstest.MapFS{
				"a.yaml": {Data: []byte("entries:\n  - id: a.b\n    surface: context_injection\n    owner: plugin/takt-vfs\n    audience: [all]\n    correlates_with: [shared/BASELINE.md]\n    text: hi\n")},
				"b.yaml": {Data: []byte("entries:\n  - id: a.b\n    surface: context_injection\n    owner: plugin/takt-vfs\n    audience: [all]\n    correlates_with: [shared/BASELINE.md]\n    text: hi\n")},
			},
			want: "duplicate prompt id",
		},
		{
			name: "invalid correlates_with",
			fsys: fstest.MapFS{"a.yaml": {Data: []byte("entries:\n  - id: a.b\n    surface: context_injection\n    owner: plugin/takt-vfs\n    audience: [all]\n    correlates_with: [../escape.md]\n    text: hi\n")}},
			want: "invalid correlates_with",
		},
		{
			name: "tool without description or params",
			fsys: fstest.MapFS{"a.yaml": {Data: []byte("tools:\n  - name: foo_bar\n    owner: plugin/takt-vfs\n    audience: [orchestrator]\n    correlates_with: [shared/BASELINE.md]\n")}},
			want: "must declare description or params",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := prompts.LoadFS(tc.fsys)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("LoadFS() error = %v, want substring %q", err, tc.want)
			}
		})
	}
}
