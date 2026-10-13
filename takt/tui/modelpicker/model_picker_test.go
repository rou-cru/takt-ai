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

// displayNames maps each deployed specialist instance to its catalog name.
func displayNames(t *testing.T) map[string]string {
	t.Helper()
	loaded, err := catalog.LoadPackages()
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, def := range loaded.Agents {
		for _, instance := range def.Instances {
			names[instance] = def.DisplayNames[instance]
		}
	}
	return names
}

// focusAgent moves the assignment list's cursor to the row of instance name,
// whatever order the picker lists agents in.
func focusAgent(t *testing.T, p *modelpicker.Model, name string) bool {
	t.Helper()
	display, ok := displayNames(t)[name]
	if !ok {
		return false
	}
	for range displayNames(t) {
		p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		if strings.HasPrefix(focusedRow(body(*p)), display+" ") {
			return true
		}
	}
	return false
}

// focusedRow is the table row under the cursor, marker and padding removed.
func focusedRow(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if row, ok := strings.CutPrefix(strings.TrimSpace(line), ">"); ok {
			return strings.TrimSpace(row)
		}
	}
	return ""
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
	if body := body(p); focusedRow(body) == "" || !rowShows(body, "Analyst", "big-pickle") || strings.Contains(body, "opencode/") {
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
	if body := body(p); !strings.Contains(body, "> a/two") {
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
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := p.SparseOverrides()[shared.OrchestratorID]; got.Model != "provider/orchestrator-model" {
		t.Fatalf("orchestrator assignment = %#v, want provider/orchestrator-model", got)
	}
	if !rowShows(body(p), "Orchestrator", "orchestrator-model") {
		t.Fatalf("assignment list does not show orchestrator model:\n%s", body(p))
	}
}

func TestLoadFailureListsNothingToChoose(t *testing.T) {
	p := modelpicker.New()
	p.LoadErr = errors.New("opencode not found")
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := body(p); !strings.Contains(view, "OpenCode models are unavailable") || strings.Contains(view, theme.Icon.Cursor) || strings.Contains(view, "No models match") {
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
	if !strings.Contains(view, theme.Icon.Cursor+"provider/model-080") || !strings.Contains(view, "81 / 100") {
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
			other := ""
			for id, display := range displayNames(t) {
				if display == p.Detail() {
					other = id
				}
			}
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

// Backspace on a specialist clears its model and variant, back to the
// OpenCode default; the hint shows only while there is something to reset.
func TestResetRemovesInstalledModelAndVariant(t *testing.T) {
	p := modelpicker.New()
	p.Available = []string{"provider/current"}
	p.Preload(map[string]model.ModelAssignment{"analyst": {Model: "provider/current", Effort: "high"}, "dev": {Model: "provider/current"}})
	if !focusAgent(t, &p, "analyst") {
		t.Fatal("analyst missing from assignment targets")
	}
	if !strings.Contains(body(p), "reset to default") {
		t.Fatalf("reset hint missing on an assigned row:\n%s", body(p))
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if p.Changes() != 1 || p.SparseOverrides()["analyst"] != (model.ModelAssignment{}) {
		t.Fatalf("reset did not clear the override: %v", p.SparseOverrides())
	}
	if view := body(p); !rowShows(view, "Analyst", "Default") || strings.Contains(view, "reset to") {
		t.Fatal(view)
	}
}

// Delete on All agents clears every specialist at once.
func TestResetOnAllAgentsClearsEveryone(t *testing.T) {
	p := modelpicker.New()
	p.Preload(map[string]model.ModelAssignment{"analyst": {Model: "a/x"}, "dev": {Model: "a/y"}})
	p.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if p.SparseOverrides() != nil || p.Changes() != 2 {
		t.Fatalf("All agents reset left overrides: %v", p.SparseOverrides())
	}
}

// The All agents row assigns one model to every agent as pending changes;
// individual rows stay editable afterwards.
func TestAllAgentsAssignsEveryAgentAtOnce(t *testing.T) {
	p := modelpicker.New()
	p.Available = []string{"provider/shared"}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if p.Changes() != len(displayNames(t)) {
		t.Fatalf("Changes() = %d, want every agent (%d)", p.Changes(), len(displayNames(t)))
	}
	for agent := range displayNames(t) {
		if p.SparseOverrides()[agent].Model != "provider/shared" {
			t.Fatalf("%s not assigned the shared model: %v", agent, p.SparseOverrides())
		}
	}
	if !rowShows(body(p), ui.TextPickerAllAgents, "shared") {
		t.Fatalf("All agents row does not show the shared model:\n%s", body(p))
	}
	if len(p.Pending()) != len(displayNames(t)) {
		t.Fatalf("Pending() = %v, want every agent", p.Pending())
	}
}

// Models read by their display name alone, and search finds them by it.
func TestModelListShowsNamesAndSearchesThem(t *testing.T) {
	p := modelpicker.New()
	p.Available = []string{"openai/gpt-6-luna-fast", "openai/fledge"}
	p.Names = map[string]string{"openai/gpt-6-luna-fast": "GPT-6 Luna Fast", "openai/fledge": "Fledge"}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := body(p); !strings.Contains(view, "> GPT-6 Luna Fast\n") {
		t.Fatalf("model row is not just its display name:\n%s", view)
	}
	typeText(&p, "fledge")
	if view := body(p); !strings.Contains(view, "Fledge") || strings.Contains(view, "Luna") {
		t.Fatalf("search by display name failed:\n%s", view)
	}
}

// Provider tabs come from the models' providers, named by OpenCode without
// the account prefix, and count what the list holds under the search.
func TestProviderTabsNarrowTheList(t *testing.T) {
	p := modelpicker.New()
	p.Available = []string{"openai/a", "openai/b", "opencode/c"}
	p.Names = map[string]string{"openai/a": "Alpha", "openai/b": "Beta", "opencode/c": "Gamma"}
	p.Providers = map[string]string{"openai": "OpenAI", "opencode": "Default / OpenCode"}
	p.Height = 6
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if view := body(p); !strings.Contains(view, "OpenAI 2") || !strings.Contains(view, "OpenCode 1") || strings.Contains(view, "Default /") || strings.Contains(view, "Gamma") {
		t.Fatalf("tabs wrong or list not narrowed:\n%s", view)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if view := body(p); !strings.Contains(view, "Gamma") || strings.Contains(view, "Alpha") {
		t.Fatalf("right did not switch provider:\n%s", view)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	typeText(&p, "beta")
	if view := body(p); !strings.Contains(view, "OpenAI 1") || !strings.Contains(view, "OpenCode 0") {
		t.Fatalf("tab counts ignore the search:\n%s", view)
	}
}

// Switching tabs never moves the divider: the list is as wide as the longest
// name of any provider.
func TestDividerStaysPutAcrossTabs(t *testing.T) {
	p := modelpicker.New()
	p.Width = 160
	p.Available = []string{"a/short", "b/a-much-longer-model-name"}
	p.Info = map[string]opencodeapi.Model{"a/short": {ContextLimit: 1000, OutputLimit: 1000}, "b/a-much-longer-model-name": {ContextLimit: 1000, OutputLimit: 1000}}
	p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	column := func() int {
		for _, line := range strings.Split(body(p), "\n") {
			if index := strings.Index(line, "│"); index >= 0 {
				return index
			}
		}
		return -1
	}
	before := column()
	p.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if after := column(); before < 0 || before != after {
		t.Fatalf("divider moved from %d to %d", before, after)
	}
}

// The picker shows only what OpenCode states, prices as one table per 1M
// tokens at a shared precision; a model without pricing gets no price block.
func TestDetailShowsOnlyStatedFacts(t *testing.T) {
	priced := opencodeapi.Model{
		Name: "Haiku", ContextLimit: 1_000_000, OutputLimit: 128_000,
		Cost: []opencodeapi.CostTier{
			{Input: 0.1, Output: 0.5, CacheRead: 0.01, CacheWrite: 0.125},
			{AboveContext: 100_000, Input: 0.5, Output: 2.5, CacheRead: 0.05, CacheWrite: 0.625},
		},
		Input: []string{"text", "image"}, Variants: []string{"low", "high"},
		Released: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	readOnly := opencodeapi.Model{Name: "Mini", ContextLimit: 1000, OutputLimit: 1000, Cost: []opencodeapi.CostTier{{Input: 1, Output: 2, CacheRead: 0.5}}}
	unpriced := opencodeapi.Model{Name: "Luna", ContextLimit: 400_000, OutputLimit: 128_000}
	build := func(width int) modelpicker.Model {
		p := modelpicker.New()
		p.Width = width
		p.Available = []string{"go/haiku", "go/mini", "plan/luna"}
		p.Names = map[string]string{"go/haiku": "Haiku", "go/mini": "Mini", "plan/luna": "Luna"}
		p.Info = map[string]opencodeapi.Model{"go/haiku": priced, "go/mini": readOnly, "plan/luna": unpriced}
		p.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
		return p
	}
	view := body(build(160))
	for _, want := range []string{"Price per 1M tokens", "Cache write", "≤100k ctx", ">100k ctx", "$0.100", "$2.500", "$0.125", "1M context · 128k output", "text  image", "low  high", "2026-10-01"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail lacks %q:\n%s", want, view)
		}
	}
	p := build(160)
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if view := body(p); !strings.Contains(view, "Cache read") || strings.Contains(view, "Cache write") || strings.Contains(view, "ctx") || !strings.Contains(view, "$1.00") {
		t.Fatalf("single tier or zero cache column shown wrong:\n%s", view)
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	if view := body(p); !strings.Contains(view, "400k context") || strings.Contains(view, "$") {
		t.Fatalf("unpriced model shows pricing:\n%s", view)
	}
	if view := body(build(60)); strings.Contains(view, "Limits") {
		t.Fatalf("detail pane drawn without room:\n%s", view)
	}
}

// A rule separates each catalog group, never leads or trails the table, and
// the cursor walks past it from the last agent of one group to the first of the next.
func TestGroupsAreSeparatedByRulesTheCursorSkips(t *testing.T) {
	p := modelpicker.New()
	p.Height = 60
	lines := strings.Split(body(p), "\n")
	rules := 0
	for index, line := range lines {
		if strings.Trim(strings.TrimSpace(line), "─") != "" {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		rules++
		if index == 0 || index == len(lines)-1 {
			t.Fatalf("rule at the table edge:\n%s", body(p))
		}
	}
	if rules != len(catalog.AgentGroups)-1 {
		t.Fatalf("%d rules, want one between each of %d groups:\n%s", rules, len(catalog.AgentGroups), body(p))
	}
	if !focusAgent(t, &p, "test") {
		t.Fatal("test missing from assignment targets")
	}
	p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if row := focusedRow(body(p)); strings.Trim(row, "─") == "" || row == "" {
		t.Fatalf("cursor landed on a rule or nowhere: %q", row)
	}
}

// In a short terminal the window scrolls with the cursor and never shows a
// rule at its edge.
func TestShortTerminalNeverClipsARuleAtTheEdge(t *testing.T) {
	p := modelpicker.New()
	p.Height = 16
	for range displayNames(t) {
		p.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		if focusedRow(body(p)) == "" {
			t.Fatalf("cursor row not visible:\n%s", body(p))
		}
		var rows []string
		for _, line := range strings.Split(body(p), "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.Contains(trimmed, " / ") {
				rows = append(rows, trimmed)
			}
		}
		if first, last := rows[1], rows[len(rows)-1]; strings.Trim(first, "─") == "" || strings.Trim(last, "─") == "" {
			t.Fatalf("rule at the window edge:\n%s", body(p))
		}
	}
}

// Models from different providers that share a name keep their provider, and
// models of one provider that share a name show their id; otherwise no
// provider prefix repeats down the model column.
func TestModelColumnDropsTheProviderUnlessNamesCollide(t *testing.T) {
	p := modelpicker.New()
	p.Available = []string{"a/one", "b/two", "a/same", "b/same", "a/x", "a/y"}
	p.Names = map[string]string{"a/one": "One", "b/two": "Two", "a/same": "Same", "b/same": "Same", "a/x": "Dup", "a/y": "Dup"}
	p.Providers = map[string]string{"a": "Alpha", "b": "Beta"}
	p.Preload(map[string]model.ModelAssignment{"analyst": {Model: "a/one"}, "dev": {Model: "b/same"}, "fix": {Model: "a/y"}})
	view := body(p)
	if !rowShows(view, "Analyst", "One") || strings.Contains(view, "a/one") {
		t.Fatalf("model shown with its provider prefix:\n%s", view)
	}
	if !rowShows(view, "Developer", "Same · Beta") {
		t.Fatalf("colliding names lost their provider:\n%s", view)
	}
	if !rowShows(view, "Bug-Fixer", " y") || strings.Contains(view, "Dup") {
		t.Fatalf("names colliding within one provider not told apart:\n%s", view)
	}
}
