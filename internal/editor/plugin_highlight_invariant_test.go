package editor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestEditorViewUsesVisiblePluginHighlightProjection(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "editor.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse editor.go: %v", err)
	}
	foundView := false
	foundProjection := false
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "View" || fn.Body == nil {
			continue
		}
		foundView = true
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "PluginHighlightRanges":
				t.Errorf("%s: View flattens file-wide plugin highlights", fset.Position(selector.Pos()))
			case "pluginHighlightRangesForProjection":
				foundProjection = true
			}
			return true
		})
	}
	if !foundView {
		t.Fatal("View was not found")
	}
	if !foundProjection {
		t.Fatal("View does not query the visible plugin highlight projection")
	}
}
