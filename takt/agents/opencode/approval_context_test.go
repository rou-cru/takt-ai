package opencode_test

import (
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/agents/opencode"
	"github.com/rou-cru/takt-ai/takt/catalog"
)

// Check the guidance as each consumer receives it: rendered native references,
// composed role context, and the memory skills that consumer can retrieve.
// These checks preserve the rules' projection; semantic approval still belongs
// to the user, not a parser or a keyword match in an artifact.
func TestApprovalGuidanceReachesConsumersWithoutPromotingRevisions(t *testing.T) {
	r := renderCatalog(t)
	assets := catalog.AssetFS()
	read := func(p string) string {
		t.Helper()
		b, err := fs.ReadFile(assets, p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	contract := read("skills/takt-memory-contract/SKILL.md")
	for name, rule := range map[string]string{
		"corrected proposal": "a corrected proposal remains",
		"scope change":       "does not inherit that approval",
		"autonomous result":  "that result into a user-approved `decision`",
	} {
		if !strings.Contains(contract, rule) {
			t.Errorf("memory contract lost the %s rule", name)
		}
	}
	for _, def := range r.pack.Agents {
		context, err := def.ComposeText(assets)
		if err != nil {
			t.Fatal(err)
		}
		for _, id := range def.Instances {
			t.Run(id, func(t *testing.T) {
				prompt := r.config.Agents[id].System
				if !strings.Contains(prompt, opencode.FileRef(catalog.BaselinePath)) || !strings.Contains(prompt, "takt-memory-contract") {
					t.Fatal("rendered consumer lost shared authority or memory guidance")
				}
				if !slices.Contains(def.Skills, "takt-memory-contract") || effectOf(r.config.Agents[id].Permissions, "skill", "takt-memory-contract") == "deny" {
					t.Fatal("consumer cannot retrieve its memory contract")
				}
				if !strings.Contains(context, "existing approval remains valid within its scope, without ritual confirmation") {
					t.Error("composed context lost the prior approval rule")
				}
				if id == "spec" {
					if !strings.Contains(prompt, "takt-memory-spec") {
						t.Fatal("spec lost its role-specific memory guidance")
					}
					spec := read("skills/takt-memory-spec/SKILL.md")
					if !strings.Contains(spec, "the specification just authored is not itself proof of approval") || strings.Contains(spec, "specification file as evidence") {
						t.Fatal("the producer treats its own artifact as approval evidence")
					}
				}
			})
		}
	}
}
