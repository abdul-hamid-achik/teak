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
	foundView, foundFocusedView, foundDelegate := false, false, false
	foundProjection := false
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || (fn.Name.Name != "View" && fn.Name.Name != "ViewWithFocus") || fn.Body == nil {
			continue
		}
		if fn.Name.Name == "View" {
			foundView = true
		} else {
			foundFocusedView = true
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "PluginHighlightRanges":
				t.Errorf("%s: View flattens file-wide plugin highlights", fset.Position(selector.Pos()))
			case "ViewWithFocus":
				if fn.Name.Name == "View" {
					foundDelegate = true
				}
			case "pluginHighlightRangesForProjection":
				foundProjection = true
			}
			return true
		})
	}
	if !foundView {
		t.Fatal("View was not found")
	}
	if !foundFocusedView || !foundDelegate {
		t.Fatal("View must delegate to the inspected ViewWithFocus render path")
	}
	if !foundProjection {
		t.Fatal("View does not query the visible plugin highlight projection")
	}
}
