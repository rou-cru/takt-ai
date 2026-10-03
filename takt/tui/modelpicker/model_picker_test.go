package modelpicker

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
	"github.com/rou-cru/takt-ai/takt/tui/ui"

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
			p.cursor = index + 1 // row 0 is All agents
			return true
		}
	}
	return false
}

// rowShows reports whether a table row lists agent with value in its model
// column, ignoring the alignment padding and the changed marker.
func rowShows(body, agent, value string) bool {
	for _, line := range strings.Split(body, "\n") {
		row := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), ">"))
		if strings.HasPrefix(row, agent+" ") && strings.Contains(row, value) {
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
	if body := body(p); !strings.Contains(body, "> analyst") || !rowShows(body, "analyst", "opencode/big-pickle") {
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
	if !rowShows(body(p), shared.OrchestratorID, "provider/orchestrator-model") {
		t.Fatalf("assignment list does not show orchestrator model:\n%s", body(p))
	}
}

func TestLoadFailureKeepsInheritChoice(t *testing.T) {
	p := New()
	p.LoadErr = errors.New("opencode not found")
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	view := body(p)
	if !strings.Contains(view, "> inherits OpenCode default") {
		t.Fatal(view)
	}
	p.LoadErr = fmt.Errorf("opencode models: %w: opencode GET /api/model: signal: killed", opencodeapi.ErrUnavailable)
	if view := body(p); !strings.Contains(view, "OpenCode did not respond") || strings.Contains(view, "signal") {
		t.Fatalf("load failure leaks the error chain:\n%s", view)
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
	if !rowShows(body(p), "analyst", "inherits OpenCode default") {
		t.Fatal(body(p))
	}
}

// The All agents row assigns one model to every agent as pending changes;
// individual rows stay editable afterwards.
func TestAllAgentsAssignsEveryAgentAtOnce(t *testing.T) {
	p := New()
	p.Available = []string{"provider/shared"}
	p.cursor = 0
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // skip inherit
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.Changes() != len(p.agents) {
		t.Fatalf("Changes() = %d, want every agent (%d)", p.Changes(), len(p.agents))
	}
	for _, agent := range p.agents {
		if p.SparseOverrides()[agent.Name].Model != "provider/shared" {
			t.Fatalf("%s not assigned the shared model: %v", agent.Name, p.SparseOverrides())
		}
	}
	if !rowShows(body(p), ui.TextPickerAllAgents, "provider/shared") {
		t.Fatalf("All agents row does not show the shared model:\n%s", body(p))
	}
	if len(p.Pending()) != len(p.agents) {
		t.Fatalf("Pending() = %v, want every agent", p.Pending())
	}
}

// Models read by their display name beside the reference, and search finds
// them by either.
func TestModelListShowsNamesAndSearchesThem(t *testing.T) {
	p := New()
	p.Available = []string{"openai/gpt-6-luna-fast", "opencode/fledge"}
	p.Names = map[string]string{"openai/gpt-6-luna-fast": "GPT-6 Luna Fast", "opencode/fledge": "Fledge"}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !rowShows(body(p), "GPT-6 Luna Fast", "openai/gpt-6-luna-fast") {
		t.Fatalf("model row lacks its display name:\n%s", body(p))
	}
	typeText(&p, "luna fast")
	if view := body(p); !strings.Contains(view, "openai/gpt-6-luna-fast") || strings.Contains(view, "opencode/fledge") {
		t.Fatalf("search by display name failed:\n%s", view)
	}
}

func TestInheritRowStartsInNameColumn(t *testing.T) {
	p := New()
	p.Available = []string{"provider/model-a"}
	p.Names = map[string]string{"provider/model-a": "Model A"}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := body(p); !strings.Contains(view, "> inherits OpenCode default") {
		t.Fatalf("inherit row is not in the name column:\n%s", view)
	}
}
