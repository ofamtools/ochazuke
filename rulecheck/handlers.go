package rulecheck

import (
	"testing"

	"ofam.dev/ochazuke"
)

const handlersHeader = `rulecheck handlers baseline: kind (missing, unused, params), name (the handler, hook or source), row (the declaration row; "" for unused), count, reason (tab separated).`

// HandlersWidth is the number of key fields of a handlers row.
const HandlersWidth = 3

// Handlers are the name table's findings on r (handlers.go in ochazuke):
// a declaration row naming a handler, hook or source the table lacks
// (missing), an entry no row cites (unused) and a verb row whose
// parameters differ from its handler's typed ones (params). The first and
// last come from the problems the noun loader reported
// (Runtime.DeclProblems), so the check reads what the loader saw.
func Handlers(r *ochazuke.Runtime) []Finding {
	var out []Finding
	for _, p := range r.DeclProblems() {
		switch p.Kind {
		case ochazuke.ProblemMissingName:
			out = append(out, Finding{Fields: []string{"missing", p.Name, p.Row}, Message: p.String()})
		case ochazuke.ProblemParams:
			out = append(out, Finding{Fields: []string{"params", p.Name, p.Row}, Message: p.String()})
		}
	}
	for _, n := range r.NamedAll() {
		if len(n.Rows) == 0 {
			out = append(out, Finding{Fields: []string{"unused", n.Name, ""}, Message: n.Kind + " " + n.Name + " (" + n.At() + ") is cited by no declaration row"})
		}
	}
	return out
}

func runHandlers(t *testing.T, c *Context) {
	var found []Finding
	for _, r := range c.AllRuntimes() {
		found = append(found, Handlers(r)...)
	}
	CheckFindings(t, "handlers", c.BaselinePath("handlers"), HandlersWidth, handlersHeader, dedupe(found))
}
