package gc

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

func normalizeAnalyzer(a Analyzer, raw string) ([]Finding, error) {
	parsers := map[string]func(string) ([]Finding, error){
		"ruff": parseRuff, "eslint": parseESLint, "pylint": parsePylint,
		"golangci-lint": parseGolangCI, "deadcode": parseDeadcode,
		"knip": parseKnip, "jscpd": parseJSCPD, "vulture": parseVulture,
	}
	parse, ok := parsers[a.Tool]
	if !ok {
		return nil, fmt.Errorf("unsupported analyzer %q", a.Tool)
	}
	return parse(raw)
}

func parseRuff(raw string) ([]Finding, error) {
	var rows []struct {
		Filename, Code, Message string
		Location                struct{ Row int }
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	out := []Finding{}
	for _, r := range rows {
		out = append(out, Finding{Path: r.Filename, Line: r.Location.Row, Rule: r.Code, Evidence: r.Message})
	}
	return out, nil
}

func parseESLint(raw string) ([]Finding, error) {
	var rows []struct {
		FilePath string
		Messages []struct {
			RuleID  string
			Line    int
			Message string
			Fatal   bool
		}
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	out := []Finding{}
	for _, r := range rows {
		for _, m := range r.Messages {
			if m.Fatal {
				return nil, fmt.Errorf("eslint parse failure: %s", m.Message)
			}
			if m.RuleID == "" {
				continue
			} // directive notice, not a finding
			out = append(out, Finding{Path: r.FilePath, Line: m.Line, Rule: m.RuleID, Evidence: m.Message})
		}
	}
	return out, nil
}

func parsePylint(raw string) ([]Finding, error) {
	var rows []struct {
		Path            string
		Line            int
		Message, Symbol string
		MessageID       string `json:"message-id"`
	}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		return nil, err
	}
	out := []Finding{}
	for _, r := range rows {
		if strings.HasPrefix(r.MessageID, "F") || strings.HasPrefix(r.MessageID, "E") {
			return nil, fmt.Errorf("pylint analysis failure: %s", r.Message)
		}
		out = append(out, Finding{Path: r.Path, Line: r.Line, Rule: r.MessageID, Symbol: r.Symbol, Evidence: r.Message})
	}
	return out, nil
}

func parseGolangCI(raw string) ([]Finding, error) {
	var result struct {
		Issues []struct {
			FromLinter, Text string
			Pos              struct {
				Filename string
				Line     int
			}
		}
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, err
	}
	out := []Finding{}
	for _, r := range result.Issues {
		out = append(out, Finding{Path: r.Pos.Filename, Line: r.Pos.Line, Rule: r.FromLinter, Evidence: r.Text})
	}
	return out, nil
}

func parseDeadcode(raw string) ([]Finding, error) {
	// deadcode prints JSON null (not []) when no functions are unreachable.
	var packages []struct {
		Funcs []struct {
			Name     string
			Position struct {
				File string
				Line int
			}
		}
	}
	if err := json.Unmarshal([]byte(raw), &packages); err != nil {
		return nil, err
	}
	out := []Finding{}
	for _, p := range packages {
		for _, f := range p.Funcs {
			if f.Name == "" || f.Position.File == "" || f.Position.Line < 1 {
				return nil, fmt.Errorf("deadcode: missing function identity or position")
			}
			out = append(out, Finding{Path: f.Position.File, Line: f.Position.Line, Symbol: f.Name, Rule: "unreachable", Evidence: "deadcode RTA: unreachable from production main/init entry points"})
		}
	}
	return out, nil
}

func parseKnip(raw string) ([]Finding, error) {
	type located struct {
		Name string
		Line int
	}
	var result struct {
		Files  []string
		Issues []struct {
			File    string
			Files   []struct{ Name string }
			Exports []located
		}
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, err
	}
	out := []Finding{}
	unusedFile := func(path string) {
		out = append(out, Finding{Path: path, Line: 1, Rule: "unused-file", Evidence: "Knip production graph: unused file", ProposalOnly: true})
	}
	for _, p := range result.Files {
		unusedFile(p)
	}
	for _, r := range result.Issues {
		for _, f := range r.Files {
			unusedFile(f.Name)
		}
		for _, f := range r.Exports {
			out = append(out, Finding{Path: r.File, Line: f.Line, Symbol: f.Name, Rule: "unused-export", Evidence: "Knip production graph: unused export", Exported: true})
		}
	}
	return out, nil
}

func parseJSCPD(raw string) ([]Finding, error) {
	type location struct {
		Name  string
		Start int
	}
	var result struct {
		Duplicates []struct {
			FirstFile, SecondFile location
			Fragment              string
		}
	}
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, err
	}
	out := []Finding{}
	for _, r := range result.Duplicates {
		out = append(out, Finding{Path: r.FirstFile.Name, Line: r.FirstFile.Start, Rule: "duplicate", Evidence: fmt.Sprintf("duplicate with %s:%d\n%s", r.SecondFile.Name, r.SecondFile.Start, r.Fragment)})
	}
	return out, nil
}

// vulturePattern matches vulture's "path:line: message" diagnostics.
var vulturePattern = regexp.MustCompile(`^(.+):(\d+): (.+)$`)

func parseVulture(raw string) ([]Finding, error) {
	out := []Finding{}
	for line := range strings.SplitSeq(strings.TrimSpace(raw), "\n") {
		if line == "" {
			continue
		}
		m := vulturePattern.FindStringSubmatch(line)
		if m == nil {
			return nil, fmt.Errorf("unrecognized vulture diagnostic: %s", line)
		}
		n, _ := strconv.Atoi(m[2])
		out = append(out, Finding{Path: m[1], Line: n, Rule: "unused", Evidence: m[3]})
	}
	return out, nil
}
