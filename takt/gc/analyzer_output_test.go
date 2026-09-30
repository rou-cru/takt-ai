package gc

import (
	"strings"
	"testing"
)

func TestNormalizeAnalyzerRejectsUnknownToolAndMalformedOutput(t *testing.T) {
	if _, err := normalizeAnalyzer(Analyzer{Tool: "mystery"}, "[]"); err == nil || !strings.Contains(err.Error(), `unsupported analyzer "mystery"`) {
		t.Errorf("unknown tool error = %v", err)
	}
	for _, tool := range []string{"ruff", "eslint", "pylint", "golangci-lint", "deadcode", "knip", "jscpd"} {
		if _, err := normalizeAnalyzer(Analyzer{Tool: tool}, "not json"); err == nil {
			t.Errorf("%s accepted output that is not JSON", tool)
		}
	}
}

func TestParseESLintFatalAndDirectiveNotices(t *testing.T) {
	if _, err := parseESLint(`[{"filePath":"a.ts","messages":[{"fatal":true,"message":"Unexpected token"}]}]`); err == nil || !strings.Contains(err.Error(), "Unexpected token") {
		t.Errorf("fatal message error = %v, want the parse failure surfaced", err)
	}
	got, err := parseESLint(`[{"filePath":"a.ts","messages":[{"message":"Unused directive"},{"ruleId":"no-unused-vars","line":4,"message":"unused"}]}]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Rule != "no-unused-vars" || got[0].Line != 4 || got[0].Path != "a.ts" {
		t.Errorf("findings = %+v, want only the rule-backed message", got)
	}
}

func TestParsePylintFailsOnFatalAndErrorMessages(t *testing.T) {
	for _, id := range []string{"F0001", "E0602"} {
		raw := `[{"path":"a.py","line":1,"message":"cannot import","symbol":"x","message-id":"` + id + `"}]`
		if _, err := parsePylint(raw); err == nil || !strings.Contains(err.Error(), "pylint analysis failure") {
			t.Errorf("parsePylint(%s) error = %v, want an analysis failure", id, err)
		}
	}
	if _, err := parsePylint("not json"); err == nil {
		t.Error("parsePylint accepted output that is not JSON")
	}
}

func TestParseKnipReportsUnusedFilesAndExports(t *testing.T) {
	raw := `{"files":["dead.ts"],"issues":[{"file":"lib.ts","files":[{"name":"orphan.ts"}],"exports":[{"name":"helper","line":7}]}]}`
	got, err := parseKnip(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("findings = %+v, want three", got)
	}
	for _, f := range got[:2] {
		if f.Rule != "unused-file" || !f.ProposalOnly || f.Line != 1 {
			t.Errorf("unused-file finding = %+v, want a proposal-only file finding", f)
		}
	}
	if e := got[2]; e.Rule != "unused-export" || e.Symbol != "helper" || e.Path != "lib.ts" || e.Line != 7 || !e.Exported {
		t.Errorf("unused-export finding = %+v", e)
	}
}

func TestParseVultureSkipsBlankLinesAndRejectsGarbage(t *testing.T) {
	got, err := parseVulture("a.py:3: unused function 'f' (60% confidence)\n\nb.py:9: unused import 'os'\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Path != "a.py" || got[0].Line != 3 || got[1].Line != 9 || got[1].Rule != "unused" {
		t.Errorf("findings = %+v", got)
	}
	if _, err := parseVulture("this is not a diagnostic"); err == nil || !strings.Contains(err.Error(), "unrecognized vulture diagnostic") {
		t.Errorf("garbage line error = %v", err)
	}
	if got, err := parseVulture("  \n"); err != nil || len(got) != 0 {
		t.Errorf("parseVulture(blank) = %v, %v; want no findings", got, err)
	}
}
