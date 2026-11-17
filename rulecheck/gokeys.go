package rulecheck

import (
	"go/ast"
	"strconv"
	"testing"
)

const gokeysHeader = `rulecheck gokeys baseline: file, function, key (the Bind call's key: a constant in quotes, a Binding literal's Keys, else the argument's expression), count, reason (tab separated).
A row says: FUNCTION in FILE binds a key row in Go with a Bind call (spine S1 keys: every key is a row of a bindings or noun
file, so the owner's bindings.json can override it and help, the palette and the CLI read it). Fix it by moving the row into
the noun's keys (a noun file) or a bindings file. A row with a reason is a reviewed exception (the loader binding the rows it
read); a row without one is debt that only shrinks (RULECHECK_UPDATE=1 removes and lowers rows, never adds one).
Seeded 2026-11-17 by run plan 5.4a (owed by 5.1): the rows without a reason are the Bind calls present that day; run plan
steps 6b to 6m (each noun's batch, its key rows moving to its noun file) remove them, 5.9 seeds what is left.`

// GoKeysWidth is the number of key fields of a gokeys row.
const GoKeysWidth = 3

// GoKeys are the Bind calls in the source (any x.Bind(...): a builder's
// Bind(keys, request), the runtime's Bind(row)).
func GoKeys(src *Source) []Finding {
	var out []Finding
	src.Decls(func(f *SourceFile, key string, n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok || methodName(c) != "Bind" || len(c.Args) == 0 {
				return true
			}
			k := bindKey(f, c.Args[0])
			out = append(out, Finding{Fields: []string{f.Path, key, k},
				Message: f.Path + ":" + strconv.Itoa(src.Line(c.Pos())) + " " + key + ": Bind(" + k + ") binds a key row in Go: move it into a noun or bindings file (S1 keys)"})
			return true
		})
	})
	return out
}

// bindKey is a Bind argument's key: a constant, a Binding literal's Keys
// field, else the expression's shape.
func bindKey(f *SourceFile, e ast.Expr) string {
	if cl, ok := ast.Unparen(e).(*ast.CompositeLit); ok {
		for _, el := range cl.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if id, ok := kv.Key.(*ast.Ident); ok && id.Name == "Keys" {
					return exprText(f, kv.Value)
				}
			}
		}
		return "<literal>"
	}
	return exprText(f, e)
}

func runGoKeys(t *testing.T, c *Context) {
	CheckFindings(t, "gokeys", c.BaselinePath("gokeys"), GoKeysWidth, gokeysHeader, GoKeys(c.Source(t)))
}
