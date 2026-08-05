package app

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forEachPackageFile parses every non-test .go file in this package.
func forEachPackageFile(t *testing.T, fn func(path string, fset *token.FileSet, file *ast.File)) {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	fset := token.NewFileSet()
	seen := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		fn(name, fset, parsed)
		seen++
	}
	// Guard against the scan silently covering nothing, which would make every
	// assertion below pass for the wrong reason.
	if seen == 0 {
		t.Fatal("no production files were scanned")
	}
}

// enclosingFunc returns the name of the function containing pos.
func enclosingFunc(file *ast.File, pos token.Pos) string {
	name := ""
	ast.Inspect(file, func(node ast.Node) bool {
		decl, ok := node.(*ast.FuncDecl)
		if !ok {
			return true
		}
		if pos >= decl.Pos() && pos <= decl.End() {
			name = decl.Name.Name
		}
		return true
	})
	return name
}

// Focus transitions must release what the area being left was holding — the
// agent panel's input, the git commit form. Assigning m.focus directly skips
// that, which left a phantom caret in the agent panel and a commit box that
// silently swallowed navigation keys. setFocus is the only place allowed to
// write the field.
func TestFocusIsAssignedOnlyThroughSetFocus(t *testing.T) {
	forEachPackageFile(t, func(path string, fset *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			assign, ok := node.(*ast.AssignStmt)
			if !ok {
				return true
			}
			for _, target := range assign.Lhs {
				selector, ok := target.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "focus" {
					continue
				}
				if enclosingFunc(file, assign.Pos()) == "setFocus" {
					continue
				}
				t.Errorf("%s: assigns .focus directly; use Model.setFocus so the previous area is released",
					fset.Position(assign.Pos()))
			}
			return true
		})
	})
}

// Update routes messages; the work belongs in named handlers so an arm can be
// found and read on its own. It grew to 1,387 lines before this was enforced,
// while the next-longest function in the package was 250.
func TestUpdateStaysARoutingFunction(t *testing.T) {
	const limit = 600

	forEachPackageFile(t, func(path string, fset *token.FileSet, file *ast.File) {
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.FuncDecl)
			if !ok || decl.Name.Name != "Update" || decl.Recv == nil {
				return true
			}
			start := fset.Position(decl.Pos()).Line
			end := fset.Position(decl.End()).Line
			if lines := end - start + 1; lines > limit {
				t.Errorf("%s: Update is %d lines (limit %d); extract the case bodies into named handlers",
					filepath.Base(path), lines, limit)
			}
			return true
		})
	})
}

// Path changes rebuild editor configuration on the Bubble Tea event loop.
// editor.New already tokenizes a byte-bounded prefix; materializing the whole
// buffer here makes Save As and tree renames pause in proportion to document
// size before the next frame can render.
func TestPathReconciliationDoesNotMaterializeBuffers(t *testing.T) {
	targets := map[string]bool{
		"reconcileSaveAs":         false,
		"reconcileTreeEditorPath": false,
	}

	forEachPackageFile(t, func(_ string, fset *token.FileSet, file *ast.File) {
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
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "Bytes" {
					t.Errorf("%s: materializes a complete buffer during path reconciliation", fset.Position(call.Pos()))
				}
				return true
			})
		}
	})

	for name, seen := range targets {
		if !seen {
			t.Errorf("path reconciliation invariant did not find %s", name)
		}
	}
}

func TestTreeLoadHandlerDoesNotBuildProjection(t *testing.T) {
	targetFound := false
	banned := map[string]bool{
		"SetFilter":         true,
		"SetShowGitIgnored": true,
		"SetShowHidden":     true,
	}
	forEachPackageFile(t, func(_ string, fset *token.FileSet, file *ast.File) {
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name.Name != "handleTreeLoaded" {
				continue
			}
			targetFound = true
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if ok && banned[selector.Sel.Name] {
					t.Errorf("%s: handleTreeLoaded must install state and dispatch one prepared projection", fset.Position(call.Pos()))
				}
				return true
			})
		}
	})
	if !targetFound {
		t.Fatal("tree-load projection invariant did not find handleTreeLoaded")
	}
}

func TestDiffLoadedHandlerDoesNotTokenizeInUpdate(t *testing.T) {
	targetFound := false
	forEachPackageFile(t, func(_ string, fset *token.FileSet, file *ast.File) {
		for _, declaration := range file.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil || fn.Name.Name != "handleDiffLoaded" {
				continue
			}
			targetFound = true
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || selector.Sel.Name != "New" {
					return true
				}
				pkg, ok := selector.X.(*ast.Ident)
				if ok && pkg.Name == "diff" {
					t.Errorf("%s: diff parsing and highlighting must be prepared before Update", fset.Position(call.Pos()))
				}
				return true
			})
		}
	})
	if !targetFound {
		t.Fatal("diff-load invariant did not find handleDiffLoaded")
	}
}
