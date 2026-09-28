package gc_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rou-cru/takt-ai/takt/gc"
)

func writeAnalyzerFixture(t *testing.T, root, name, content string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

// analyzerPreparation wires a single project-declared analyzer whose command
// is a fake "./analyzer" shell script standing in for the real ruff/eslint/
// pylint/golangci-lint/knip/jscpd/vulture binary — the same fake-external-
// tool convention deadcode_test.go's fakePreparation already established for
// deadcode. Each parser it exercises (analyzer_parsers.go) is otherwise only
// reachable through a real subprocess, so this is the black-box seam for it.
func analyzerPreparation(t *testing.T, mandate gc.MandateClass, lang, tool, sourceFile, script string) (string, gc.Plan, gc.Preparation) {
	t.Helper()
	root, state := t.TempDir(), t.TempDir()
	writeAnalyzerFixture(t, root, "lock.txt", "v1\n")
	writeAnalyzerFixture(t, root, "analyzer", "#!/bin/sh\nif [ \"$1\" = version ]; then echo fixture-v1; exit 0; fi\n"+script+"\n")

	// AnalyzePrepared's own protectSessionSymbols step AST-snapshots every
	// finding's file, independent of the analyzer under test: for go via
	// go/parser directly (needs the real file on disk), for python/typescript
	// via a real project-local ./.venv/bin/python or "node" on PATH. Fake
	// those the same way as the analyzer binary itself — external tools
	// standing behind a seam, not business logic.
	switch lang {
	case "go":
		writeAnalyzerFixture(t, root, sourceFile, "package fixture\n\nfunc F() {}\n")
	case "python":
		writeAnalyzerFixture(t, root, ".venv/bin/python", "#!/bin/sh\necho '[]'\n")
	case "typescript":
		nodeDir := t.TempDir()
		writeAnalyzerFixture(t, nodeDir, "node", "#!/bin/sh\necho '[]'\n")
		t.Setenv("PATH", nodeDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}
	cfg := gc.ProjectConfig{
		Version: 1, Locks: []string{"lock.txt"},
		Checks: [][]string{{"/bin/sh", "-c", "exit 0"}},
		Analyzers: []gc.Analyzer{{
			Language: lang, Mandate: mandate, Tool: tool, Version: "fixture-v1",
			Command: []string{"./analyzer", "-json"}, VersionCommand: []string{"./analyzer", "version"},
		}},
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeAnalyzerFixture(t, root, ".takt/gc.json", string(b))
	p, err := gc.Prepare(context.Background(), root, state, "s")
	if err != nil {
		t.Fatal(err)
	}
	plan := gc.Plan{Request: gc.Request{SessionID: "s", CycleID: "c", Mandate: mandate}, Closure: []string{sourceFile}}
	return root, plan, p
}

func runAnalyzer(t *testing.T, mandate gc.MandateClass, lang, tool, sourceFile, script string) (gc.Report, error) {
	t.Helper()
	root, plan, p := analyzerPreparation(t, mandate, lang, tool, sourceFile, script)
	return gc.AnalyzePrepared(context.Background(), root, plan, p)
}

func TestAnalyzerParsersProduceFindingsFromToolOutput(t *testing.T) {
	for _, tc := range []struct {
		name, lang, tool, file, script string
		mandate                        gc.MandateClass
		check                          func(t *testing.T, f gc.Finding)
	}{
		{
			name: "ruff", lang: "python", tool: "ruff", file: "a.py", mandate: gc.MandateComplexity,
			script: `echo '[{"Filename":"a.py","Code":"E1","Message":"bad","Location":{"Row":3}}]'; exit 0`,
			check: func(t *testing.T, f gc.Finding) {
				if f.Line != 3 || f.Rule != "E1" || f.Evidence != "bad" {
					t.Fatalf("ruff finding = %+v", f)
				}
			},
		},
		{
			name: "eslint", lang: "typescript", tool: "eslint", file: "a.ts", mandate: gc.MandateComplexity,
			script: `echo '[{"FilePath":"a.ts","Messages":[{"RuleID":"no-unused","Line":2,"Message":"m"}]}]'; exit 0`,
			check: func(t *testing.T, f gc.Finding) {
				if f.Line != 2 || f.Rule != "no-unused" || f.Evidence != "m" {
					t.Fatalf("eslint finding = %+v", f)
				}
			},
		},
		{
			name: "pylint", lang: "python", tool: "pylint", file: "a.py", mandate: gc.MandateComplexity,
			script: `echo '[{"Path":"a.py","Line":4,"Message":"msg","Symbol":"sym","message-id":"C0111"}]'; exit 0`,
			check: func(t *testing.T, f gc.Finding) {
				if f.Line != 4 || f.Rule != "C0111" || f.Symbol != "sym" || f.Evidence != "msg" {
					t.Fatalf("pylint finding = %+v", f)
				}
			},
		},
		{
			name: "golangci-lint", lang: "go", tool: "golangci-lint", file: "a.go", mandate: gc.MandateComplexity,
			script: `echo '{"Issues":[{"FromLinter":"govet","Text":"t","Pos":{"Filename":"a.go","Line":5}}]}'; exit 0`,
			check: func(t *testing.T, f gc.Finding) {
				if f.Line != 5 || f.Rule != "govet" || f.Evidence != "t" {
					t.Fatalf("golangci-lint finding = %+v", f)
				}
			},
		},
		{
			name: "knip", lang: "typescript", tool: "knip", file: "a.ts", mandate: gc.MandateComplexity,
			script: `echo '{"Issues":[{"File":"a.ts","Exports":[{"Name":"Foo","Line":6}]}]}'; exit 0`,
			check: func(t *testing.T, f gc.Finding) {
				if f.Line != 6 || f.Rule != "unused-export" || f.Symbol != "Foo" || !f.Exported {
					t.Fatalf("knip finding = %+v", f)
				}
			},
		},
		{
			name: "jscpd", lang: "typescript", tool: "jscpd", file: "a.ts", mandate: gc.MandateDuplication,
			script: `echo '{"Duplicates":[{"FirstFile":{"Name":"a.ts","Start":1},"SecondFile":{"Name":"b.ts","Start":9},"Fragment":"dup"}]}'; exit 0`,
			check: func(t *testing.T, f gc.Finding) {
				if f.Line != 1 || f.Rule != "duplicate" {
					t.Fatalf("jscpd finding = %+v", f)
				}
			},
		},
		{
			name: "vulture", lang: "python", tool: "vulture", file: "a.py", mandate: gc.MandateComplexity,
			script: `echo 'a.py:7: unused variable (v)'; exit 0`,
			check: func(t *testing.T, f gc.Finding) {
				if f.Line != 7 || f.Rule != "unused" {
					t.Fatalf("vulture finding = %+v", f)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			report, err := runAnalyzer(t, tc.mandate, tc.lang, tc.tool, tc.file, tc.script)
			if err != nil {
				t.Fatalf("AnalyzePrepared() error = %v", err)
			}
			if len(report.Findings) != 1 {
				t.Fatalf("Findings = %+v, want exactly one", report.Findings)
			}
			tc.check(t, report.Findings[0])
		})
	}
}

func TestAnalyzerParsersRejectInvalidOutput(t *testing.T) {
	for _, tc := range []struct {
		name, lang, tool, file string
		mandate                gc.MandateClass
		script                 string
	}{
		{"ruff malformed", "python", "ruff", "a.py", gc.MandateComplexity, `echo 'not json'; exit 0`},
		{"eslint fatal", "typescript", "eslint", "a.ts", gc.MandateComplexity, `echo '[{"FilePath":"a.ts","Messages":[{"Fatal":true,"Message":"parse error"}]}]'; exit 0`},
		{"pylint fatal", "python", "pylint", "a.py", gc.MandateComplexity, `echo '[{"Path":"a.py","Line":1,"Message":"crash","message-id":"E0001"}]'; exit 0`},
		{"golangci-lint malformed", "go", "golangci-lint", "a.go", gc.MandateComplexity, `echo 'not json'; exit 0`},
		{"knip malformed", "typescript", "knip", "a.ts", gc.MandateComplexity, `echo 'not json'; exit 0`},
		{"jscpd malformed", "typescript", "jscpd", "a.ts", gc.MandateDuplication, `echo 'not json'; exit 0`},
		{"vulture unrecognized", "python", "vulture", "a.py", gc.MandateComplexity, `echo 'not a vulture line'; exit 0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := runAnalyzer(t, tc.mandate, tc.lang, tc.tool, tc.file, tc.script); err == nil {
				t.Fatal("AnalyzePrepared() error = nil, want an error for invalid tool output")
			}
		})
	}
}
