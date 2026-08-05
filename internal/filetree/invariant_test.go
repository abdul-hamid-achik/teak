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

func TestInteractiveProjectionDispatchAndApplyDoNotTraverseTree(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "filetree.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse filetree.go: %v", err)
	}
	targets := map[string]bool{
		"ClearFilter":               false,
		"StartFilter":               false,
		"ToggleShowHiddenAsync":     false,
		"ToggleShowGitIgnoredAsync": false,
		"handleDirExpanded":         false,
		"handleFilterReady":         false,
		"toggleDir":                 false,
	}
	banned := map[string]bool{
		"baseFlatEntries":                         true,
		"filterFlatEntriesContext":                true,
		"flatEntries":                             true,
		"flattenEntries":                          true,
		"flattenVisibleEntriesContext":            true,
		"ensureCursorVisible":                     true,
		"invalidateFlatCache":                     true,
		"invalidateProjectionPreservingSelection": true,
		"restoreSelection":                        true,
		"selectedPath":                            true,
		"SetFilter":                               true,
		"visibleEntries":                          true,
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
			var name string
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			}
			if banned[name] {
				t.Errorf("%s: %s must only dispatch or install a prepared projection", fset.Position(call.Pos()), fn.Name.Name)
			}
			return true
		})
	}
	for name, found := range targets {
		if !found {
			t.Errorf("interactive projection invariant did not find %s", name)
		}
	}
}
