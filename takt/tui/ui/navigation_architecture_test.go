package ui_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestNavigationStateChangesOnlyThroughTransitionTables(t *testing.T) {
	_, file, _, _ := runtime.Caller(0)
	root := filepath.Dir(filepath.Dir(file))
	allowed := map[string]bool{
		"diagnostics/diagnostics.go:New":              true,
		"diagnostics/diagnostics.go:applyTransition":  true,
		"drift/drift.go:New":                          true,
		"drift/drift.go:applyTransition":              true,
		"install/install.go:New":                      true,
		"install/install.go:apply":                    true,
		"modelpicker/model_picker.go:applyTransition": true,
		"models/models.go:applyTransition":            true,
		"models/models.go:Discard":                    true,
		"tui.go:applyRoute":                           true,
		"tui.go:applyGuard":                           true,
		"uninstall/uninstall.go:New":                  true,
		"uninstall/uninstall.go:applyTransition":      true,
	}
	wanted := map[string]bool{"state": true, "step": true, "phase": true, "route": true}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		set := token.NewFileSet()
		parsed, err := parser.ParseFile(set, path, nil, 0)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil || allowed[filepath.ToSlash(relative)+":"+function.Name.Name] {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				assignment, ok := node.(*ast.AssignStmt)
				if !ok {
					return true
				}
				for _, expression := range assignment.Lhs {
					selector, ok := expression.(*ast.SelectorExpr)
					if ok && wanted[selector.Sel.Name] {
						position := set.Position(selector.Pos())
						t.Errorf("direct navigation-state assignment outside a transition-table apply: %s:%d (%s)", path, position.Line, function.Name.Name)
					}
				}
				return true
			})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
