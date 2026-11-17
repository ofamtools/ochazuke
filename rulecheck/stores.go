package rulecheck

import (
	"go/ast"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The store checks (spine S6 rules 1 to 3): storeliterals, storeui and
// hookrefusal. They find a store's code by syntax, through ochazuke's own
// names only: an ochazuke.Store literal's function fields are store
// functions and its ConfirmFor a hook; an ochazuke.DocHooks literal's
// fields are hooks; AddSource's function is a store function and
// AddHook's a hook. A field's value is followed to its body: a function
// literal, or a function or method of the same package named by an
// identifier or a method value (a.patchPane, by its name).

const storeLiteralsHeader = `rulecheck storeliterals baseline: file, function, count, reason (tab separated).
A row says: FUNCTION in FILE writes an ochazuke.Store literal (spine S6 rule 1: a noun's store is its declaration's store row,
one of the four kinds ochazuke fills, plus named hooks; no application writes a Store by hand). Fix it by declaring the
store in the noun file (doc, facts, proxy or instances) with the hooks it needs registered by name (AddHook, AddSource).
A row with a reason is a reviewed exception (ochazuke building a store kind); a row without one is debt that only shrinks
(RULECHECK_UPDATE=1 removes and lowers rows, never adds one).
Seeded 2026-11-17 by run plan 5.4a: the rows without a reason are the Store literals present that day; the overlay plan's
ST2 to ST4 in run plan 5.4 (pane, session) and 6b, 6f (the other nouns' stores) remove them.`

const storeUIHeader = `rulecheck storeui baseline: file, function, role (Store.FIELD, DocHooks.FIELD, AddSource or AddHook), UI field read (FIELD, or "via FUNC: FIELD" one call down), count, reason (tab separated).
A row says: a store function or hook reads a UI field: a field of the package whose type is a page's UI state or the
model (its type name ends in UI, View, Page or Model: *paneUI, *ochazuke.Page, *ochazuke.Model), directly or in a
function of the package it calls (spine S6 rule 2: a store never touches UI state; it emits a change, pages listen, and
scope arrives in the ListQuery). Fields are matched by name, without types. Fix it by moving the UI work to a listener of
the store's change event. A row with a reason is a reviewed exception; a row without one is debt that only shrinks
(RULECHECK_UPDATE=1).
Seeded 2026-11-17 by run plan 5.4a: the rows without a reason are present that day; the store moves of run plan 5.4
and 6b, 6f remove them.`

const hookRefusalHeader = `rulecheck hookrefusal baseline: file, function, role (DocHooks.FIELD, Store.ConfirmFor or AddHook), answer (the refusal it builds, or "via FUNC: ANSWER" one call down), count, reason (tab separated).
A row says: a store hook answers a refusal (Refuse, a RefusedError, ReadOnlyError, NotApplicableError, DeclinedError), a
Nothing (Nothing, NothingErr, KeptError) or a Need (Ctx.Need, NeedTarget, MissingParameterError) in place of doing its
side effect or failing (spine S6 rule 3: a hook does domain side effects only; an error from a hook is failed). Fix it by
doing the side effect, failing with a plain error, or moving the question to the verb. A row with a reason is a reviewed
exception; a row without one is debt that only shrinks (RULECHECK_UPDATE=1).
Seeded 2026-11-17 by run plan 5.4a, where the changes table's hooks are written.`

// Widths of the store checks' rows.
const (
	StoreLiteralsWidth = 2
	StoreUIWidth       = 4
	HookRefusalWidth   = 4
)

// StoreFunc is one store function or hook the store checks read: its
// role (Store.List, DocHooks.BeforeWrite, AddHook, ...), whether it is a
// hook, and its body.
type StoreFunc struct {
	Role string
	Hook bool
	FuncBody
}

// StoreLiterals are the ochazuke.Store composite literals (Store{...} in
// package ochazuke itself).
func StoreLiterals(src *Source) []Finding {
	var out []Finding
	src.Decls(func(f *SourceFile, key string, n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if ok && cl.Type != nil && isOchazukeType(f, cl.Type, "Store") {
				out = append(out, Finding{Fields: []string{f.Path, key},
					Message: f.Path + ":" + strconv.Itoa(src.Line(cl.Pos())) + " " + key + ": an ochazuke.Store literal: declare the store in the noun file (S6 rule 1)"})
			}
			return true
		})
	})
	return out
}

// StoreFuncs are the store functions and hooks of the source, each body
// once per role.
func StoreFuncs(src *Source) []StoreFunc {
	var out []StoreFunc
	seen := map[string]bool{}
	add := func(role string, hook bool, bodies []FuncBody) {
		for _, b := range bodies {
			k := role + "\x00" + b.File.Path + "\x00" + strconv.Itoa(int(b.Body.Pos()))
			if !seen[k] {
				seen[k] = true
				out = append(out, StoreFunc{Role: role, Hook: hook, FuncBody: b})
			}
		}
	}
	src.Decls(func(f *SourceFile, key string, n ast.Node) {
		ast.Inspect(n, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				if x.Type == nil {
					return true
				}
				typ := ""
				switch {
				case isOchazukeType(f, x.Type, "Store"):
					typ = "Store"
				case isOchazukeType(f, x.Type, "DocHooks"):
					typ = "DocHooks"
				default:
					return true
				}
				for _, el := range x.Elts {
					kv, ok := el.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					id, ok := kv.Key.(*ast.Ident)
					if !ok {
						continue
					}
					switch ast.Unparen(kv.Value).(type) {
					case *ast.BasicLit, *ast.CompositeLit:
						continue
					}
					hook := typ == "DocHooks" || id.Name == "ConfirmFor"
					add(typ+"."+id.Name, hook, f.Funcs(kv.Value, key))
				}
			case *ast.CallExpr:
				switch m := methodName(x); m {
				case "AddHook", "AddSource":
					if len(x.Args) == 2 {
						add(m, m == "AddHook", f.Funcs(x.Args[1], key))
					}
				}
			}
			return true
		})
	})
	return out
}

// StoreUI are the UI fields store functions and hooks read.
func StoreUI(src *Source) []Finding {
	ui := uiFields(src)
	reads := func(f *SourceFile, body *ast.BlockStmt) []string {
		var got []string
		ast.Inspect(body, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok && ui[f.Dir][sel.Sel.Name] && !slices.Contains(got, sel.Sel.Name) {
				got = append(got, sel.Sel.Name)
			}
			return true
		})
		return got
	}
	return storeFindings(src, false, reads, "reads UI field", "move the UI work to a listener of the store's change (S6 rule 2)")
}

// HookRefusals are the refusals, Nothing and Need answers store hooks
// build.
func HookRefusals(src *Source) []Finding {
	answers := func(f *SourceFile, body *ast.BlockStmt) []string {
		var got []string
		ast.Inspect(body, func(n ast.Node) bool {
			name := ""
			switch x := n.(type) {
			case *ast.CallExpr:
				name = lastPart(callName(x))
				if !refusalCalls[name] {
					name = ""
				}
			case *ast.CompositeLit:
				if x.Type != nil {
					t := x.Type
					if st, ok := t.(*ast.StarExpr); ok {
						t = st.X
					}
					switch tt := t.(type) {
					case *ast.Ident:
						name = tt.Name
					case *ast.SelectorExpr:
						name = tt.Sel.Name
					}
					if !refusalTypes[name] {
						name = ""
					}
				}
			}
			if name != "" && !slices.Contains(got, name) {
				got = append(got, name)
			}
			return true
		})
		return got
	}
	return storeFindings(src, true, answers, "answers", "do the side effect or fail with a plain error (S6 rule 3)")
}

// refusalCalls and refusalTypes build a refusal, a Nothing or a Need
// answer (ochazuke's own names, as refusecheck reads them).
var (
	refusalCalls = map[string]bool{"Refuse": true, "refuse": true, "refused": true, "Nothing": true, "NothingErr": true,
		"Need": true, "NeedTarget": true, "needThing": true, "needValue": true}
	refusalTypes = map[string]bool{"RefusedError": true, "ReadOnlyError": true, "NotApplicableError": true, "DeclinedError": true,
		"KeptError": true, "NothingError": true, "MissingParameterError": true}
)

// storeFindings runs find over each store function (hooks only when
// hooks) and over the package functions it calls, one level down ("via
// FUNC: X"); one finding per function, role and hit.
func storeFindings(src *Source, hooks bool, find func(*SourceFile, *ast.BlockStmt) []string, verb, fix string) []Finding {
	var out []Finding
	seen := map[string]bool{}
	direct := map[*ast.BlockStmt][]string{}
	hits := func(b FuncBody) []string {
		got, ok := direct[b.Body]
		if !ok {
			got = find(b.File, b.Body)
			direct[b.Body] = got
		}
		return got
	}
	for _, sf := range StoreFuncs(src) {
		if hooks && !sf.Hook {
			continue
		}
		var got []string
		got = append(got, hits(sf.FuncBody)...)
		called := map[string]bool{}
		ast.Inspect(sf.Body, func(n ast.Node) bool {
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			// A plain call names a function of the package; x.m(...) a
			// method whose receiver is also named x (a.livePanes from a
			// store built in an App method), so a method of another type
			// with the same name is not followed.
			var name, recv string
			switch fn := ast.Unparen(c.Fun).(type) {
			case *ast.Ident:
				name = fn.Name
			case *ast.SelectorExpr:
				x, ok := fn.X.(*ast.Ident)
				if !ok {
					return true
				}
				name, recv = fn.Sel.Name, x.Name
			}
			if name == "" || called[recv+"."+name] {
				return true
			}
			called[recv+"."+name] = true
			for _, b := range sf.File.FuncNamed(name) {
				if b.Body == sf.Body || b.Recv != recv {
					continue
				}
				for _, h := range hits(b) {
					got = append(got, "via "+b.Key+": "+h)
				}
			}
			return true
		})
		for _, h := range got {
			fields := []string{sf.File.Path, sf.Key, sf.Role, h}
			k := strings.Join(fields, "\t")
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, Finding{Fields: fields,
				Message: sf.File.Path + ":" + strconv.Itoa(src.Line(sf.Body.Pos())) + " " + sf.Key + " (" + sf.Role + ") " + verb + " " + h + ": " + fix})
		}
	}
	return out
}

// uiFields are, per package directory, the names of struct fields whose
// type is a page's UI state or the model: a type name (through pointers,
// slices and map values, without its package) ending in UI, View, Page or
// Model.
func uiFields(src *Source) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, f := range src.Files {
		ast.Inspect(f.AST, func(n ast.Node) bool {
			st, ok := n.(*ast.StructType)
			if !ok {
				return true
			}
			for _, fl := range st.Fields.List {
				if !uiType(fl.Type) {
					continue
				}
				for _, nm := range fl.Names {
					if out[f.Dir] == nil {
						out[f.Dir] = map[string]bool{}
					}
					out[f.Dir][nm.Name] = true
				}
			}
			return true
		})
	}
	return out
}

func uiType(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.StarExpr:
			e = x.X
			continue
		case *ast.ArrayType:
			e = x.Elt
			continue
		case *ast.MapType:
			e = x.Value
			continue
		case *ast.SelectorExpr:
			e = x.Sel
			continue
		case *ast.Ident:
			for _, suf := range []string{"UI", "View", "Page", "Model"} {
				if strings.HasSuffix(x.Name, suf) {
					return true
				}
			}
		}
		return false
	}
}

func lastPart(name string) string {
	if i := strings.LastIndexByte(name, '.'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func runStoreLiterals(t *testing.T, c *Context) {
	CheckFindings(t, "storeliterals", c.BaselinePath("storeliterals"), StoreLiteralsWidth, storeLiteralsHeader, StoreLiterals(c.Source(t)))
}

func runStoreUI(t *testing.T, c *Context) {
	CheckFindings(t, "storeui", c.BaselinePath("storeui"), StoreUIWidth, storeUIHeader, StoreUI(c.Source(t)))
}

func runHookRefusal(t *testing.T, c *Context) {
	CheckFindings(t, "hookrefusal", c.BaselinePath("hookrefusal"), HookRefusalWidth, hookRefusalHeader, HookRefusals(c.Source(t)))
}
