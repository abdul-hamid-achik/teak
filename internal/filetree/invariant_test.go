package filetree

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestRefreshDispatchAndApplyDoNotBuildTreeProjections(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "filetree.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse filetree.go: %v", err)
	}
	targets := map[string]bool{
		"SnapshotForRefresh": false,
		"ApplyRefresh":       false,
	}
	banned := map[string]bool{
		"flattenEntries":                    true,
		"flatEntries":                       true,
		"filterFlatEntriesContext":          true,
		"visibleEntries":                    true,
		"mergeRefreshedEntries":             true,
		"refreshEntriesPreservingExpansion": true,
	}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if _, tracked := targets[fn.Name.Name]; !tracked {
			continue
		}
		targets[fn.Name.Name] = true
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if ok && banned[ident.Name] {
				t.Errorf("%s: %s must consume an immutable prepared projection", fset.Position(call.Pos()), fn.Name.Name)
			}
			return true
		})
	}
	for name, found := range targets {
		if !found {
			t.Errorf("refresh projection invariant did not find %s", name)
		}
	}
}
