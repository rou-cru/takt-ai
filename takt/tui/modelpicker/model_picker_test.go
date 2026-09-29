package modelpicker

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

func typeText(p *Model, text string) {
	for _, r := range text {
		if r == ' ' {
			p.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			continue
		}
		p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func body(p Model) string { return ansi.Strip(p.Frame().Body) }

func focusAgent(p *Model, name string) bool {
	for index, agent := range p.agents {
		if agent.Name == name {
			p.cursor = index
			return true
		}
	}
	return false
}

func TestOpenCodeSearchAcceptsJKAndReturnsRealSelection(t *testing.T) {
	p := New()
	p.Available = []string{"opencode/big-pickle", "provider/jk-model"}
	if !focusAgent(&p, "analyst") {
		t.Fatal("analyst missing from assignment targets")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	typeText(&p, "pickle")
	if body := body(p); !strings.Contains(body, "opencode/big-pickle") || strings.Contains(body, "provider/jk-model") {
		t.Fatal(body)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if body := body(p); !strings.Contains(body, "> analyst — opencode/big-pickle") {
		t.Fatalf("selection missing from the assignment list:\n%s", body)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.search != "pickle" {
		t.Fatalf("filter not kept for the same specialist: %q", p.search)
	}
	p.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if p.search != "" {
		t.Fatalf("clear search failed: %q", p.search)
	}
}

// While search owns focus, printable keys type letters and arrows still move:
// the key bar names no letter shortcuts so the two modes stay distinguishable.
func TestSearchFieldTypesShortcutKeys(t *testing.T) {
	p := New()
	p.Available = []string{"a/one", "a/two", "a/three"}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	typeText(&p, "jk q?")
	if body := body(p); strings.Contains(body, "a/one") {
		t.Fatalf("filter did not exclude non-matching models:\n%s", body)
	}
	p.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if body := body(p); !strings.Contains(body, "> a/one") {
		t.Fatalf("arrows did not move the list:\n%s", body)
	}
}

func TestOrchestratorCanBeAssignedAModel(t *testing.T) {
	p := New()
	p.Available = []string{"provider/orchestrator-model"}
	if !focusAgent(&p, shared.OrchestratorID) {
		t.Fatalf("orchestrator %q missing from assignment targets", shared.OrchestratorID)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // skip inherit; choose the discovered model
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := p.SparseOverrides()[shared.OrchestratorID]; got.Model != "provider/orchestrator-model" {
		t.Fatalf("orchestrator assignment = %#v, want provider/orchestrator-model", got)
	}
	if !strings.Contains(body(p), shared.OrchestratorID+" — provider/orchestrator-model") {
		t.Fatalf("assignment list does not show orchestrator model:\n%s", body(p))
	}
}

func TestLoadFailureKeepsInheritChoice(t *testing.T) {
	p := New()
	p.LoadErr = errors.New("opencode not found")
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := body(p)
	if !strings.Contains(view, "> inherit from OpenCode") {
		t.Fatal(view)
	}
}

func TestLongModelCatalogKeepsCursorVisible(t *testing.T) {
	p := New()
	p.Height = 10
	for i := range 100 {
		p.Available = append(p.Available, fmt.Sprintf("provider/model-%03d", i))
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	for range 80 {
		p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	view := body(p)
	if !strings.Contains(view, theme.Icon.Cursor+"provider/model-079") || !strings.Contains(view, "81 / 101") {
		t.Fatal(view)
	}
	if strings.Contains(view, "model-000") {
		t.Fatal("catalog did not scroll")
	}
}

func TestInstalledVariantsSurvivePickerVisitsAndUnrelatedChanges(t *testing.T) {
	for _, confirm := range []bool{false, true} {
		t.Run(fmt.Sprintf("confirm-current=%t", confirm), func(t *testing.T) {
			p := New()
			p.Available = []string{"provider/current", "provider/new"}
			installed := map[string]model.ModelAssignment{
				"analyst":           {Model: "provider/current", Effort: "high"},
				"custom-specialist": {Model: "provider/unlisted", Effort: "low"},
			}
			p.Preload(installed)
			if !focusAgent(&p, "analyst") {
				t.Fatal("analyst missing from assignment targets")
			}
			p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			if confirm {
				p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			} else {
				p.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			}
			if p.Changes() != 0 || !maps.Equal(p.SparseOverrides(), installed) {
				t.Fatalf("visiting existing model changed variants: %v", p.SparseOverrides())
			}
			if done, back := p.Update(tea.KeyPressMsg{Code: tea.KeyEscape}); done || !back {
				t.Fatal("closing the unchanged picker did not go back")
			}
			p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			if done, _ := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); done {
				t.Fatal("Apply must be unavailable without changes")
			}
			p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			other := p.Detail()
			typeText(&p, "provider/new")
			p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			p.Update(tea.KeyPressMsg{Code: tea.KeyTab})
			if done, back := p.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); !done || back {
				t.Fatal("Apply did not accept the unrelated model change")
			}
			want := maps.Clone(installed)
			want[other] = model.ModelAssignment{Model: "provider/new"}
			if p.Changes() != 1 || !maps.Equal(p.SparseOverrides(), want) {
				t.Fatalf("unrelated edit lost installed variants: got %v, want %v", p.SparseOverrides(), want)
			}
		})
	}
}

func TestInheritRemovesInstalledModelAndVariant(t *testing.T) {
	p := New()
	p.Available = []string{"provider/current"}
	p.Preload(map[string]model.ModelAssignment{"analyst": {Model: "provider/current", Effort: "high"}})
	if !focusAgent(&p, "analyst") {
		t.Fatal("analyst missing from assignment targets")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.Changes() != 1 || p.SparseOverrides() != nil {
		t.Fatalf("inherit did not clear the override: %v", p.SparseOverrides())
	}
	if !strings.Contains(body(p), "analyst — inherit from OpenCode") {
		t.Fatal(body(p))
	}
}
