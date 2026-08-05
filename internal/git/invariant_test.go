package git

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestRefreshApplicationDoesNotBuildRepositoryProjection(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "panel.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse panel.go: %v", err)
	}

	found := false
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "ApplyPreparedRefresh" || fn.Body == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if !ok {
				return true
			}
			switch ident.Name {
			case "buildTree", "buildTreeRowCache", "deriveGroups":
				t.Errorf("%s: repository projection must be built in the preparation command", fset.Position(call.Pos()))
			}
			return true
		})
	}
	if !found {
		t.Fatal("ApplyPreparedRefresh was not found")
	}
}

func TestInputHandlersDoNotBuildTreeRows(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "panel.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse panel.go: %v", err)
	}

	wanted := map[string]bool{"handleClick": false, "handleKey": false}
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if _, ok := wanted[fn.Name.Name]; !ok {
			continue
		}
		wanted[fn.Name.Name] = true
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && selector.Sel.Name == "rebuildTreeRowCache" {
				t.Errorf("%s: input handlers must schedule tree-row preparation", fset.Position(call.Pos()))
			}
			return true
		})
	}
	for name, found := range wanted {
		if !found {
			t.Errorf("%s was not found", name)
		}
	}
}

func TestTreeRowSnapshotHasNoSynchronousFallback(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "panel.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse panel.go: %v", err)
	}

	found := false
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "treeRowsSnapshot" || fn.Body == nil {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			ident, ok := call.Fun.(*ast.Ident)
			if ok && (ident.Name == "buildTreeRowCache" || ident.Name == "flattenTree") {
				t.Errorf("%s: cache lookup must not build a repository-sized fallback", fset.Position(call.Pos()))
			}
			return true
		})
	}
	if !found {
		t.Fatal("treeRowsSnapshot was not found")
	}
}
