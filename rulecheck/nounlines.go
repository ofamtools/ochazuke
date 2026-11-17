package rulecheck

import (
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// NounLinesRule is the measure of spine S12 (review A1), stated once: the
// Go lines that name a noun. hub tools/pane-parity wrote it first
// (nounlines.go); this is now the one implementation.
const NounLinesRule = "a function or method counts whole (its declaration, doc comment excluded) for a noun when it names the noun: a string literal NOUN or NOUN followed by . : / (a verb key, a row id, a key table), a package-level constant holding one, or an identifier NOUN[s] alone or followed by [A-Z0-9_] (case of the first letter free: pane, paneRow, PaneCopy, panes; panesd does not); a function naming several nouns counts for each; non-test, non-generated .go files of the scanned directories"

const nounLinesHeader = `rulecheck nounlines baseline: noun, count (the Go lines that name it), reason (tab separated).
Rule: ` + NounLinesRule + `.
The nouns are the runtimes' stored nouns (record, singleton, facts, instances: those with a store).
A row says: NOUN is named by COUNT lines of Go (spine S12: code stays only where data cannot say it; moving a store's logic
into a hooks file must not count as removing it). Every count may only fall: RULECHECK_UPDATE=1 lowers it, never raises it;
a step that adds a noun's lines raises its row as a reviewed edit with a reason. Seeded 2026-11-17 by run plan 5.4a; every
step that removes a store or page file reports these numbers before and after.`

// NounLinesWidth is the number of key fields of a nounlines row.
const NounLinesWidth = 1

// NounFileLines is one noun's count in one file: the functions that name
// it and their lines.
type NounFileLines struct {
	Noun, File   string
	Funcs, Lines int
}

// NounLines measures NounLinesRule over src for nouns, per noun and file,
// sorted.
func NounLines(src *Source, nouns []string) []NounFileLines {
	set := map[string]bool{}
	for _, n := range nouns {
		set[n] = true
	}
	litNames := func(s string, into map[string]bool) {
		if set[s] {
			into[s] = true
		}
		for i := 0; i < len(s); i++ {
			if c := s[i]; (c == '.' || c == ':' || c == '/') && set[s[:i]] {
				into[s[:i]] = true
			}
		}
	}
	identName := func(id string, into map[string]bool) {
		if id == "" {
			return
		}
		end := len(id)
		for i := 1; i < len(id); i++ {
			if c := id[i]; c == '_' || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
				end = i
				break
			}
		}
		head := strings.ToLower(id[:1]) + id[1:end]
		if set[head] {
			into[head] = true
		}
		if h, ok := strings.CutSuffix(head, "s"); ok && set[h] {
			into[h] = true
		}
	}
	type key struct{ noun, file string }
	counts := map[key]*NounFileLines{}
	for _, f := range src.Files {
		for _, d := range f.AST.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			named := map[string]bool{}
			ast.Inspect(fd, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.BasicLit:
					if x.Kind == token.STRING {
						if s, err := strconv.Unquote(x.Value); err == nil {
							litNames(s, named)
						}
					}
				case *ast.Ident:
					if s, ok := f.ConstNamed(x.Name); ok {
						litNames(s, named)
					}
					identName(x.Name, named)
				}
				return true
			})
			lines := src.Line(fd.End()) - src.Line(fd.Pos()) + 1
			for n := range named {
				k := key{n, f.Path}
				c := counts[k]
				if c == nil {
					c = &NounFileLines{Noun: n, File: f.Path}
					counts[k] = c
				}
				c.Funcs++
				c.Lines += lines
			}
		}
	}
	out := make([]NounFileLines, 0, len(counts))
	for _, c := range counts {
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b NounFileLines) int {
		if c := strings.Compare(a.Noun, b.Noun); c != 0 {
			return c
		}
		return strings.Compare(a.File, b.File)
	})
	return out
}

// NounLineFindings are a noun's lines as findings, one per line, so its
// baseline row's count is the measure and may only fall.
func NounLineFindings(counts []NounFileLines) []Finding {
	total := map[string]int{}
	for _, c := range counts {
		total[c.Noun] += c.Lines
	}
	var out []Finding
	for _, n := range slices.Sorted(maps.Keys(total)) {
		f := Finding{Fields: []string{n}, Message: "noun " + n + ": " + strconv.Itoa(total[n]) + " Go lines name it (S12: may only fall)"}
		for range total[n] {
			out = append(out, f)
		}
	}
	return out
}

// storedNouns are the runtimes' nouns with a store, sorted.
func (c *Context) storedNouns() []string {
	set := map[string]bool{}
	for _, r := range c.AllRuntimes() {
		for name, n := range r.Nouns {
			if n != nil && n.Store != nil {
				set[name] = true
			}
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func runNounLines(t *testing.T, c *Context) {
	CheckFindings(t, "nounlines", c.BaselinePath("nounlines"), NounLinesWidth, nounLinesHeader, NounLineFindings(NounLines(c.Source(t), c.storedNouns())))
}
