package main

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

// findMainSource returns the parsed main.go.
func parseMain(t *testing.T) (*token.FileSet, *ast.File) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	return fset, f
}

// argsOfCall returns the source text of each argument in a call to name.
func argsOfCall(fset *token.FileSet, f *ast.File, name string) [][]string {
	var out [][]string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			if fn.Sel.Name != name {
				return true
			}
		case *ast.Ident:
			// Plain call: runCheckPage(...). Matching only selectors would have
			// found zero calls and the test would have "passed" vacuously.
			if fn.Name != name {
				return true
			}
		default:
			return true
		}
		args := make([]string, 0, len(call.Args))
		for _, a := range call.Args {
			args = append(args, exprSource(fset, a))
		}
		out = append(out, args)
		return true
	})
	return out
}

// The --check call site must receive the SOURCE slice.
//
// This started as a helper bug (verdicts written to filtered copies), got fixed
// in the helper, and the smoke still showed the wrong model named — because
// main.go was still passing `visible`. Every helper-level mutation stayed green
// through that, correctly: the helper was right and the caller was wrong.
//
// So this asserts the CALL SITE by reading the AST. Asserting behaviour by
// replicating it in the test is what let the bug survive in the first place.
func TestCheckCallSiteReceivesTheSourceSlice(t *testing.T) {
	fset, f := parseMain(t)
	calls := argsOfCall(fset, f, "runCheckPage")
	if len(calls) != 1 {
		t.Fatalf("esperava 1 chamada a runCheckPage, achei %d", len(calls))
	}
	args := calls[0]
	if len(args) < 3 {
		t.Fatalf("runCheckPage com %d argumentos", len(args))
	}
	slice := args[2]
	if slice != "entries" {
		t.Errorf("runCheckPage recebe %q; precisa de \"entries\" (o slice de origem), "+
			"porque catalog.Filter devolve copias e o re-render repinta a partir de entries", slice)
	}
}

// Same reasoning for the outcome lines: reading them from `visible` would name
// a model that is not on the page the operator is looking at.
func TestCheckOutcomeLinesCallSiteReadsTheSourceSlice(t *testing.T) {
	fset, f := parseMain(t)
	calls := argsOfCall(fset, f, "checkOutcomeLines")
	if len(calls) != 1 {
		t.Fatalf("esperava 1 chamada a checkOutcomeLines, achei %d", len(calls))
	}
	if got := calls[0][0]; got != "entries" {
		t.Errorf("checkOutcomeLines le %q; precisa de \"entries\"", got)
	}
}

// The re-render after a check is not optional: without it the table shown
// directly under the verdict lines contradicts them.
func TestCheckRerendersTheTableAfterProbing(t *testing.T) {
	fset, f := parseMain(t)
	calls := argsOfCall(fset, f, "catalogLines")
	if len(calls) < 2 {
		t.Fatalf("catalogLines chamado %d vez(es); o segundo call e o re-render pos-check", len(calls))
	}
	// The re-render must also read the source, not the stale filtered slice.
	if got := calls[len(calls)-1][0]; got != "entries" {
		t.Errorf("o re-render le %q; precisa de \"entries\"", got)
	}
}

// exprSource renders an expression back to source text via go/format, so the
// assertion compares what the code SAYS rather than how it is spelled.
func exprSource(fset *token.FileSet, e ast.Expr) string {
	var sb strings.Builder
	if err := format.Node(&sb, fset, e); err != nil {
		return ""
	}
	return sb.String()
}
