package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// Zweite Nachbesserung T5, Punkt 4a: procguard.DenyInspection ist die erste
// Anweisung in main, für jedes Kommando. Vorher kam der Schutz erst nach
// store.Open, Migrate und loadPolicy, und ein Passwort aus der Umgebung
// stand bis dahin (gemessen 80 bis 360 ms) in /proc/<pid>/environ. Die
// Wirkung selbst prüft TestT5NDenyInspection (internal/procguard).
func TestT5N2DenyInspectionFirstInMain(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "main" || fn.Recv != nil {
			continue
		}
		if len(fn.Body.List) == 0 {
			t.Fatal("main is empty")
		}
		var call *ast.CallExpr
		switch s := fn.Body.List[0].(type) {
		case *ast.AssignStmt:
			if len(s.Rhs) == 1 {
				call, _ = s.Rhs[0].(*ast.CallExpr)
			}
		case *ast.ExprStmt:
			call, _ = s.X.(*ast.CallExpr)
		case *ast.IfStmt:
			if a, ok := s.Init.(*ast.AssignStmt); ok && len(a.Rhs) == 1 {
				call, _ = a.Rhs[0].(*ast.CallExpr)
			}
		}
		if call == nil {
			t.Fatal("first statement of main is not a call")
		}
		se, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || se.Sel.Name != "DenyInspection" {
			t.Fatal("first statement of main is not procguard.DenyInspection()")
		}
		if id, ok := se.X.(*ast.Ident); !ok || id.Name != "procguard" {
			t.Fatal("first statement of main is not procguard.DenyInspection()")
		}
		return
	}
	t.Fatal("func main not found")
}
