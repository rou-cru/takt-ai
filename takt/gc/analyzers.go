package gc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// pylintConventionExitMask contains pylint's convention and warning bits accepted as success.
	pylintConventionExitMask = 4 | 8 | 16
)

// AnalyzePrepared never installs tools: incomplete coverage, timeout and
// failed tools return errors, never a successful empty report.
func AnalyzePrepared(ctx context.Context, workspace string, plan Plan, p Preparation) (Report, error) {
	if err := validateTimeouts(p.Config); err != nil {
		return Report{}, err
	}
	if plan.Mandate == MandateAnalyzerIntegrity {
		return IntegrityFindings(ctx, workspace, plan, p)
	}
	out, langs := analysisScope(plan)
	for _, lang := range []string{"go", "typescript", "python"} {
		if !langs[lang] {
			continue
		}
		analyzers, err := analyzersFor(p.Config, lang, plan.Mandate)
		if err != nil {
			recordCoverageGap(&out, plan, lang, err.Error())
			return out, err
		}
		for _, a := range analyzers {
			findings, err := executeAnalyzer(ctx, workspace, a, p.Config)
			if err != nil {
				recordCoverageGap(&out, plan, lang, err.Error())
				return out, err
			}
			if err = collectFindings(workspace, plan, a, findings, &out); err != nil {
				recordCoverageGap(&out, plan, lang, err.Error())
				return out, err
			}
			out.Coverage = append(out.Coverage, analyzerCoverage(a))
		}
	}
	if err := protectSessionSymbols(ctx, workspace, p, &out); err != nil {
		return out, err
	}
	finalizeFindings(&out)
	return out, nil
}

func analysisScope(plan Plan) (Report, map[string]bool) {
	out := Report{Findings: []Finding{}}
	langs := map[string]bool{}
	for _, path := range plan.Closure {
		if excludedPath(plan, path) {
			continue
		}
		lang := sourceLanguage(path)
		if lang == "" {
			out.Gaps = append(out.Gaps, Gap{Path: path, Language: "unknown", Reason: "no analyzer declared for this file type"})
		} else {
			langs[lang] = true
		}
	}
	return out, langs
}

func excludedPath(plan Plan, path string) bool {
	return IsTest(path) || slices.ContainsFunc(plan.Delta, func(c Change) bool { return c.Path == path && c.Deleted })
}

// recordCoverageGap keeps a failed or unavailable analyzer visible to callers
// that persist the partial report. The error is still returned: a gap must not
// be mistaken for a clean analysis.
func recordCoverageGap(out *Report, plan Plan, lang, reason string) {
	for _, p := range plan.Closure {
		if excludedPath(plan, p) || sourceLanguage(p) != lang {
			continue
		}
		out.Gaps = append(out.Gaps, Gap{Path: p, Language: lang, Reason: reason})
	}
}

func analyzersFor(cfg ProjectConfig, lang string, mandate MandateClass) ([]Analyzer, error) {
	var out []Analyzer
	for _, a := range cfg.Analyzers {
		if a.Language == lang && a.Mandate == mandate {
			out = append(out, a)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("gc: no prepared %s analyzer for %s", lang, mandate)
	}
	if lang == "go" && mandate == MandateDeadCode && (len(out) != 1 || out[0].Tool != "deadcode") {
		return nil, fmt.Errorf("gc: Go dead-code requires exactly one production deadcode analyzer")
	}
	return out, nil
}

func executeAnalyzer(ctx context.Context, workspace string, a Analyzer, cfg ProjectConfig) ([]Finding, error) {
	if err := validateAnalyzer(a); err != nil {
		return nil, err
	}
	if err := verifyAnalyzer(ctx, workspace, a); err != nil {
		return nil, err
	}
	ev := runPreparedWithTimeout(ctx, workspace, a.Command, analyzerTimeout(cfg))
	if !ev.Completed || !analyzerExitOK(a.Tool, ev.Exit) {
		return nil, fmt.Errorf("gc: %s analysis failed (exit %d): %s", a.Tool, ev.Exit, ev.Output)
	}
	findings, err := normalizeAnalyzer(a, ev.Stdout)
	if err != nil {
		return nil, fmt.Errorf("gc: invalid %s output: %w", a.Tool, err)
	}
	if ev.Exit != 0 && len(findings) == 0 {
		return nil, fmt.Errorf("gc: %s failed without findings: %s", a.Tool, ev.Output)
	}
	return findings, nil
}

func analyzerExitOK(tool string, exit int) bool {
	switch tool {
	case "deadcode":
		return exit == 0
	case "vulture":
		return exit == 0 || exit == 3
	case "pylint":
		return exit >= 0 && exit&^pylintConventionExitMask == 0
	default:
		return exit == 0 || exit == 1
	}
}

func analyzerCoverage(a Analyzer) Coverage {
	if a.Tool == "deadcode" {
		return DeclaredCoverage
	}
	return Coverage{Language: a.Language, Analyzer: a.Tool}
}

func collectFindings(workspace string, plan Plan, a Analyzer, findings []Finding, out *Report) error {
	for _, f := range findings {
		if f.Path == "" || f.Line < 1 {
			return fmt.Errorf("gc: %s output has invalid finding location", a.Tool)
		}
		path, err := findingPath(workspace, f.Path, a.Tool)
		if err != nil {
			return err
		}
		f.Path = path
		if !slices.Contains(plan.Closure, f.Path) || excludedPath(plan, f.Path) {
			continue
		}
		f.Language, f.Mandate, f.Tool, f.ToolVersion = a.Language, plan.Mandate, a.Tool, a.Version
		f.Investigation, f.Status = "uninvestigated", "candidate"
		out.Findings = append(out.Findings, f)
	}
	return nil
}

// findingPath applies the same workspace boundary to relative and absolute
// analyzer paths. Relative paths are untrusted too: filepath.Clean alone would
// otherwise leave ../outside.go available for downstream consumers to resolve.
func findingPath(workspace, raw, tool string) (string, error) {
	path := raw
	if filepath.IsAbs(raw) {
		rel, err := filepath.Rel(workspace, raw)
		if err != nil {
			return "", err
		}
		path = rel
	}
	path = filepath.Clean(path)
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("gc: %s output escapes workspace: %q", tool, raw)
	}
	return filepath.ToSlash(path), nil
}

func finalizeFindings(out *Report) {
	for i := range out.Findings {
		f := &out.Findings[i]
		if f.Mandate == MandateDeadCode && f.Language == "go" {
			// Snapshot names use T.Method; preserve legacy CodeGraph T::Method IDs.
			f.Symbol = strings.ReplaceAll(f.Symbol, ".", "::")
			f.Kind, f.Status = "function", StatusDead
			if strings.Contains(f.Symbol, "::") {
				f.Kind = "method"
			}
			if f.Introduced {
				f.Status = StatusPending
			}
			f.ID = string(f.Mandate) + ":" + f.Path + "#" + f.Symbol
		} else {
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%s:%s:%s:%d", f.Mandate, f.Path, f.Symbol, f.Rule, f.Line)))
			f.ID = hex.EncodeToString(sum[:])
		}
	}
	slices.SortFunc(out.Findings, func(a, b Finding) int { return strings.Compare(a.ID, b.ID) })
	out.Findings = slices.CompactFunc(out.Findings, func(a, b Finding) bool { return a.ID == b.ID })
}
