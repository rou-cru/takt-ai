package gc

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const (
	// oversizedSnapshotBytes is one byte over the snapshot size cap (2 MiB).
	oversizedSnapshotBytes = 2<<20 + 1
	goSource               = "package p\n\nfunc Plain() {}\n\ntype T struct{}\n\nfunc (T) Value() {}\n\nfunc (t *T) Pointer() {}\n\nfunc unexported() {\n\t_ = 1\n}\n"
)

func TestSourceLanguageAndAnalysisConfig(t *testing.T) {
	for path, want := range map[string]string{
		"a.go": "go", "dir/b.ts": "typescript", "c.tsx": "typescript", "d.py": "python", "e.md": "", "noext": "",
	} {
		if got := sourceLanguage(path); got != want {
			t.Errorf("sourceLanguage(%q) = %q, want %q", path, got, want)
		}
	}
	for path, want := range map[string]bool{
		".eslintrc.json": true, "sub/.golangci.yml": true, "knip.json": true, "ruff.toml": true, ".pylintrc": true,
		".jscpd.json": true, ".vulture": true, "pyproject.toml": true, "package.json": true, "tsconfig.json": true,
		"gc.json": true, "README.md": false, "main.go": false,
	} {
		if got := analysisConfig(path); got != want {
			t.Errorf("analysisConfig(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestSnapshotProjectSkipsVendoredTreesAndSymlinks(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, "main.go", goSource, 0o644)
	putFile(t, root, "main_test.go", "package p\n", 0o644)
	putFile(t, root, "package.json", "{}", 0o644)
	putFile(t, root, "notes.txt", "ignored", 0o644)
	for _, dir := range []string{".git", "node_modules", ".venv", "venv", "vendor", ".codegraph"} {
		putFile(t, root, dir+"/hidden.go", "package p\nfunc Hidden() {}\n", 0o644)
	}
	if err := os.Symlink(filepath.Join(root, "main.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}

	got, err := snapshotProject(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for p := range got {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	if want := []string{"main.go", "main_test.go", "package.json"}; !slices.Equal(paths, want) {
		t.Fatalf("snapshot paths = %v, want %v", paths, want)
	}
	if got["main_test.go"].Symbols != nil {
		t.Errorf("test file symbols = %v, want none parsed", got["main_test.go"].Symbols)
	}
	if got["main.go"].Content != goSource {
		t.Error("snapshot content differs from the file")
	}
}

func TestSnapshotProjectRejectsOversizedAndBrokenFiles(t *testing.T) {
	t.Run("oversized", func(t *testing.T) {
		root := t.TempDir()
		putFile(t, root, "package.json", strings.Repeat("x", oversizedSnapshotBytes), 0o644)
		if _, err := snapshotProject(context.Background(), root); err == nil || !strings.Contains(err.Error(), "exceeds 2 MiB") {
			t.Fatalf("error = %v, want the size rejection", err)
		}
	})
	t.Run("unparsable Go", func(t *testing.T) {
		root := t.TempDir()
		putFile(t, root, "bad.go", "this is not go", 0o644)
		if _, err := snapshotProject(context.Background(), root); err == nil || !strings.Contains(err.Error(), "AST snapshot bad.go") {
			t.Fatalf("error = %v, want the AST failure naming the file", err)
		}
	})
	t.Run("missing workspace", func(t *testing.T) {
		if _, err := snapshotProject(context.Background(), filepath.Join(t.TempDir(), "absent")); err == nil {
			t.Fatal("error = nil")
		}
	})
	t.Run("python without project interpreter", func(t *testing.T) {
		root := t.TempDir()
		putFile(t, root, "mod.py", "def f(): pass\n", 0o644)
		if _, err := snapshotProject(context.Background(), root); err == nil || !strings.Contains(err.Error(), "AST dependency absent") {
			t.Fatalf("error = %v, want the absent-dependency rejection", err)
		}
	})
	t.Run("typescript without project compiler", func(t *testing.T) {
		root := t.TempDir()
		putFile(t, root, "mod.ts", "export function f() {}\n", 0o644)
		if _, err := snapshotProject(context.Background(), root); err == nil {
			t.Fatal("error = nil, want the missing node_modules/typescript to fail closed")
		}
	})
}

func TestReadGoSymbolsNamesReceiversAndVisibility(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, "p.go", goSource, 0o644)
	symbols, err := readGoSymbols(root, "p.go")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]SymbolSnapshot{}
	for _, s := range symbols {
		byName[s.Name] = s
	}
	for name, exported := range map[string]bool{"Plain": true, "T.Value": true, "T.Pointer": true, "unexported": false} {
		s, ok := byName[name]
		if !ok || s.Exported != exported {
			t.Errorf("symbol %q = %+v (present %v), want exported=%v", name, s, ok, exported)
		}
	}
	if u := byName["unexported"]; u.Line != 11 || u.End != 13 {
		t.Errorf("unexported spans lines %d-%d, want 11-13", u.Line, u.End)
	}
}

func TestReceiverNameIgnoresNonIdentifierReceivers(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, "g.go", "package p\n\ntype G[T any] struct{}\n\nfunc (G[T]) Method() {}\n", 0o644)
	symbols, err := readGoSymbols(root, "g.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 1 || symbols[0].Name != "Method" {
		t.Fatalf("symbols = %+v, want the generic receiver left off the name", symbols)
	}
}

func TestProtectFindingClassifiesAgainstPriorSymbols(t *testing.T) {
	current := []SymbolSnapshot{
		{Name: "Old", Line: 1, End: 5, Exported: true},
		{Name: "fresh", Line: 7, End: 9},
	}
	prior := []SymbolSnapshot{{Name: "Old"}}

	cases := []struct {
		name             string
		finding          Finding
		wantSymbol       string
		wantIntroduced   bool
		wantProposalOnly bool
		wantExported     bool
	}{
		{"pre-existing symbol", Finding{Line: 3}, "Old", false, false, true},
		{"symbol introduced this session", Finding{Line: 8}, "fresh", true, false, false},
		{"line outside every symbol", Finding{Line: 6}, "", false, true, false},
		{"old symbol, documentation mandate", Finding{Line: 2, Mandate: MandateDocumentation}, "Old", false, true, true},
		{"new symbol, documentation mandate", Finding{Line: 8, Mandate: MandateDocumentation}, "fresh", false, false, false},
		{"already exported finding stays exported", Finding{Line: 8, Exported: true}, "fresh", true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.finding
			protectFinding(&f, current, prior)
			if f.Symbol != tc.wantSymbol || f.Introduced != tc.wantIntroduced || f.ProposalOnly != tc.wantProposalOnly || f.Exported != tc.wantExported {
				t.Errorf("protectFinding() = %+v", f)
			}
		})
	}
}

func TestProtectSessionSymbolsAnnotatesFindingsFromCurrentAST(t *testing.T) {
	root := t.TempDir()
	putFile(t, root, "p.go", goSource, 0o644)
	prepared := Preparation{Sources: map[string]SourceSnapshot{
		"p.go": {Symbols: []SymbolSnapshot{{Name: "Plain"}}},
	}}
	report := Report{Findings: []Finding{
		{ID: "a", Path: "p.go", Line: 3},  // Plain: existed before
		{ID: "b", Path: "p.go", Line: 12}, // unexported: created this session
		{ID: "c", Path: "p.go", Line: 1},  // package clause: no symbol
		{ID: "d", Path: "notes.md", Line: 1},
	}}
	if err := protectSessionSymbols(context.Background(), root, prepared, &report); err != nil {
		t.Fatal(err)
	}
	a, b, c, d := report.Findings[0], report.Findings[1], report.Findings[2], report.Findings[3]
	if a.Symbol != "Plain" || a.Introduced {
		t.Errorf("finding a = %+v, want the pre-existing symbol", a)
	}
	if b.Symbol != "unexported" || !b.Introduced {
		t.Errorf("finding b = %+v, want the session-created symbol", b)
	}
	if c.Symbol != "" || !c.ProposalOnly {
		t.Errorf("finding c = %+v, want proposal-only with no symbol", c)
	}
	if d.Symbol != "" || d.ProposalOnly {
		t.Errorf("finding d = %+v, want a file without a language left untouched", d)
	}

	t.Run("unparsable source fails", func(t *testing.T) {
		putFile(t, root, "p.go", "broken", 0o644)
		bad := Report{Findings: []Finding{{ID: "x", Path: "p.go", Line: 1}}}
		if err := protectSessionSymbols(context.Background(), root, prepared, &bad); err == nil {
			t.Fatal("error = nil")
		}
	})
}

func TestIntegrityFindingsFlagsNewSuppressionsAndConfigEdits(t *testing.T) {
	root := t.TempDir()
	before := "package p\n\nfunc a() {} //nolint:unused\n\nfunc b() {}\n"
	putFile(t, root, "p.go", before, 0o644)
	putFile(t, root, "package.json", `{"a":1}`, 0o644)
	putFile(t, root, "untouched.go", "package p\n", 0o644)
	prepared := Preparation{Sources: map[string]SourceSnapshot{
		"p.go":          {Content: before},
		"package.json":  {Content: `{"a":1}`},
		"untouched.go":  {Content: "package p\n"},
		"deleted.go":    {Content: "package p\n"},
		"unlisted.go":   {Content: "package p\n"},
		"config-new.md": {},
	}}
	plan := Plan{Closure: []string{"p.go", "package.json", "untouched.go", "deleted.go", "missing-in-closure.go"}}

	// Nothing changed: an old suppression is never proposed for removal.
	report, err := IntegrityFindings(context.Background(), root, plan, prepared)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("findings = %+v, want none for an unchanged tree", report.Findings)
	}

	// A new suppression and an edited analysis config are flagged.
	putFile(t, root, "p.go", before+"func c() {} // noqa\nfunc d() {} // Nolint\n", 0o644)
	putFile(t, root, "package.json", `{"a":2}`, 0o644)
	report, err = IntegrityFindings(context.Background(), root, plan, prepared)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, f := range report.Findings {
		ids = append(ids, f.ID)
		if !f.ProposalOnly || f.Mandate != MandateAnalyzerIntegrity || f.Rule != "new-analysis-override" || f.Status != "candidate" {
			t.Errorf("finding %+v is not a proposal-only integrity candidate", f)
		}
	}
	slices.Sort(ids)
	want := []string{"integrity:p.go:6", "integrity:p.go:7", "integrity:package.json:1"}
	if !slices.Equal(ids, want) {
		t.Fatalf("finding ids = %v, want %v", ids, want)
	}

	t.Run("unsnapshotable workspace", func(t *testing.T) {
		if _, err := IntegrityFindings(context.Background(), filepath.Join(t.TempDir(), "absent"), plan, prepared); err == nil {
			t.Fatal("error = nil")
		}
	})
}

func TestRefutationStoreRoundTripAndFailures(t *testing.T) {
	state := t.TempDir()
	if refs, err := LoadRefutations(state, "c1"); err != nil || len(refs) != 0 {
		t.Fatalf("LoadRefutations(fresh) = %v, %v; want none", refs, err)
	}
	first := []Refutation{{FindingID: "f1"}}
	if err := SaveRefutations(state, "c1", first); err != nil {
		t.Fatal(err)
	}
	if err := SaveRefutations(state, "c2", []Refutation{{FindingID: "f2"}}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRefutations(state, "c1")
	if err != nil || len(got) != 1 || got[0].FindingID != "f1" {
		t.Fatalf("LoadRefutations(c1) = %+v, %v; want the cycle's own refutations", got, err)
	}

	putFile(t, state, refutationsFile, "{ nope", 0o600)
	if _, err := LoadRefutations(state, "c1"); err == nil {
		t.Error("LoadRefutations(corrupt) error = nil")
	}
	if err := SaveRefutations(state, "c1", first); err == nil {
		t.Error("SaveRefutations over a corrupt store error = nil, want it refused rather than overwritten")
	}

	unreadable := t.TempDir()
	if err := os.MkdirAll(filepath.Join(unreadable, refutationsFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRefutations(unreadable, "c1"); err == nil {
		t.Error("LoadRefutations(directory in place of file) error = nil")
	}
	blocker := filepath.Join(t.TempDir(), "state-is-a-file")
	putFile(t, filepath.Dir(blocker), filepath.Base(blocker), "x", 0o600)
	if err := SaveRefutations(blocker, "c1", first); err == nil {
		t.Error("SaveRefutations where the state path is a file error = nil")
	}
}
