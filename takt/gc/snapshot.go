package gc

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// SymbolSnapshot identifies declarations, not files: editing a file never
// protects all its old dead symbols. Bodies and line shifts do not change identity.
type SymbolSnapshot struct {
	Name     string `json:"name"`
	Line     int    `json:"line"`
	End      int    `json:"end"`
	Exported bool   `json:"exported"`
}

// SourceSnapshot is one retained file: its pre-session content plus the
// function symbols parsed out of it.
type SourceSnapshot struct {
	Content string           `json:"content"`
	Symbols []SymbolSnapshot `json:"symbols"`
}

func sourceLanguage(p string) string {
	switch filepath.Ext(p) {
	case ".go":
		return "go"
	case ".ts", ".tsx":
		return "typescript"
	case ".py":
		return "python"
	}
	return ""
}
func snapshotProject(ctx context.Context, workspace string) (map[string]SourceSnapshot, error) {
	out := map[string]SourceSnapshot{}
	err := filepath.WalkDir(workspace, snapshotFile(ctx, workspace, out))
	return out, err
}

func snapshotFile(ctx context.Context, workspace string, out map[string]SourceSnapshot) fs.WalkDirFunc {
	return func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(workspace, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			return snapshotDirectory(d)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		lang := sourceLanguage(rel)
		if lang == "" && !analysisConfig(rel) {
			return nil
		}
		item, err := readSnapshot(ctx, workspace, rel, lang)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = item
		return nil
	}
}

func snapshotDirectory(d fs.DirEntry) error {
	if slices.Contains([]string{".git", "node_modules", ".venv", "venv", "vendor", ".codegraph"}, d.Name()) {
		return filepath.SkipDir
	}
	return nil
}

func readSnapshot(ctx context.Context, workspace, rel, lang string) (SourceSnapshot, error) {
	b, err := os.ReadFile(filepath.Join(workspace, rel))
	if err != nil {
		return SourceSnapshot{}, err
	}
	if len(b) > 2<<20 {
		return SourceSnapshot{}, fmt.Errorf("gc: snapshot file exceeds 2 MiB: %s", rel)
	}
	item := SourceSnapshot{Content: string(b)}
	if lang != "" && !IsTest(rel) {
		item.Symbols, err = readSymbols(ctx, workspace, rel, lang)
		if err != nil {
			return SourceSnapshot{}, fmt.Errorf("gc: AST snapshot %s: %w", rel, err)
		}
	}
	return item, nil
}
func analysisConfig(p string) bool {
	b := filepath.Base(p)
	for _, name := range []string{"eslint", "golangci", "knip", "ruff", "pylint", "jscpd", "vulture"} {
		if strings.Contains(strings.ToLower(b), name) {
			return true
		}
	}
	return slices.Contains([]string{"pyproject.toml", "package.json", "tsconfig.json", "gc.json"}, b)
}
func readSymbols(ctx context.Context, workspace, path, lang string) ([]SymbolSnapshot, error) {
	if lang == "go" {
		return readGoSymbols(workspace, path)
	}
	var argv []string
	if lang == "python" {
		argv = []string{"./.venv/bin/python", "-c", `import ast,json,sys
root=ast.parse(open(sys.argv[1]).read()); out=[]
def walk(node,prefix=''):
 for child in ast.iter_child_nodes(node):
  if isinstance(child,(ast.FunctionDef,ast.AsyncFunctionDef,ast.ClassDef)):
   name=prefix+child.name; out.append(dict(name=name,line=child.lineno,end=child.end_lineno,exported=not child.name.startswith('_'))); walk(child,name+'.')
  else: walk(child,prefix)
walk(root); print(json.dumps(out))`, path}
	} else {
		argv = []string{"node", "-e", `const ts=require('./node_modules/typescript'); const fs=require('fs'); const path=process.argv[1]; const f=ts.createSourceFile(path,fs.readFileSync(path,'utf8'),ts.ScriptTarget.Latest,true); if(f.parseDiagnostics.length) throw Error('TypeScript parse failure'); const out=[]; function walk(n,p=''){let name=n.name?.getText(f); if(name&&(ts.isFunctionDeclaration(n)||ts.isMethodDeclaration(n)||ts.isClassDeclaration(n)||ts.isVariableDeclaration(n))){out.push({name:p+name,line:f.getLineAndCharacterOfPosition(n.getStart(f)).line+1,end:f.getLineAndCharacterOfPosition(n.end).line+1,exported:!!n.modifiers?.some(m=>m.kind===ts.SyntaxKind.ExportKeyword)});p+=name+'.'} ts.forEachChild(n,c=>walk(c,p));} walk(f); console.log(JSON.stringify(out));`, path}
	}
	ev := runPreparedWithTimeout(ctx, workspace, argv, DefaultASTTimeout)
	if !ev.Completed || ev.Exit != 0 {
		return nil, fmt.Errorf("project AST dependency absent or failed: %s", ev.Output)
	}
	var out []SymbolSnapshot
	e := json.Unmarshal([]byte(ev.Stdout), &out)
	return out, e
}

func readGoSymbols(workspace, path string) ([]SymbolSnapshot, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, filepath.Join(workspace, path), nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	out := []SymbolSnapshot{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		out = append(out, goSymbol(fset, fn))
	}
	return out, nil
}

func goSymbol(fset *token.FileSet, fn *ast.FuncDecl) SymbolSnapshot {
	name := fn.Name.Name
	if fn.Recv != nil {
		if receiver := receiverName(fn); receiver != "" {
			name = receiver + "." + name
		}
	}
	return SymbolSnapshot{Name: name, Line: fset.Position(fn.Pos()).Line, End: fset.Position(fn.End()).Line, Exported: fn.Name.IsExported()}
}

func receiverName(fn *ast.FuncDecl) string {
	recv := fn.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	if id, ok := recv.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func protectSessionSymbols(ctx context.Context, workspace string, p Preparation, report *Report) error {
	current := map[string][]SymbolSnapshot{}
	for i := range report.Findings {
		f := &report.Findings[i]
		lang := sourceLanguage(f.Path)
		if lang == "" {
			continue
		}
		symbols, ok := current[f.Path]
		if !ok {
			var e error
			symbols, e = readSymbols(ctx, workspace, f.Path, lang)
			if e != nil {
				return e
			}
			current[f.Path] = symbols
		}
		protectFinding(f, symbols, p.Sources[f.Path].Symbols)
	}
	return nil
}

func protectFinding(f *Finding, current, prior []SymbolSnapshot) {
	for _, symbol := range current {
		if f.Line < symbol.Line || f.Line > symbol.End {
			continue
		}
		f.Symbol = symbol.Name
		f.Exported = f.Exported || symbol.Exported
		old := slices.ContainsFunc(prior, func(previous SymbolSnapshot) bool { return previous.Name == symbol.Name })
		f.Introduced = !old
		if f.Mandate == MandateDocumentation {
			f.Introduced = false
			f.ProposalOnly = f.ProposalOnly || old
		}
		return
	}
	f.ProposalOnly = true
}

var suppression = regexp.MustCompile(`(?i)(nolint|noqa|eslint-disable|type:\s*ignore|pylint:\s*disable|istanbul ignore|pragma:\s*no cover|ruff:\s*noqa|knip-ignore)`)

// IntegrityFindings compares retained pre-session text, never proposes removing
// old suppressions, and never authorizes removing a new one autonomously.
func IntegrityFindings(ctx context.Context, workspace string, plan Plan, p Preparation) (Report, error) {
	now, e := snapshotProject(ctx, workspace)
	if e != nil {
		return Report{}, e
	}
	out := Report{Findings: []Finding{}}
	for _, path := range plan.Closure {
		after, ok := now[path]
		if !ok {
			continue
		}
		before := p.Sources[path]
		old := map[string]int{}
		for line := range strings.SplitSeq(before.Content, "\n") {
			old[line]++
		}
		n := 0
		for line := range strings.SplitSeq(after.Content, "\n") {
			n++
			if old[line] > 0 {
				old[line]--
				continue
			}
			if suppression.MatchString(line) || analysisConfig(path) && before.Content != after.Content {
				out.Findings = append(out.Findings, Finding{ID: fmt.Sprintf("integrity:%s:%d", path, n), Path: path, Line: n, Language: sourceLanguage(path), Mandate: MandateAnalyzerIntegrity, Tool: "session-snapshot", ToolVersion: "1", Rule: "new-analysis-override", Evidence: line, ProposalOnly: true, Status: "candidate", Investigation: "uninvestigated"})
			}
		}
	}
	return out, nil
}
