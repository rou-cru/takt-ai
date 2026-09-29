package catalog

import (
	"io/fs"
	"strings"
	"testing"
)

func TestPlanningDocumentTemplates(t *testing.T) {
	// This is the set of recognized types promised by the planning skills. Keep
	// the contract structural: descriptions may improve without snapshot churn.
	for _, tc := range []struct {
		skill, file, title string
	}{
		{"takt-memory-pm", "brief", "Brief"},
		{"takt-memory-pm", "prd", "PRD"},
		{"takt-memory-architect", "adr-madr", "ADR — MADR"},
		{"takt-memory-architect", "adr-nygard", "ADR — Nygard"},
		{"takt-memory-architect", "adr-y-statement", "ADR — Y-Statement"},
		{"takt-memory-architect", "architecture-overview", "Architecture Overview"},
		{"takt-memory-architect", "c4-model", "C4 Model"},
		{"takt-memory-architect", "interface-contract", "Interface Contract"},
		{"takt-memory-product-designer", "brand-guidelines", "Brand Guidelines"},
		{"takt-memory-product-designer", "design-decision-record", "Design Decision Record"},
		{"takt-memory-product-designer", "design-system-spec", "Design System Spec"},
		{"takt-memory-product-designer", "design-tokens", "Design Tokens"},
		{"takt-memory-product-designer", "journey-map", "Journey Map"},
		{"takt-memory-product-designer", "style-guide", "Style Guide"},
		{"takt-memory-spec", "bdd-spec", "BDD Spec"},
		{"takt-memory-spec", "frd", "FRD"},
		{"takt-memory-tpm", "wbs", "WBS"},
		{"takt-memory-analyst", "technical-spike", "Technical Spike"},
		{"takt-memory-analyst", "trade-off-analysis", "Trade-off Analysis"},
		{"takt-invariant-authoring", "generic-artifact", "Generic Artifact Skeleton"},
	} {
		t.Run(tc.skill+"/"+tc.file, func(t *testing.T) {
			reference := "references/" + tc.file + ".md"
			descriptor, err := fs.ReadFile(AssetFS(), "skills/"+tc.skill+"/SKILL.md")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(descriptor), "`"+reference+"`") {
				t.Errorf("skill does not direct the reader to %s", reference)
			}
			body, err := fs.ReadFile(AssetFS(), "skills/"+tc.skill+"/"+reference)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(string(body), "# "+tc.title+"\n") {
				t.Errorf("template must be titled %q", tc.title)
			}
			_, table, ok := strings.Cut(string(body), "| Field | Holds |\n| --- | --- |\n")
			if !ok {
				t.Fatal("missing Field | Holds table")
			}
			fields := make(map[string]bool)
			for _, row := range strings.Split(strings.TrimSpace(table), "\n") {
				cells := strings.Split(row, "|")
				if len(cells) != 4 || strings.TrimSpace(cells[0]) != "" || strings.TrimSpace(cells[3]) != "" {
					t.Fatalf("expected two-column template row, got %q", row)
				}
				field := strings.ToLower(strings.TrimSpace(cells[1]))
				if field == "" || strings.TrimSpace(cells[2]) == "" {
					t.Errorf("empty field or guidance in %q", row)
				}
				if fields[field] {
					t.Errorf("duplicate field %q", field)
				}
				fields[field] = true
				switch field {
				case "status", "open questions", "next steps":
					t.Errorf("atemporal artifact contains process field %q", field)
				}
			}
			if len(fields) < 3 {
				t.Error("template lacks a substantive section skeleton")
			}
		})
	}
}

func TestPlanningAgentsDeclareInvariantAuthoring(t *testing.T) {
	pack, err := LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	assertAgentsDeclareSkill(t, pack, "takt-invariant-authoring",
		"takt-pm", "takt-architect", "takt-product-designer", "takt-spec", "takt-tpm", "takt-analyst")
}
