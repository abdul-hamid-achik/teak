package toolpath_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the test's working directory to the module root.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate go.mod above the test directory")
		}
		dir = parent
	}
}

// forEachProductionFile parses every non-test .go file in the module and hands
// the AST to fn. Test files are excluded: a test may legitimately probe the
// environment directly.
func forEachProductionFile(t *testing.T, fn func(path string, fset *token.FileSet, file *ast.File)) {
	t.Helper()
	root := repoRoot(t)
	fset := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "bin", "testdata":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		fn(path, fset, parsed)
		return nil
	})
	if err != nil {
		t.Fatalf("walking module: %v", err)
	}
}

// selectorName renders a selector expression such as `exec.LookPath` as a
// dotted string, or returns "" when the expression is not one.
func selectorName(expr ast.Expr) string {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	ident, ok := selector.X.(*ast.Ident)
	if !ok {
		return ""
	}
	return ident.Name + "." + selector.Sel.Name
}

// Resolving binaries anywhere else reintroduces the bug this package exists to
// prevent: Teak inherits whatever PATH its parent had, which routinely omits
// Homebrew, version-manager shims and ~/go/bin, so a bare lookup reports tools
// as missing that work fine from the user's own shell.
func TestBinariesAreResolvedOnlyThroughToolpath(t *testing.T) {
	const rule = "resolve it with toolpath.Resolve or toolpath.Command instead"

	forEachProductionFile(t, func(path string, fset *token.FileSet, file *ast.File) {
		if strings.Contains(filepath.ToSlash(path), "/internal/toolpath/") {
			return // this package is the one allowed to do the lookup
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			if selectorName(call.Fun) == "exec.LookPath" {
				t.Errorf("%s: exec.LookPath is not allowed here; %s",
					fset.Position(call.Pos()), rule)
			}
			return true
		})
	})
}

// A bare command name makes os/exec perform its own PATH lookup, which has the
// same blind spot as exec.LookPath. Passing an absolute path resolved by
// toolpath removes any dependency on the inherited PATH.
func TestExecIsNotGivenABareCommandName(t *testing.T) {
	forEachProductionFile(t, func(path string, fset *token.FileSet, file *ast.File) {
		if strings.Contains(filepath.ToSlash(path), "/internal/toolpath/") {
			return
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := selectorName(call.Fun)
			if name != "exec.Command" && name != "exec.CommandContext" {
				return true
			}
			// The command argument is first for Command, second for
			// CommandContext (which takes a context first).
			index := 0
			if name == "exec.CommandContext" {
				index = 1
			}
			if len(call.Args) <= index {
				return true
			}
			literal, ok := call.Args[index].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true // a variable, presumably already resolved
			}
			if strings.ContainsAny(literal.Value, "/\\") {
				return true // an explicit path, not a PATH lookup
			}
			t.Errorf("%s: %s is given the bare name %s; resolve it with toolpath.Command instead",
				fset.Position(call.Pos()), name, literal.Value)
			return true
		})
	})
}
