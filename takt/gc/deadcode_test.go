package gc_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rou-cru/takt-ai/takt/gc"
)

func writeFixture(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func deadCodePlan(closure ...string) gc.Plan {
	return gc.Plan{Request: gc.Request{SessionID: "s", CycleID: "c", Mandate: gc.MandateDeadCode}, Closure: closure}
}

func fakePreparation(t *testing.T, script string) (string, string, gc.Preparation) {
	t.Helper()
	root, state := t.TempDir(), t.TempDir()
	writeFixture(t, root, "go.mod", "module fixture\n\ngo 1.25\n")
	writeFixture(t, root, "lib.go", "package main\nfunc old() {}\ntype T struct{}\nfunc (T) Idle() {}\n")
	writeFixture(t, root, "analyzer", "#!/bin/sh\nif [ \"$1\" = version ]; then echo fixture-v1; exit 0; fi\n"+script+"\n")
	cfg := gc.ProjectConfig{Version: 1, Locks: []string{"go.mod"}, Checks: [][]string{{"/bin/sh", "-c", "exit 0"}}, Analyzers: []gc.Analyzer{{Language: "go", Mandate: gc.MandateDeadCode, Tool: "deadcode", Version: "fixture-v1", Command: []string{"./analyzer", "-json", "./..."}, VersionCommand: []string{"./analyzer", "version"}}}}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, root, ".takt/gc.json", string(b))
	p, err := gc.Prepare(context.Background(), root, state, "s")
	if err != nil {
		t.Fatal(err)
	}
	return root, state, p
}

const fixtureOutput = `echo '[{"Funcs":[{"Name":"old","Position":{"File":"lib.go","Line":2}},{"Name":"T.Idle","Position":{"File":"lib.go","Line":4}},{"Name":"fresh","Position":{"File":"new.go","Line":2}},{"Name":"test","Position":{"File":"lib_test.go","Line":2}},{"Name":"gone","Position":{"File":"gone.go","Line":2}}]}]'`

func TestFindingsSharePreparedSemanticsAndStableRefutationIDs(t *testing.T) {
	root, _, p := fakePreparation(t, fixtureOutput)
	writeFixture(t, root, "new.go", "package main\nfunc fresh() {}\n")
	plan := deadCodePlan("lib.go", "new.go", "lib_test.go", "gone.go", "notes.md")
	// Editing old.go must not protect all existing symbols in the touched file.
	plan.Delta = []gc.Change{{Path: "lib.go"}, {Path: "new.go", Introduced: true}, {Path: "gone.go", Deleted: true}}
	first, err := gc.AnalyzePrepared(context.Background(), root, plan, p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := gc.AnalyzePrepared(context.Background(), root, plan, p)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("API divergence: %+v %+v %v", first, second, err)
	}
	if len(first.Findings) != 3 || len(first.Gaps) != 1 || first.Coverage[0].Analyzer != "deadcode" {
		t.Fatalf("report: %+v", first)
	}
	got := map[string]gc.Finding{}
	for _, f := range first.Findings {
		got[f.ID] = f
	}
	if got["dead-code:lib.go#old"].Status != gc.StatusDead || got["dead-code:lib.go#T.Idle"].Kind != "method" || !got["dead-code:lib.go#T.Idle"].Exported || got["dead-code:new.go#fresh"].Status != gc.StatusPending {
		t.Fatalf("findings: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".codegraph")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("analysis initialized an index: %v", err)
	}
	refs, err := gc.Refute(plan, first, nil, gc.Refutation{FindingID: "dead-code:lib.go#old", Class: gc.EvidenceReflection, Evidence: "external plugin contract", Instance: "reviewer", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	// A later baseline makes the new symbol dead without changing its ID.
	p.Sources["new.go"] = gc.SourceSnapshot{Symbols: []gc.SymbolSnapshot{{Name: "fresh"}}}
	later, err := gc.AnalyzePrepared(context.Background(), root, plan, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range later.Findings {
		if f.Status != gc.StatusDead {
			t.Fatalf("pending after baseline: %+v", f)
		}
	}
	for _, f := range gc.Actionable(later.Findings, refs) {
		if f.Symbol == "old" {
			t.Fatal("lost stable refutation")
		}
	}
}

func TestFindingsFailuresAreNotEmptySuccess(t *testing.T) {
	for _, tc := range []struct {
		name, script string
		change       func(*gc.Preparation)
	}{
		{"tool failure", "echo broken >&2; exit 2", nil},
		{"deadcode exit one", "echo 'null'; exit 1", nil},
		{"malformed output", "echo broken", nil},
		{"missing finding location", `echo '[{"Funcs":[{"Name":"old","Position":{"File":"","Line":2}}]}]'`, nil},
		{"finding outside workspace", `echo '[{"Funcs":[{"Name":"old","Position":{"File":"/tmp/outside.go","Line":2}}]}]'`, nil},
		{"missing tool", "echo null", func(p *gc.Preparation) { p.Config.Analyzers[0].Command[0] = "./absent" }},
		{"version changed", "echo null", func(p *gc.Preparation) { p.Config.Analyzers[0].Version = "v2" }},
		{"missing coverage", "echo null", func(p *gc.Preparation) { p.Config.Analyzers = nil }},
		{"test roots", "echo null", func(p *gc.Preparation) {
			p.Config.Analyzers[0].Command = append(p.Config.Analyzers[0].Command, "-test")
		}},
		{"wrong engine", "echo null", func(p *gc.Preparation) { p.Config.Analyzers[0].Tool = "codegraph" }},
		{"timeout", "echo null; exec sleep 20", func(p *gc.Preparation) { p.Config.AnalyzerTimeout = "20ms" }},
		{"bad timeout", "echo null", func(p *gc.Preparation) { p.Config.AnalyzerTimeout = "0s" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, _, p := fakePreparation(t, tc.script)
			if tc.change != nil {
				tc.change(&p)
			}
			if report, err := gc.AnalyzePrepared(context.Background(), root, deadCodePlan("lib.go"), p); err == nil {
				t.Fatalf("failure became success: %+v", report)
			} else if tc.name != "bad timeout" && len(report.Gaps) == 0 {
				t.Fatalf("failure was not recorded as a coverage gap: %+v", report)
			}
		})
	}
}

func TestFindingsMissingLanguageCoverageAndDeletedFiles(t *testing.T) {
	root, _, p := fakePreparation(t, "echo null")
	if _, err := gc.AnalyzePrepared(context.Background(), root, deadCodePlan("lib.go", "tool.py"), p); err == nil {
		t.Fatal("missing Python coverage silently accepted")
	}
	plan := deadCodePlan("gone.py", "gone.go")
	plan.Delta = []gc.Change{{Path: "gone.py", Deleted: true}, {Path: "gone.go", Deleted: true}}
	p.Config.Analyzers = nil
	if r, err := gc.AnalyzePrepared(context.Background(), root, plan, p); err != nil || len(r.Findings) != 0 {
		t.Fatalf("deleted files: %+v %v", r, err)
	}
}

func TestPreparationBackwardCompatibilityAndCheckTimeout(t *testing.T) {
	root, state, p := fakePreparation(t, "echo null")
	loaded, err := gc.LoadPreparation(root, state)
	if err != nil || loaded.Config.CheckTimeout != "" {
		t.Fatalf("old state: %+v %v", loaded, err)
	}
	writeFixture(t, root, "new.go", "package main\nfunc later(){}\n")
	again, err := gc.Prepare(context.Background(), root, state, "s")
	if err != nil || !reflect.DeepEqual(p.Sources, again.Sources) {
		t.Fatalf("baseline lost on reprepare: %v", err)
	}
	if !gc.ChecksPass(gc.RunChecks(context.Background(), root, loaded)) {
		t.Fatal("legacy checks failed")
	}
	p.Config.CheckTimeout = "20ms"
	p.Config.Checks = [][]string{{"/bin/sh", "-c", "echo started; sleep 20; echo unexpected"}}
	start := time.Now()
	evs := gc.RunChecks(context.Background(), root, p)
	if gc.ChecksPass(evs) || len(evs) != 1 || evs[0].Completed || !evs[0].TimedOut || !strings.Contains(evs[0].Output, "deadline exceeded") || time.Since(start) > 3*time.Second {
		t.Fatalf("timeout lost: %+v", evs)
	}
	p.Config.CheckTimeout = "nonsense"
	if gc.ChecksPass(gc.RunChecks(context.Background(), root, p)) {
		t.Fatal("invalid timeout accepted")
	}
}

func TestRunPreparedDisablesDownloadsAndPreservesFailures(t *testing.T) {
	root := t.TempDir()
	check := []string{"/bin/sh", "-c", `printf '%s\n' "$GOPROXY,$GOSUMDB,$GOTOOLCHAIN,$npm_config_offline,$UV_OFFLINE,$PIP_NO_INDEX"; echo problem >&2; exit 7`}
	ev := gc.RunChecks(context.Background(), root, gc.Preparation{Config: gc.ProjectConfig{Checks: [][]string{check}}})[0]
	if !ev.Completed || ev.Exit != 7 || strings.TrimSpace(ev.Stdout) != "off,off,local,true,1,1" || !strings.Contains(ev.Output, "problem") {
		t.Fatalf("evidence: %+v", ev)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ev = gc.RunChecks(ctx, root, gc.Preparation{Config: gc.ProjectConfig{Checks: [][]string{{"/bin/sh", "-c", "echo null"}}}})[0]
	if ev.Completed || !strings.Contains(ev.Output, "context canceled") {
		t.Fatalf("canceled: %+v", ev)
	}
}

func TestIsTest(t *testing.T) {
	for p, want := range map[string]bool{"a/b_test.go": true, "test_x.py": true, "src/x.spec.ts": true, "x.test.js": true, "pkg/testdata/f.go": true, "tests/helper.py": true, "pkg/a.go": false, "contest.go": false, "attest/a.go": false} {
		if got := gc.IsTest(p); got != want {
			t.Errorf("IsTest(%q) = %v", p, got)
		}
	}
}
