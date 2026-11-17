package rulecheck

import (
	"go/ast"
	"strconv"
	"testing"
)

const declarationsHeader = `rulecheck declarations baseline: file, function, noun (the Define argument: a constant in quotes, else its expression), count, reason (tab separated).
A row says: FUNCTION in FILE declares a noun in Go with a Define call (spine S1 rule 1: a noun exists only through its declaration
file, and the noun-file loader, cli.LoadNouns, is the one caller of Define). Fix it by moving the noun's rows into a noun file.
A row with a reason is a reviewed exception (the loader itself); a row without one is debt that only shrinks
(RULECHECK_UPDATE=1 removes and lowers rows, never adds one; adding a row is a reviewed edit).
Seeded 2026-11-17 by run plan 5.4a (owed by 5.1): the rows without a reason are the Define calls present that day; run plan
steps 6b to 6m (each noun's batch) remove them, 5.9 seeds what is left.`

// DeclarationsWidth is the number of key fields of a declarations row.
const DeclarationsWidth = 3

// Declarations are the Define calls in the source (any x.Define(arg): no
// types are read, so a Define method of another type counts too and is a
// row with a reason).
func Declarations(src *Source) []Finding {
	var out []Finding
	src.Decls(func(f *SourceFile, key string, n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || methodName(c) != "Define" || len(c.Args) != 1 {
				return true
			}
			noun := exprText(f, c.Args[0])
			out = append(out, Finding{Fields: []string{f.Path, key, noun},
				Message: f.Path + ":" + strconv.Itoa(src.Line(c.Pos())) + " " + key + ": Define(" + noun + ") declares a noun in Go: move its rows into a noun file (S1 rule 1)"})
			return true
		})
	})
	return out
}

func runDeclarations(t *testing.T, c *Context) {
	CheckFindings(t, "declarations", c.BaselinePath("declarations"), DeclarationsWidth, declarationsHeader, Declarations(c.Source(t)))
}
