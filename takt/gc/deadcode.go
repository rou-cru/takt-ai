package gc

import (
	"path"
	"slices"
	"strings"
)

// Dead-code statuses as reported by analyzers.
const (
	// StatusDead marks a finding the analysis judged dead.
	StatusDead = "dead"
	// StatusPending marks a finding awaiting investigation before it counts as dead.
	StatusPending = "pending"
)

// Finding is a tool-computed candidate, not permission to mutate source.
type Finding struct {
	// Mandate is the mandate class the finding's analyzer serves.
	Mandate MandateClass `json:"mandate,omitempty"`
	// Language is the source language the analyzer covered.
	Language string `json:"language,omitempty"`
	// Tool is the analyzer binary that produced the finding.
	Tool string `json:"tool,omitempty"`
	// ToolVersion is the exact analyzer version that produced it.
	ToolVersion string `json:"tool_version,omitempty"`
	// Rule is the analyzer's rule or check identifier.
	Rule string `json:"rule,omitempty"`
	// Evidence is the analyzer's own justification for the finding.
	Evidence string `json:"evidence,omitempty"`
	// Investigation records the investigation that settled the finding, if any.
	Investigation string `json:"investigation,omitempty"`
	// ProposalOnly marks findings a demoted mandate may state but not act on.
	ProposalOnly bool `json:"proposal_only,omitempty"`
	// Introduced marks findings created inside the current cycle's window.
	Introduced bool `json:"introduced,omitempty"`
	// Go dead-code IDs retain mandate:path#qualified-name, independent of lines
	// and pending status, so existing refutations continue to match.
	ID       string `json:"id"`
	Status   string `json:"status"`
	Path     string `json:"path"`
	Symbol   string `json:"symbol"`
	Kind     string `json:"kind"`
	Line     int    `json:"line"`
	Exported bool   `json:"exported,omitempty"`
}

// Coverage declares one analyzer's reach so reports state what was seen.
type Coverage struct {
	Language string   `json:"language"`
	Analyzer string   `json:"analyzer"`
	Kinds    []string `json:"kinds"`
	// Blind names what the analysis could not see, so gaps stay explicit.
	Blind string `json:"blind"`
}

// DeclaredCoverage describes the production RTA analysis, not CodeGraph's index.
var DeclaredCoverage = Coverage{
	Language: "go", Analyzer: "deadcode", Kinds: []string{"function", "method"},
	Blind: "production main/init roots under the current build configuration; requires a main package; reflection, plugins, external library clients and other build tags need investigation; constants, variables and types are not judged; test references are not collected",
}

// Gap is one coverage hole: a path, its language and why it was skipped.
type Gap struct {
	Path     string `json:"path"`
	Language string `json:"language"`
	Reason   string `json:"reason"`
}

// Report is one analysis pass: findings plus the coverage they rest on.
type Report struct {
	Findings []Finding  `json:"findings"`
	Coverage []Coverage `json:"coverage"`
	Gaps     []Gap      `json:"gaps,omitempty"`
}

// IsTest reports whether p looks like test code, so liveness analysis can
// exclude it: test filename stems and test directories count, extensions
// decide nothing.
func IsTest(p string) bool {
	base := path.Base(p)
	stem := strings.TrimSuffix(base, path.Ext(base))
	if strings.HasSuffix(stem, "_test") || strings.HasPrefix(stem, "test_") || base == "conftest.py" || strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec") {
		return true
	}
	for _, dir := range strings.Split(path.Dir(p), "/") {
		if slices.Contains([]string{"test", "tests", "__tests__", "testdata"}, dir) {
			return true
		}
	}
	return false
}
