package diff

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func parseViewFile(t *testing.T) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "view.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse view.go: %v", err)
	}
	return fset, file
}

func TestDiffEventHandlersDoNotTokenizeSynchronously(t *testing.T) {
	fset, file := parseViewFile(t)
	targets := map[string]bool{"Update": false, "ApplyHighlight": false, "buildHighlighting": false}
	banned := map[string]bool{
		"Tokenize":                      true,
		"TokenizeViewportSnapshotBatch": true,
		"PrepareViewport":               true,
		"prepareViewportHighlight":      true,
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
			name := ""
			switch called := call.Fun.(type) {
			case *ast.Ident:
				name = called.Name
			case *ast.SelectorExpr:
				name = called.Sel.Name
			}
			if banned[name] {
				t.Errorf("%s: %s must be scheduled outside the event handler", fset.Position(call.Pos()), name)
			}
			return true
		})
	}
	for name, found := range targets {
		if !found {
			t.Errorf("diff invariant did not find %s", name)
		}
	}
}

func TestDiffGutterWidthUsesPreparedMetadata(t *testing.T) {
	fset, file := parseViewFile(t)
	found := false
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Body == nil || fn.Name.Name != "gutterWidth" {
			continue
		}
		found = true
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if loop, ok := node.(*ast.RangeStmt); ok {
				t.Errorf("%s: gutter width must not scan every diff line during View", fset.Position(loop.Pos()))
			}
			return true
		})
	}
	if !found {
		t.Fatal("diff invariant did not find gutterWidth")
	}
}
