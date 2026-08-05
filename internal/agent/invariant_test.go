package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

func TestPromptCompletionProjectionRunsInsideCommand(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "panel.go", nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse panel.go: %v", err)
	}

	updateCaseFound := false
	deferredProjectionFound := false
	for _, declaration := range file.Decls {
		fn, ok := declaration.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		switch fn.Name.Name {
		case "Update":
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				clause, ok := node.(*ast.CaseClause)
				if !ok || !caseNamesType(clause, "AgentPromptResponseMsg") {
					return true
				}
				updateCaseFound = true
				for _, statement := range clause.Body {
					ast.Inspect(statement, func(inner ast.Node) bool {
						if _, ok := inner.(*ast.RangeStmt); ok {
							t.Errorf("%s: prompt completion must not scan stream blocks in Update", fset.Position(inner.Pos()))
						}
						if callNames(inner, "preparePromptResponse") {
							t.Errorf("%s: prompt completion must dispatch projection through a tea.Cmd", fset.Position(inner.Pos()))
						}
						return true
					})
				}
				return false
			})
		case "schedulePromptFinalization":
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				literal, ok := node.(*ast.FuncLit)
				if !ok {
					return true
				}
				ast.Inspect(literal.Body, func(inner ast.Node) bool {
					if callNames(inner, "preparePromptResponse") {
						deferredProjectionFound = true
					}
					return true
				})
				return false
			})
		}
	}
	if !updateCaseFound {
		t.Fatal("AgentPromptResponseMsg case was not found in Update")
	}
	if !deferredProjectionFound {
		t.Fatal("schedulePromptFinalization does not prepare the response inside its tea.Cmd")
	}
}

func caseNamesType(clause *ast.CaseClause, name string) bool {
	for _, expression := range clause.List {
		selector, ok := expression.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == name {
			return true
		}
	}
	return false
}

func callNames(node ast.Node, name string) bool {
	call, ok := node.(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == name
}
