package modelpicker_test

import (
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/catalog"
	"github.com/rou-cru/takt-ai/takt/internal/opencodeapi"
	"github.com/rou-cru/takt-ai/takt/tui/modelpicker"
	"github.com/rou-cru/takt-ai/takt/tui/ui"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/rou-cru/takt-ai/takt/agents/shared"
	"github.com/rou-cru/takt-ai/takt/model"
	"github.com/rou-cru/takt-ai/takt/tui/theme"
)

func typeText(p *modelpicker.Model, text string) {
	for _, r := range text {
		if r == ' ' {
			p.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
			continue
		}
		p.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
}

func body(p modelpicker.Model) string { return ansi.Strip(p.Frame().Body) }

// agentNames lists the deployed specialist instances in picker order.
func agentNames(t *testing.T) []string {
	t.Helper()
	loaded, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, def := range loaded.Agents {
		names = append(names, def.Instances...)
	}
	return names
}

// focusAgent moves the assignment list's cursor to name; row 0 is All agents.
func focusAgent(t *testing.T, p *modelpicker.Model, name string) bool {
	t.Helper()
	for index, agent := range agentNames(t) {
		if agent == name {
			for range index + 1 {
				p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
			}
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
	p := modelpicker.New()
	p.Available = []string{"opencode/big-pickle", "provider/jk-model"}
	if !focusAgent(t, &p, "analyst") {
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
	if !strings.Contains(body(p), "Search: pickle") {
		t.Fatalf("filter not kept for the same specialist:\n%s", body(p))
	}
	p.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	if strings.Contains(body(p), "Search: pickle") {
		t.Fatalf("clear search failed:\n%s", body(p))
	}
}

// While search owns focus, printable keys type letters and arrows still move:
// the key bar names no letter shortcuts so the two modes stay distinguishable.
func TestSearchFieldTypesShortcutKeys(t *testing.T) {
	p := modelpicker.New()
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
	p := modelpicker.New()
	p.Available = []string{"provider/orchestrator-model"}
	if !focusAgent(t, &p, shared.OrchestratorID) {
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
	p := modelpicker.New()
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
	p := modelpicker.New()
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
			p := modelpicker.New()
			p.Available = []string{"provider/current", "provider/new"}
			installed := map[string]model.ModelAssignment{
				"analyst":           {Model: "provider/current", Effort: "high"},
				"custom-specialist": {Model: "provider/unlisted", Effort: "low"},
			}
			p.Preload(installed)
			if !focusAgent(t, &p, "analyst") {
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
	p := modelpicker.New()
	p.Available = []string{"provider/current"}
	p.Preload(map[string]model.ModelAssignment{"analyst": {Model: "provider/current", Effort: "high"}})
	if !focusAgent(t, &p, "analyst") {
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
	p := modelpicker.New()
	p.Available = []string{"provider/shared"}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown}) // skip inherit
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.Changes() != len(agentNames(t)) {
		t.Fatalf("Changes() = %d, want every agent (%d)", p.Changes(), len(agentNames(t)))
	}
	for _, agent := range agentNames(t) {
		if p.SparseOverrides()[agent].Model != "provider/shared" {
			t.Fatalf("%s not assigned the shared model: %v", agent, p.SparseOverrides())
		}
	}
	if !rowShows(body(p), ui.TextPickerAllAgents, "provider/shared") {
		t.Fatalf("All agents row does not show the shared model:\n%s", body(p))
	}
	if len(p.Pending()) != len(agentNames(t)) {
		t.Fatalf("Pending() = %v, want every agent", p.Pending())
	}
}

// Models read by their display name, and search finds them by it. Where one
// name can repeat across providers (the All tab) the provider labels the row.
func TestModelListShowsNamesAndSearchesThem(t *testing.T) {
	p := modelpicker.New()
	p.Available = []string{"openai/gpt-6-luna-fast", "opencode/fledge"}
	p.Names = map[string]string{"openai/gpt-6-luna-fast": "GPT-6 Luna Fast", "opencode/fledge": "Fledge"}
	p.Providers = map[string]string{"openai": "OpenAI", "opencode": "OpenCode"}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !rowShows(body(p), "GPT-6 Luna Fast", "OpenAI") {
		t.Fatalf("model row lacks its display name and provider:\n%s", body(p))
	}
	typeText(&p, "luna fast")
	if view := body(p); !strings.Contains(view, "GPT-6 Luna Fast") || strings.Contains(view, "Fledge") {
		t.Fatalf("search by display name failed:\n%s", view)
	}
}

// Provider tabs come from the models' providers, named by OpenCode, and the
// left and right arrows narrow the list to one provider.
func TestProviderTabsNarrowTheList(t *testing.T) {
	p := modelpicker.New()
	p.Available = []string{"openai/a", "openai/b", "opencode/c"}
	p.Names = map[string]string{"openai/a": "Alpha", "openai/b": "Beta", "opencode/c": "Gamma"}
	p.Providers = map[string]string{"openai": "OpenAI", "opencode": "OpenCode"}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := body(p); !strings.Contains(view, "All 3") || !strings.Contains(view, "OpenAI 2") || !strings.Contains(view, "OpenCode 1") || !strings.Contains(view, "Gamma") {
		t.Fatalf("tabs missing or All tab incomplete:\n%s", view)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if view := body(p); !strings.Contains(view, "Alpha") || strings.Contains(view, "Gamma") {
		t.Fatalf("provider tab did not narrow the list:\n%s", view)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	if view := body(p); !strings.Contains(view, "Gamma") {
		t.Fatalf("left did not return to All:\n%s", view)
	}
}

// The picker shows only what OpenCode states: a model without pricing gets no
// price or Cost line, and the detail pane needs room beside the list.
func TestDetailShowsOnlyStatedFacts(t *testing.T) {
	priced := opencodeapi.Model{
		Name: "Haiku", ContextLimit: 1_000_000, OutputLimit: 128_000,
		Cost: []opencodeapi.CostTier{
			{Input: 0.1, Output: 0.5, CacheRead: 0.01, CacheWrite: 0.125},
			{AboveContext: 100_000, Input: 0.5, Output: 2.5},
		},
		Input: []string{"text", "image"}, Variants: []string{"low", "high"},
		Released: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	unpriced := opencodeapi.Model{Name: "Luna", ContextLimit: 400_000, OutputLimit: 128_000}
	build := func(width int) modelpicker.Model {
		p := modelpicker.New()
		p.Width = width
		p.Available = []string{"go/haiku", "plan/luna"}
		p.Names = map[string]string{"go/haiku": "Haiku", "plan/luna": "Luna"}
		p.Info = map[string]opencodeapi.Model{"go/haiku": priced, "plan/luna": unpriced}
		p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		return p
	}
	view := body(build(160))
	for _, want := range []string{"$0.10 in · $0.50 out", ">100k ctx: $0.50 / $2.50", "read $0.01 · write $0.125", "1M context · 128k output", "text  image", "low  high", "2026-10-01"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail lacks %q:\n%s", want, view)
		}
	}
	p := build(160)
	p.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	p.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if view := body(p); !strings.Contains(view, "400k context") || strings.Contains(view, "Cost") || strings.Contains(view, "$") {
		t.Fatalf("unpriced model shows pricing:\n%s", view)
	}
	if view := body(build(60)); strings.Contains(view, "Limits") {
		t.Fatalf("detail pane drawn without room:\n%s", view)
	}
}

func TestInheritRowStartsInNameColumn(t *testing.T) {
	p := modelpicker.New()
	p.Available = []string{"provider/model-a"}
	p.Names = map[string]string{"provider/model-a": "Model A"}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := body(p); !strings.Contains(view, "> inherits OpenCode default") {
		t.Fatalf("inherit row is not in the name column:\n%s", view)
	}
}
