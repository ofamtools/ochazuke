// Package refusecheck fails an application's tests when its code adds a
// refusal: a place where the program answers "no" to a request it could
// have run, run another way, asked about, or recorded.
//
// It reads non-test Go and finds two things:
//
//   - error sites with a literal template: fmt.Errorf, errors.New, Failed,
//     Refuse, resultError and the typed errors ReadOnlyError,
//     NotApplicableError and MissingParameterError (a MissingParameterError
//     built by Ctx.Need is an enabling answer and not a site);
//   - declarations that make a verb or value refuse: an Unavailable: or
//     Lacks: field, a When guard on a verb with no Fallback, a Confirm
//     without Loses. (An Enum no longer refuses: Parameter.Accept keeps,
//     registers or nears a value outside it, so it is not a site.)
//
// Each site is classified into one of the kinds K1 to K15 of the
// no-refusals plan by classes.json (an ordered list of message patterns;
// declarations carry their kind). Sites are compared with a baseline text
// file keyed by file, function, class and template, never by line. A site
// the baseline does not cover fails; a baseline row the code no longer
// matches is reported as stale and fails too, so the baseline only shrinks.
// A row whose last column is an exception reason (E1 to E5, or rights,
// loses, failure, declaration, cancel) is a legitimate refusal that stays.
//
// The fields Fallback, Loses and Accept may not exist yet: a declaration
// without them is simply a site in the baseline until its field arrives.
//
// An application adopts it with one test:
//
//	func TestNoNewRefusals(t *testing.T) {
//		refusecheck.Check(t, ".", "testdata/refusecheck.txt")
//	}
//
// See README.md for what counts as a refusal and how to keep an exception.
package refusecheck

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed classes.json
var classesJSON []byte

// Class is one kind of refusal. Pattern (case-insensitive) matches the
// message templates of the kind; Use says what to do instead.
type Class struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Title   string `json:"title"`
	Pattern string `json:"pattern"`
	Use     string `json:"use"`
	Stays   string `json:"stays"`
	re      *regexp.Regexp
}

// Classes are the kinds in match order: the first pattern that matches
// wins; K14 (wrap) and K15 (other) have none and are assigned by shape.
func Classes() []Class {
	var cl []Class
	if err := json.Unmarshal(classesJSON, &cl); err != nil {
		panic("refusecheck: classes.json: " + err.Error())
	}
	for i := range cl {
		if cl[i].Pattern != "" {
			cl[i].re = regexp.MustCompile("(?i)" + cl[i].Pattern)
		}
	}
	return cl
}

// Class ids the scanner assigns by the shape of the site.
const (
	ClassAvailability = "K1"
	ClassChoice       = "K2"
	ClassConfirm      = "K11"
	ClassWrap         = "K14"
	ClassOther        = "K15"
)

// Site is one refusal site found in the code.
type Site struct {
	File, Func, Class, Template string
	Line                        int
}

// Key is the baseline key; it holds no line number.
func (s Site) Key() Key { return Key{s.File, s.Func, s.Class, s.Template} }

// Key identifies a baseline row.
type Key struct{ File, Func, Class, Template string }

func (k Key) String() string {
	return fmt.Sprintf("%s\t%s\t%s\t%s", k.File, k.Func, k.Class, strconv.Quote(k.Template))
}

// Config is what Scan needs.
type Config struct {
	// Dirs are the package directories to scan, relative to Root;
	// "dir/..." walks dir and below (hidden directories,
	// testdata, vendor and nested modules are skipped). Default ".".
	Dirs []string
	// Root is the directory Dirs and the reported file names are relative
	// to; default ".".
	Root string
}

// errCalls build an error from a template; surfaceCalls are the
// framework's error surfaces. A surface call whose argument is itself an
// error construction is not counted twice.
var (
	errCalls     = map[string]bool{"fmt.Errorf": true, "errors.New": true}
	surfaceCalls = map[string]bool{"Failed": true, "Refuse": true, "resultError": true, "refused": true, "refuse": true, "contextError": true}
	typedErrors  = map[string]bool{"ReadOnlyError": true, "NotApplicableError": true, "MissingParameterError": true}
)

type scanner struct {
	fset    *token.FileSet
	classes []Class
	file    string
	fn      string
	sites   []Site
	covered map[*ast.CallExpr]bool // When calls followed by Fallback in their chain
}

// Scan parses the directories and returns every refusal site, sorted.
func Scan(cfg Config) ([]Site, error) {
	dirs := cfg.Dirs
	if len(dirs) == 0 {
		dirs = []string{"."}
	}
	root := cfg.Root
	if root == "" {
		root = "."
	}
	joined := make([]string, len(dirs))
	for i, d := range dirs {
		joined[i] = filepath.ToSlash(filepath.Join(root, d))
		if strings.HasSuffix(d, "/...") {
			joined[i] = filepath.ToSlash(filepath.Join(root, strings.TrimSuffix(d, "/..."))) + "/..."
		}
	}
	files, err := listFiles(joined)
	if err != nil {
		return nil, err
	}
	s := &scanner{fset: token.NewFileSet(), classes: Classes()}
	for _, name := range files {
		f, err := parser.ParseFile(s.fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		if generated(f) {
			continue
		}
		s.file = filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(name), filepath.ToSlash(filepath.Clean(root))+"/"))
		if root == "." {
			s.file = name
		}
		s.covered = map[*ast.CallExpr]bool{}
		for _, d := range f.Decls {
			s.markFallbacks(d)
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Body != nil {
					s.fn = funcKey(d)
					s.walk(d)
				}
			case *ast.GenDecl:
				if d.Tok == token.VAR || d.Tok == token.CONST {
					s.fn = genName(d)
					s.walk(d)
				}
			}
		}
	}
	sort.SliceStable(s.sites, func(i, j int) bool {
		a, b := s.sites[i], s.sites[j]
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Line < b.Line
	})
	return s.sites, nil
}

// markFallbacks records the When calls that a Fallback call follows in a
// builder chain: `.When(g).Fallback(v)` is a guard with its answer.
func (s *scanner) markFallbacks(d ast.Decl) {
	ast.Inspect(d, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || selName(c.Fun) != "Fallback" {
			return true
		}
		var cur ast.Expr = c.Fun
		for {
			sel, ok := cur.(*ast.SelectorExpr)
			if !ok {
				break
			}
			cur = sel.X
			if rc, ok := cur.(*ast.CallExpr); ok {
				if selName(rc.Fun) == "When" {
					s.covered[rc] = true
				}
				cur = rc.Fun
			}
		}
		return true
	})
	return
}

func (s *scanner) walk(d ast.Decl) {
	ast.Inspect(d, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.CallExpr:
			s.call(v)
		case *ast.CompositeLit:
			s.composite(v)
		}
		return true
	})
}

func (s *scanner) add(pos token.Pos, class, tmpl string) {
	s.sites = append(s.sites, Site{File: s.file, Func: s.fn, Class: class, Template: tmpl, Line: s.fset.Position(pos).Line})
}

func (s *scanner) call(c *ast.CallExpr) {
	name := calleeName(c.Fun)
	switch {
	case errCalls[name]:
		tmpl, ok := template(c.Args)
		if !ok {
			tmpl = name + "(…)"
		}
		class := s.classify(tmpl)
		if strings.Contains(tmpl, "%w") {
			class = ClassWrap
		}
		s.add(c.Pos(), class, tmpl)
	case surfaceCalls[lastName(name)]:
		if hasErrConstruction(c.Args) {
			return
		}
		tmpl, ok := template(c.Args)
		if !ok {
			tmpl = lastName(name) + "(…)"
		}
		s.add(c.Pos(), s.classify(tmpl), tmpl)
	case selName(c.Fun) == "When" && !s.covered[c]:
		s.add(c.Pos(), ClassAvailability, "When("+firstLit(c.Args)+") without Fallback")
	}
}

func (s *scanner) composite(cl *ast.CompositeLit) {
	if id := typeName(cl.Type); typedErrors[id] {
		tmpl, ok := literalFields(cl)
		if !ok {
			tmpl = id + "{…}"
		}
		if id == "MissingParameterError" && !ok {
			return // built from a value Ctx.Need or the form answers with
		}
		s.add(cl.Pos(), s.classify(tmpl), tmpl)
		return
	}
	keys := map[string]ast.Expr{}
	for _, e := range cl.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				keys[id.Name] = kv.Value
			}
		}
	}
	has := func(k string) bool { _, ok := keys[k]; return ok }
	nonEmpty := func(k string) (string, bool) {
		v, ok := keys[k]
		if !ok {
			return "", false
		}
		if t, isLit := lit(v); isLit {
			return t, t != ""
		}
		return "<expr>", true
	}
	if t, ok := nonEmpty("Unavailable"); ok {
		s.add(cl.Pos(), ClassAvailability, "Unavailable: "+t)
	}
	if t, ok := nonEmpty("Lacks"); ok {
		s.add(cl.Pos(), ClassAvailability, "Lacks: "+t)
	}
	if v, ok := keys["When"]; ok && !has("Fallback") {
		t, _ := lit(v)
		s.add(cl.Pos(), ClassAvailability, "When: "+orExpr(t)+" without Fallback")
	}
	if t, ok := nonEmpty("Confirm"); ok && !has("Loses") {
		s.add(cl.Pos(), ClassConfirm, "Confirm: "+t+" without Loses")
	}
}

func orExpr(s string) string {
	if s == "" {
		return "<expr>"
	}
	return s
}

func (s *scanner) classify(tmpl string) string {
	for _, c := range s.classes {
		if c.re != nil && c.re.MatchString(tmpl) {
			return c.ID
		}
	}
	return ClassOther
}

// template is the first string literal among args (concatenations with
// non-literals keep %v for the unknown part).
func template(args []ast.Expr) (string, bool) {
	for _, a := range args {
		if t, ok := lit(a); ok {
			return t, true
		}
	}
	return "", false
}

func firstLit(args []ast.Expr) string {
	if t, ok := template(args); ok {
		return strconv.Quote(t)
	}
	return "…"
}

func literalFields(cl *ast.CompositeLit) (string, bool) {
	for _, e := range cl.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			if t, ok := lit(kv.Value); ok && t != "" {
				return t, true
			}
		}
	}
	return "", false
}

func hasErrConstruction(args []ast.Expr) bool {
	found := false
	for _, a := range args {
		ast.Inspect(a, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok && errCalls[calleeName(c.Fun)] {
				found = true
			}
			return !found
		})
	}
	return found
}

func lit(e ast.Expr) (string, bool) {
	switch v := e.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			if s, err := strconv.Unquote(v.Value); err == nil {
				return s, true
			}
		}
	case *ast.ParenExpr:
		return lit(v.X)
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			a, ok1 := lit(v.X)
			b, ok2 := lit(v.Y)
			if ok1 || ok2 {
				if !ok1 {
					a = "%v"
				}
				if !ok2 {
					b = "%v"
				}
				return a + b, true
			}
		}
	}
	return "", false
}

func calleeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		if x, ok := v.X.(*ast.Ident); ok {
			return x.Name + "." + v.Sel.Name
		}
		return "?." + v.Sel.Name
	}
	return ""
}

func lastName(n string) string {
	if i := strings.LastIndex(n, "."); i >= 0 {
		return n[i+1:]
	}
	return n
}

func selName(e ast.Expr) string {
	if s, ok := e.(*ast.SelectorExpr); ok {
		return s.Sel.Name
	}
	return ""
}

func typeName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.Ident:
		return v.Name
	case *ast.SelectorExpr:
		return v.Sel.Name
	}
	return ""
}

func generated(f *ast.File) bool {
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "// Code generated") && strings.HasSuffix(c.Text, "DO NOT EDIT.") {
				return true
			}
		}
	}
	return false
}

func funcKey(d *ast.FuncDecl) string {
	if d.Recv == nil || len(d.Recv.List) == 0 {
		return d.Name.Name
	}
	t := d.Recv.List[0].Type
	if s, ok := t.(*ast.StarExpr); ok {
		t = s.X
	}
	switch ix := t.(type) {
	case *ast.IndexExpr:
		t = ix.X
	case *ast.IndexListExpr:
		t = ix.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name + "." + d.Name.Name
	}
	return d.Name.Name
}

func genName(g *ast.GenDecl) string {
	for _, sp := range g.Specs {
		if vs, ok := sp.(*ast.ValueSpec); ok && len(vs.Names) > 0 {
			return g.Tok.String() + " " + vs.Names[0].Name
		}
	}
	return g.Tok.String() + " _"
}

// listFiles is the non-test Go files of dirs, as slash paths.
func listFiles(dirs []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	add := func(dir string) error {
		ents, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() || !strings.HasSuffix(n, ".go") || strings.HasSuffix(n, "_test.go") {
				continue
			}
			p := path.Clean(filepath.ToSlash(filepath.Join(dir, n)))
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
		return nil
	}
	for _, d := range dirs {
		root, recursive := strings.CutSuffix(d, "/...")
		if !recursive {
			if err := add(d); err != nil {
				return nil, err
			}
			continue
		}
		err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || !e.IsDir() {
				return err
			}
			if p != root {
				n := e.Name()
				if strings.HasPrefix(n, ".") || n == "testdata" || n == "vendor" {
					return filepath.SkipDir
				}
				if _, err := os.Stat(filepath.Join(p, "go.mod")); err == nil {
					return filepath.SkipDir
				}
			}
			return add(p)
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(out)
	return out, nil
}
