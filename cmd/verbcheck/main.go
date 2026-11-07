// Command verbcheck reads one or more repositories for references to verbs
// the ochazuke catalog lacks or its removed list names (removed_verbs.json
// and any -removed file): tracked Go, JSON and Markdown, dated CHANGELOG
// sections excepted. It changes nothing.
//
//	go run ./cmd/verbcheck [-removed FILE.json] [-catalog FILE.json] [-unknown] [-allow REF,...] DIR...
//
// It prints one block per repository: the violations (a verb already gone
// or never there), then the pending references (a verb that works until a
// release removes it) with the replacement. It exits 1 when any repository
// has a violation. A repository's own nouns are not in the framework
// catalog, so a verb of a consumer's noun is judged by the removed list
// alone (the standard verbs the framework still generates are pending)
// until -catalog gives the consumer's catalog (or -unknown asks anyway).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"maps"
	"os"
	"strings"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/grid"
	"ofam.dev/ochazuke/markdown"
	"ofam.dev/ochazuke/namecheck"
)

func main() {
	removedFile := flag.String("removed", "", "a JSON file of extra removed-verb rows")
	allow := flag.String("allow", "", "comma-separated references to accept")
	asJSON := flag.Bool("json", false, "print the hits as JSON")
	unknown := flag.Bool("unknown", false, "also report verbs of the framework's nouns that the catalog lacks (the consumer's own nouns need -catalog)")
	var catalogs multi
	flag.Var(&catalogs, "catalog", "a JSON catalog (namecheck.Catalog) of a consumer's nouns and verbs; repeatable")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: verbcheck [-removed FILE] [-allow REF,...] DIR...")
		os.Exit(2)
	}
	r := ochazuke.NewRuntime()
	ochazuke.NewModel(r)
	markdown.Register(r)
	grid.Register(r)
	removed := r.RemovedVerbs()
	if *removedFile != "" {
		data, err := os.ReadFile(*removedFile)
		if err != nil {
			fail(err)
		}
		var extra []namecheck.Removed
		if err := json.Unmarshal(data, &extra); err != nil {
			fail(fmt.Errorf("%s: %w", *removedFile, err))
		}
		removed = append(removed, extra...)
	}
	cat := r.VerbCatalog()
	for _, file := range catalogs {
		data, err := os.ReadFile(file)
		if err != nil {
			fail(err)
		}
		var extra namecheck.Catalog
		if err := json.Unmarshal(data, &extra); err != nil {
			fail(fmt.Errorf("%s: %w", file, err))
		}
		maps.Copy(cat.Nouns, extra.Nouns)
		maps.Copy(cat.Known, extra.Known)
	}
	cfg := namecheck.VerbsConfig{Catalog: cat, Removed: removed}
	if !*unknown && len(catalogs) == 0 {
		cfg.UnknownIn = []string{}
	}
	if *allow != "" {
		cfg.Allow = strings.Split(*allow, ",")
	}
	failed := false
	all := map[string][]namecheck.VerbHit{}
	for _, dir := range flag.Args() {
		cfg.Root = dir
		rep, err := namecheck.Verbs(cfg)
		if err != nil {
			fail(fmt.Errorf("%s: %w", dir, err))
		}
		all[dir] = rep.Hits
		failed = failed || len(rep.Violations()) > 0
		if !*asJSON {
			show(dir, rep)
		}
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(all)
	}
	if failed {
		os.Exit(1)
	}
}

func show(dir string, rep namecheck.VerbReport) {
	v, p, d := rep.Violations(), rep.Pending(), rep.Deprecated()
	fmt.Printf("== %s: %d violations, %d pending, %d deprecated parameters\n", dir, len(v), len(p), len(d))
	for _, list := range [][]namecheck.VerbHit{v, p, d} {
		for _, h := range list {
			fmt.Println("  " + h.String())
		}
	}
}

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

func fail(err error) {
	fmt.Fprintln(os.Stderr, "verbcheck:", err)
	os.Exit(2)
}
