// Command renamekeys rewrites operation keys from one spelling to the other
// across the repository's .go, .json, .md and .html files. It was written for
// the verb.noun -> noun.verb move and stays as the tool for any later key
// rename that has to touch every literal and prose mention at once.
//
// The authoritative key set is built from the code, not guessed from text:
// every runtime the repo can instantiate (NewRuntime, NewModel, NewPicker,
// grid.Register, demo.New), the verbs the full-project config declares in
// actions.json, and a static walk of every Go file for Do("v.n") literals,
// Operation{Verb:, Noun:} and Request{Verb:, Noun:} literals, and
// Define("noun")...Verb("verb") chains (so main packages such as
// examples/csv and test-only operations are included).
//
// Each key is matched only as a whole dotted token: the characters on both
// sides must not be identifier characters or '.', so Go selectors such as
// r.Execute, file names and domain names are never touched. The tool refuses
// to run when a key's reversed spelling is already a different key.
//
//	go run ./cmd/renamekeys -dry                    # report what would change
//	go run ./cmd/renamekeys                         # apply: verb.noun -> noun.verb
//	go run ./cmd/renamekeys -check -skip bindings_test.go
//	    # after the move: prove no verb.noun token remains (replacements: 0);
//	    # the skipped test keeps the old spelling on purpose
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"ochazuke"
	"ochazuke/examples/demo"
	"ochazuke/grid"
)

type pair struct{ Verb, Noun string }

func main() {
	dry := flag.Bool("dry", false, "report matches without writing files")
	verify := flag.Bool("check", false, "after the move: read Do literals as noun.verb and report any verb.noun token left (implies -dry)")
	skipFiles := flag.String("skip", "", "comma-separated files to leave out: ones that keep the old spelling on purpose, such as the deprecation test")
	flag.Parse()
	if *verify {
		*dry = true
	}
	// The spelling Do("...") literals in the tree use now; harvest reads them
	// in that order so the pair set stays consistent with the registry.
	literalOrder := "verb.noun"
	if *verify {
		literalOrder = "noun.verb"
	}

	keys := map[pair]string{}
	add := func(verb, noun, origin string) {
		if verb == "" || noun == "" {
			return
		}
		if _, ok := keys[pair{verb, noun}]; !ok {
			keys[pair{verb, noun}] = origin
		}
	}
	harvest := func(r *ochazuke.Runtime, origin string) {
		for _, op := range r.Operations {
			add(op.Verb, op.Noun, origin)
		}
	}
	r := ochazuke.NewRuntime()
	harvest(r, "NewRuntime")
	ochazuke.NewModel(r)
	harvest(r, "NewModel")
	ochazuke.NewPicker(r)
	harvest(r, "NewPicker")
	grid.Register(r)
	harvest(r, "grid.Register")
	harvest(demo.New().Runtime, "demo.New")

	var actions []struct{ Verb, Noun string }
	data, err := os.ReadFile("examples/full-project/config/actions.json")
	check(err)
	check(json.Unmarshal(data, &actions))
	for _, a := range actions {
		add(a.Verb, a.Noun, "actions.json")
	}

	files := collect(strings.Split(*skipFiles, ","))
	literals := map[string][]string{}
	for _, path := range files {
		if filepath.Ext(path) != ".go" {
			continue
		}
		for _, p := range harvestGo(path, literals, literalOrder) {
			add(p.Verb, p.Noun, path)
		}
	}

	old := map[string]string{}
	var collisions []string
	for p := range keys {
		old[p.Verb+"."+p.Noun] = p.Noun + "." + p.Verb
	}
	for p := range keys {
		if _, ok := keys[pair{p.Noun, p.Verb}]; ok && p.Noun != p.Verb {
			collisions = append(collisions, p.Verb+"."+p.Noun)
		}
	}
	if len(collisions) > 0 {
		sort.Strings(collisions)
		fmt.Fprintln(os.Stderr, "refusing: reversed spelling is already a different key:", strings.Join(collisions, ", "))
		os.Exit(2)
	}
	for k := range old {
		if !identToken.MatchString(k) {
			fmt.Fprintf(os.Stderr, "refusing: key %q is not a plain dotted identifier\n", k)
			os.Exit(2)
		}
	}
	fmt.Printf("keys: %d\n", len(old))

	total, touched := 0, 0
	var bare []string
	for _, path := range files {
		src, err := os.ReadFile(path)
		check(err)
		out, n, b := rewrite(string(src), old, path)
		if n == 0 {
			continue
		}
		total += n
		touched++
		bare = append(bare, b...)
		fmt.Printf("%4d %s\n", n, path)
		if !*dry {
			check(os.WriteFile(path, []byte(out), 0644))
		}
	}
	fmt.Printf("replacements: %d in %d files\n", total, touched)
	if len(bare) > 0 {
		fmt.Printf("unquoted matches (review): %d\n", len(bare))
		for _, b := range bare {
			fmt.Println("  " + b)
		}
	}
	var unmatched []string
	for lit, where := range literals {
		if _, ok := old[lit]; !ok {
			unmatched = append(unmatched, fmt.Sprintf("%s (%s)", lit, strings.Join(where, ", ")))
		}
	}
	sort.Strings(unmatched)
	if len(unmatched) > 0 && !*verify {
		fmt.Printf("dotted Go string literals not in the key set (review): %d\n", len(unmatched))
		for _, u := range unmatched {
			fmt.Println("  " + u)
		}
	}
}

var identToken = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
var dotted = regexp.MustCompile(`[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`)

func isIdent(c byte) bool {
	return c == '_' || c == '.' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// rewrite replaces every whole-token old key in src and reports the count
// and the matches that were not inside a string quote.
func rewrite(src string, old map[string]string, path string) (string, int, []string) {
	var out strings.Builder
	var bare []string
	n, last := 0, 0
	for _, m := range dotted.FindAllStringIndex(src, -1) {
		tok := src[m[0]:m[1]]
		repl, ok := old[tok]
		if !ok {
			continue
		}
		if m[0] > 0 && isIdent(src[m[0]-1]) || m[1] < len(src) && isIdent(src[m[1]]) {
			continue
		}
		out.WriteString(src[last:m[0]])
		out.WriteString(repl)
		last = m[1]
		n++
		before := byte(' ')
		if m[0] > 0 {
			before = src[m[0]-1]
		}
		if before != '"' && before != '`' && before != '\'' {
			line := 1 + strings.Count(src[:m[0]], "\n")
			bare = append(bare, fmt.Sprintf("%s:%d: %s", path, line, tok))
		}
	}
	out.WriteString(src[last:])
	return out.String(), n, bare
}

// collect lists the files the rename may touch.
func collect(skipFiles []string) []string {
	var files []string
	skip := map[string]bool{".git": true, ".worktrees": true, "transcripts": true, "renamekeys": true}
	skipped := map[string]bool{}
	for _, f := range skipFiles {
		skipped[filepath.Clean(f)] = true
	}
	check(filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] && path != "." {
				return filepath.SkipDir
			}
			return nil
		}
		if skipped[path] {
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".json", ".md", ".html":
			files = append(files, path)
		}
		return nil
	}))
	sort.Strings(files)
	return files
}

// harvestGo walks one Go file for key-forming expressions and records every
// dotted string literal it sees in literals for the unmatched report.
func harvestGo(path string, literals map[string][]string, literalOrder string) []pair {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	check(err)
	var out []pair
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				s, err := strconv.Unquote(x.Value)
				if err == nil && identToken.MatchString(s) {
					literals[s] = append(literals[s], fmt.Sprintf("%s:%d", path, fset.Position(x.Pos()).Line))
				}
			}
		case *ast.CallExpr:
			name := calleeName(x.Fun)
			if name == "Do" && len(x.Args) > 0 {
				if s, ok := lit(x.Args[0]); ok {
					a, b, found := strings.Cut(s, ".")
					if found && literalOrder == "verb.noun" {
						out = append(out, pair{a, b})
					} else if found {
						out = append(out, pair{b, a})
					}
				}
			}
			if name == "Verb" && len(x.Args) > 0 {
				if verb, ok := lit(x.Args[0]); ok {
					if noun, ok := definedNoun(x.Fun); ok {
						out = append(out, pair{verb, noun})
					}
				}
			}
		case *ast.CompositeLit:
			tn := calleeName(x.Type)
			if tn != "Operation" && tn != "Request" {
				return true
			}
			var p pair
			for _, el := range x.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				k, _ := kv.Key.(*ast.Ident)
				v, ok := lit(kv.Value)
				if !ok || k == nil {
					continue
				}
				switch k.Name {
				case "Verb":
					p.Verb = v
				case "Noun":
					p.Noun = v
				}
			}
			if p.Verb != "" && p.Noun != "" {
				out = append(out, p)
			}
		}
		return true
	})
	return out
}

func calleeName(e ast.Expr) string {
	switch f := e.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func lit(e ast.Expr) (string, bool) {
	b, ok := e.(*ast.BasicLit)
	if !ok || b.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(b.Value)
	return s, err == nil
}

// definedNoun walks a builder chain back to its Define("noun") call.
func definedNoun(e ast.Expr) (string, bool) {
	for {
		sel, ok := e.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		call, ok := sel.X.(*ast.CallExpr)
		if !ok {
			return "", false
		}
		if calleeName(call.Fun) == "Define" && len(call.Args) == 1 {
			return lit(call.Args[0])
		}
		e = call.Fun
	}
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
