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
