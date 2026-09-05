package editor

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestEditorRenderPathsUsePreparedDiagnosticProjection(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "editor.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse editor.go: %v", err)
	}
	wanted := map[string]bool{"View": false, "ViewWithFocus": false, "diagnosticHighlights": false}
	projectionFound := make(map[string]bool, len(wanted))
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		if _, tracked := wanted[fn.Name.Name]; !tracked {
			continue
		}
		wanted[fn.Name.Name] = true
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "Diagnostics":
				t.Errorf("%s: %s reads the file-wide diagnostic slice during rendering", fset.Position(selector.Pos()), fn.Name.Name)
			case "ViewWithFocus":
				if fn.Name.Name == "View" {
					projectionFound[fn.Name.Name] = true
				}
			case "visibleDiagnosticProjection":
				projectionFound[fn.Name.Name] = true
			}
			return true
		})
	}
	for name, found := range wanted {
		if !found {
			t.Errorf("%s was not found", name)
		} else if !projectionFound[name] {
			t.Errorf("%s does not query the prepared visible diagnostic projection", name)
		}
	}
}
