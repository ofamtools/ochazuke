package rulecheck

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const stringIDsHeader = `rulecheck stringids baseline: file, function, kind (join, split, prefix-join, prefix-split, id-or-name), text (the separator, the literal prefix, or the id and name fields), count, reason (tab separated).
A row says: FUNCTION in FILE glues or cuts a reference in a string outside Ref (spine S5 rule 4: one text form, noun:key, made
and read only by Ref.String and ParseRef; report 1f's rule): a + chain with a literal ":" or "/" between two values, a literal
id prefix ("pane:", a lower-case word ending in : or /) before a value, a fmt.Sprintf format joining two verbs with : or /
or starting with such a prefix, strings.Join with ":" or "/", a Split, Cut, Index or Count on ":" or "/", a HasPrefix,
TrimPrefix or CutPrefix of a literal prefix, and cmp.Or of an id field and its name field (cmp.Or(p.SessionID, p.Session)).
Left out as not ids: a format whose verbs around the separator are both %d (clock times), a chain or format with a
literal holding "//", white space (a message) or a leading "/", a URL scheme, and a + chain or Join whose values are paths (an operand named like a path: dir, path, root, home,
file, url, base, cwd, repo, folder, ...; a filepath or path call). A row with a reason is a reviewed exception (Ref's own
text form, a non-id string such as a key sequence or a time); a row without one is debt that only shrinks
(RULECHECK_UPDATE=1). Seeded 2026-11-17 by run plan 5.4a (owed by 5.2): the rows without a reason are present that day;
run plan 5.6 (the pane list's row ids) and the noun batches 6b to 6m remove them.`

// StringIDsWidth is the number of key fields of a stringids row.
const StringIDsWidth = 4

var (
	idPrefixRe  = regexp.MustCompile(`^[a-z][a-z0-9_.-]*[/:]$`)
	fmtVerb     = `%[-+# 0-9.*]*[a-zA-Z]`
	fmtJoinRe   = regexp.MustCompile(`(` + fmtVerb + `)([/:])(` + fmtVerb + `)`)
	fmtPrefixRe = regexp.MustCompile(`^([a-z][a-z0-9_.-]*[/:])` + fmtVerb)
	// pathWords mark a name that holds a path or URL: its words
	// (split at upper case and _) include one of these.
	pathWords = map[string]bool{"dir": true, "dirs": true, "path": true, "paths": true, "root": true, "home": true,
		"file": true, "files": true, "filename": true, "url": true, "uri": true, "base": true, "cwd": true, "repo": true,
		"folder": true, "host": true, "addr": true, "endpoint": true, "remote": true, "branch": true,
		"module": true, "pkg": true, "src": true, "dst": true, "dest": true, "ext": true, "tmp": true,
		"wd": true, "abs": true, "rel": true, "href": true, "port": true}
	splitCalls = map[string]bool{"strings.Split": true, "strings.SplitN": true, "strings.SplitAfter": true, "strings.SplitAfterN": true,
		"strings.Cut": true, "strings.Index": true, "strings.LastIndex": true, "strings.IndexByte": true,
		"strings.LastIndexByte": true, "strings.Count": true, "strings.IndexRune": true}
	prefixCalls = map[string]bool{"strings.HasPrefix": true, "strings.TrimPrefix": true, "strings.CutPrefix": true}
	// urlSchemes are prefixes that begin a URL or a URI, not an id.
	urlSchemes = map[string]bool{"http:": true, "https:": true, "file:": true, "mailto:": true, "ssh:": true, "git:": true,
		"tcp:": true, "unix:": true, "data:": true, "javascript:": true, "ws:": true, "wss:": true}
)

// StringIDs are the joins and splits of structured strings outside Ref
// (report 1f's rule, narrowed: see the header).
func StringIDs(src *Source) []Finding {
	var out []Finding
	src.Decls(func(f *SourceFile, key string, n ast.Node) {
		add := func(pos token.Pos, kind, text string) {
			out = append(out, Finding{Fields: []string{f.Path, key, kind, text},
				Message: f.Path + ":" + strconv.Itoa(src.Line(pos)) + " " + key + ": " + kind + " " + strconv.Quote(text) + " builds or reads an id in a string: use Ref (S5 rule 4)"})
		}
		var visit func(n ast.Node) bool
		visit = func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.BinaryExpr:
				if x.Op != token.ADD {
					return true
				}
				// A chain is read whole; its operands (never a + node:
				// flattened) are read on their own.
				ops := flattenAdd(x)
				for _, h := range concatHits(f, ops) {
					add(x.Pos(), h[0], h[1])
				}
				for _, op := range ops {
					ast.Inspect(op, visit)
				}
				return false
			case *ast.CallExpr:
				callHits(f, x, add)
			}
			return true
		}
		ast.Inspect(n, visit)
	})
	return out
}

// flattenAdd is a + chain's operands, left to right.
func flattenAdd(e ast.Expr) []ast.Expr {
	if b, ok := ast.Unparen(e).(*ast.BinaryExpr); ok && b.Op == token.ADD {
		return append(flattenAdd(b.X), flattenAdd(b.Y)...)
	}
	return []ast.Expr{e}
}

// concatHits are a + chain's joins: a literal ":" or "/" between two
// values, or an id prefix before a value; none when a value is a path.
func concatHits(f *SourceFile, ops []ast.Expr) [][2]string {
	type part struct {
		konst bool
		val   string
	}
	ps := make([]part, len(ops))
	for i, op := range ops {
		ps[i].val, ps[i].konst = f.Const(op)
	}
	for i, op := range ops {
		if !ps[i].konst && pathy(op) {
			return nil
		}
		if ps[i].konst && (strings.Contains(ps[i].val, "//") || strings.ContainsAny(ps[i].val, " \t\n") ||
			(strings.HasPrefix(ps[i].val, "/") && len(ps[i].val) > 1)) {
			return nil
		}
	}
	var out [][2]string
	for i, p := range ps {
		if !p.konst {
			continue
		}
		prevVar := i > 0 && !ps[i-1].konst
		nextVar := i+1 < len(ps) && !ps[i+1].konst
		if (p.val == ":" || p.val == "/") && prevVar && nextVar {
			out = append(out, [2]string{"join", p.val})
		}
		if nextVar && (i == 0 || !ps[i-1].konst) && idPrefixRe.MatchString(p.val) && !urlSchemes[p.val] {
			out = append(out, [2]string{"prefix-join", p.val})
		}
	}
	return out
}

// callHits are a call's joins and splits: Sprintf, Join, the split and
// prefix functions, and cmp.Or of an id and its name.
func callHits(f *SourceFile, c *ast.CallExpr, add func(token.Pos, string, string)) {
	name := callName(c)
	arg := func(i int) (string, bool) {
		if i < len(c.Args) {
			return f.Const(c.Args[i])
		}
		return "", false
	}
	switch {
	case name == "fmt.Sprintf":
		format, ok := arg(0)
		if !ok || strings.Contains(format, "//") || strings.ContainsAny(format, " \t\n") {
			return
		}
		for _, a := range c.Args[1:] {
			if pathy(a) {
				return
			}
		}
		for _, m := range fmtJoinRe.FindAllStringSubmatch(format, -1) {
			if strings.HasSuffix(m[1], "d") && strings.HasSuffix(m[3], "d") {
				continue
			}
			add(c.Pos(), "join", m[2])
		}
		if m := fmtPrefixRe.FindStringSubmatch(format); m != nil && !urlSchemes[m[1]] {
			add(c.Pos(), "prefix-join", m[1])
		}
	case name == "strings.Join":
		if sep, ok := arg(1); ok && (sep == ":" || sep == "/") && len(c.Args) == 2 && !pathy(c.Args[0]) {
			add(c.Pos(), "join", sep)
		}
	case splitCalls[name]:
		if sep, ok := arg(1); ok && (sep == ":" || sep == "/") && len(c.Args) >= 2 && !pathy(c.Args[0]) {
			add(c.Pos(), "split", sep)
		}
	case prefixCalls[name]:
		if p, ok := arg(1); ok && idPrefixRe.MatchString(p) && !urlSchemes[p] {
			add(c.Pos(), "prefix-split", p)
		}
	case name == "cmp.Or":
		var fields []string
		for _, a := range c.Args {
			if sel, ok := ast.Unparen(a).(*ast.SelectorExpr); ok {
				fields = append(fields, sel.Sel.Name)
			}
		}
		for _, fl := range fields {
			if base, ok := strings.CutSuffix(fl, "ID"); ok && base != "" {
				for _, g := range fields {
					if g == base {
						add(c.Pos(), "id-or-name", fl+" "+base)
					}
				}
			}
		}
	}
}

// pathy says whether a value holds a path or a URL: a call to a path,
// filepath or url function, or an identifier or selector whose name has a
// path word (dir, path, root, ...; split at upper case and _).
func pathy(e ast.Expr) bool {
	switch x := ast.Unparen(e).(type) {
	case *ast.CallExpr:
		if id, ok := ast.Unparen(x.Fun).(*ast.SelectorExpr); ok {
			if pkg, ok := id.X.(*ast.Ident); ok && (pkg.Name == "filepath" || pkg.Name == "path" || pkg.Name == "url" || pkg.Name == "os") {
				return true
			}
			return nameIsPath(id.Sel.Name)
		}
		if id, ok := ast.Unparen(x.Fun).(*ast.Ident); ok {
			return nameIsPath(id.Name)
		}
	case *ast.Ident:
		return nameIsPath(x.Name)
	case *ast.SelectorExpr:
		return nameIsPath(x.Sel.Name)
	case *ast.IndexExpr:
		return pathy(x.X)
	case *ast.SliceExpr:
		return pathy(x.X)
	}
	return false
}

func nameIsPath(name string) bool {
	for _, w := range nameWords(name) {
		if pathWords[w] {
			return true
		}
	}
	return false
}

// nameWords splits an identifier at upper case letters and _, lower-cased
// (workDir: work, dir; URLPath: url, path).
func nameWords(name string) []string {
	var out []string
	start := 0
	rs := []rune(name)
	for i := 1; i <= len(rs); i++ {
		if i == len(rs) || rs[i] == '_' ||
			(isUpper(rs[i]) && (!isUpper(rs[i-1]) || (i+1 < len(rs) && !isUpper(rs[i+1]) && rs[i+1] != '_'))) {
			if w := strings.Trim(string(rs[start:i]), "_"); w != "" {
				out = append(out, strings.ToLower(w))
			}
			start = i
		}
	}
	return out
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

func runStringIDs(t *testing.T, c *Context) {
	CheckFindings(t, "stringids", c.BaselinePath("stringids"), StringIDsWidth, stringIDsHeader, StringIDs(c.Source(t)))
}
