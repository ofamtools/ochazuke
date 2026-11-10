// Package scaffoldcheck fails a test when a stored noun is not served by
// the generic scaffold from its declaration alone (noun scaffold plan,
// section 5 and lane sc-rights-and-conformance): an index, the edit form
// over its props, new, set, archive and restore through its archive role,
// delete (an agent never runs it alone: never, or confirm from agents.rights
// so the owner says yes; a plain allow fails), a label, the free verbs of
// its value kinds, the CLI words get and list, the scaffold's standard
// verbs (show, duplicate, yank, history, restore, associate), the generated
// key block's rows for the verbs it has, and the show form's history and
// relation sections. It reads only the registry, so an
// application or tool runs it over its own runtime with every extension
// mounted (their catalogs' nouns are checked as the host serves them).
//
// A baseline file lists today's failures, one "noun<TAB>check" per line.
// Check fails on a failure the baseline lacks and on a baseline line that
// no longer fails, so the list only shrinks. SCAFFOLDCHECK_UPDATE=1 drops
// lines that pass now; SCAFFOLDCHECK_UPDATE=seed writes every failure (the
// first run).
//
//	func TestScaffold(t *testing.T) {
//		scaffoldcheck.Check(t, scaffoldcheck.Config{Runtime: rt, Baseline: "testdata/scaffoldcheck.txt"})
//	}
package scaffoldcheck

import (
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// The checks a noun can fail, each a Problem.Check.
const (
	CheckIndex   = "index"   // a list From its store: List or Query and a key prop
	CheckLabel   = "label"   // a label prop (Role label) to show its things by
	CheckForm    = "form"    // edit opens the form over its props
	CheckNew     = "new"     // create, or a verb that creates (Operation.Creates)
	CheckSet     = "set"     // set writes a prop
	CheckArchive = "archive" // an archive role prop and archive
	CheckRestore = "restore" // its archive role is writable, so set @archive false restores
	CheckDelete  = "delete"  // delete, which an agent may never run
	CheckWords   = "words"   // the CLI words get and list
	CheckValues  = "values"  // the free verbs of its props' value kinds (path: open, open-with, reveal, yank-path; url: open, yank-path; reference: follow)
	CheckVerbs   = "verbs"   // the scaffold's standard verbs: show, duplicate (alias dup), yank, history; restore where it archives; associate where relations are kept
	CheckKeys    = "keys"    // the generated key block (keys.noun-defaults) has a row for each verb it has, unless it declines it (DefaultKeys(false))
	CheckShow    = "show"    // the show form's sections: the history section reads history, and the relation sections relate where relations are kept
)

// Problem is one check a noun fails, with why.
type Problem struct {
	Noun, Check, Why string
}

// Row is the problem as a baseline line.
func (p Problem) Row() string { return p.Noun + "\t" + p.Check }

// Config says what to check.
type Config struct {
	Runtime *ochazuke.Runtime
	// Baseline is the path of the accepted failures.
	Baseline string
	// Keep, when set, chooses the nouns checked (default: every record
	// noun the application or its extensions declare, not the framework's).
	Keep func(n *ochazuke.Noun) bool
}

// Problems are the failures of every checked noun, sorted.
func Problems(c Config) []Problem {
	r := c.Runtime
	keep := c.Keep
	if keep == nil {
		keep = func(n *ochazuke.Noun) bool { return !n.Builtin }
	}
	var out []Problem
	for _, name := range sortedNouns(r) {
		n := r.Nouns[name]
		if r.NounKind(name) != ochazuke.NounKindRecord || !keep(n) {
			continue
		}
		out = append(out, nounProblems(r, n)...)
	}
	return out
}

func sortedNouns(r *ochazuke.Runtime) []string {
	names := make([]string, 0, len(r.Nouns))
	for name := range r.Nouns {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// available reports whether noun.verb is registered and not unavailable.
func available(r *ochazuke.Runtime, noun, verb string) (ochazuke.Operation, string) {
	op, ok := r.Operation(noun + "." + verb)
	switch {
	case !ok:
		return op, "no " + verb + " verb"
	case op.Unavailable != "":
		return op, verb + " is unavailable: " + op.Unavailable
	case op.Disabled:
		return op, verb + " is disabled"
	}
	return op, ""
}

func nounProblems(r *ochazuke.Runtime, n *ochazuke.Noun) []Problem {
	var out []Problem
	fail := func(check, why string) { out = append(out, Problem{Noun: n.Name, Check: check, Why: why}) }
	need := func(check, verb string) {
		if _, why := available(r, n.Name, verb); why != "" {
			fail(check, why)
		}
	}
	var key, label, archive *ochazuke.PropSpec
	for i := range n.Props {
		p := &n.Props[i]
		switch {
		case p.Key:
			key = p
		case p.Role == ochazuke.RoleLabel:
			label = p
		case p.Role == ochazuke.RoleArchive:
			archive = p
		}
	}
	if s := n.Store; s.List == nil && s.Query == nil || key == nil {
		fail(CheckIndex, "an index needs a store that lists (List or Query) and a key prop")
	}
	if label == nil {
		fail(CheckLabel, "no prop has Role label")
	}
	need(CheckForm, "edit")
	if _, why := available(r, n.Name, "create"); why != "" && !createsBySomeVerb(r, n.Name) {
		fail(CheckNew, why+", and no verb of it Creates")
	}
	need(CheckSet, "set")
	if archive == nil {
		fail(CheckArchive, "no prop has Role archive")
		fail(CheckRestore, "no archive prop to set false")
	} else {
		if op, why := available(r, n.Name, "archive"); why != "" {
			fail(CheckArchive, why)
		} else if op.AgentRightOf(false) == ochazuke.AgentNever {
			fail(CheckArchive, "an agent may not archive; agents archive instead of deleting")
		}
		if archive.ReadOnly {
			fail(CheckRestore, "its archive prop is read-only")
		}
	}
	if op, why := available(r, n.Name, "delete"); why != "" {
		fail(CheckDelete, why)
	} else if right := r.AgentRight(op, false); right.Right == ochazuke.AgentAllow {
		// An agent's delete is refused (never) or runs only with the
		// owner's yes (confirm, from agents.rights: a pane or a session
		// closes with the owner's OK); a plain allow is an agent delete.
		fail(CheckDelete, "an agent may run delete ("+right.Right+", "+right.Rule+"); agents only archive, or delete with the owner's confirmation")
	}
	out = append(out, scaffoldProblems(r, n, archive != nil)...)
	for _, verb := range []string{"get", "list"} {
		if _, why := available(r, n.Name, verb); why != "" {
			fail(CheckWords, why)
		}
	}
	if missing := missingValueVerbs(r, n); len(missing) > 0 {
		fail(CheckValues, "missing "+strings.Join(missing, ", "))
	}
	return out
}

// scaffoldProblems are the checks of the scaffold's standard verbs, the
// generated keys and the show form's sections.
func scaffoldProblems(r *ochazuke.Runtime, n *ochazuke.Noun, archives bool) []Problem {
	var out []Problem
	fail := func(check, why string) { out = append(out, Problem{Noun: n.Name, Check: check, Why: why}) }
	verbs := []string{"show", "yank", "history"}
	if _, why := available(r, n.Name, "create"); why == "" {
		verbs = append(verbs, "duplicate") // a noun that makes no things fails new, not this
	}
	if archives {
		verbs = append(verbs, "restore")
	}
	if r.RelationStore() != nil {
		verbs = append(verbs, "associate")
	}
	var missing []string
	for _, verb := range verbs {
		if _, why := available(r, n.Name, verb); why != "" {
			missing = append(missing, verb)
		}
	}
	if len(missing) > 0 {
		fail(CheckVerbs, "missing "+strings.Join(missing, ", "))
	}
	if keys := missingKeys(r, n); len(keys) > 0 {
		fail(CheckKeys, "no generated row for "+strings.Join(keys, ", "))
	}
	if _, why := available(r, n.Name, "history"); why != "" {
		fail(CheckShow, "the history section has nothing to read: "+why)
	} else if r.RelationStore() != nil {
		if _, why := available(r, n.Name, "relate"); why != "" {
			fail(CheckShow, "the relation sections cannot relate: "+why)
		}
	}
	return out
}

// missingKeys are the keys of the generated block the noun should have (its
// verb available, its role present) and lacks, none when it declines the
// block or the setting is off.
func missingKeys(r *ochazuke.Runtime, n *ochazuke.Noun) []string {
	if n.NoDefaultKeys || n.Pin != "" || !r.Settings().Bool(ochazuke.NounDefaultsSetting) {
		return nil
	}
	have := map[string]bool{}
	for _, b := range r.RowsOf(n.Name, "normal") {
		have[b.Keys] = true
	}
	var out []string
	for _, t := range ochazuke.NounKeyRows() {
		op, why := available(r, n.Name, t.Verb)
		if why != "" || op.Standard == "" || t.Role != "" && !hasRole(n, t.Role) {
			continue
		}
		if !have[t.Key] {
			out = append(out, t.Key)
		}
	}
	return out
}

func hasRole(n *ochazuke.Noun, role string) bool {
	return slices.ContainsFunc(n.Props, func(p ochazuke.PropSpec) bool { return p.Role == role && !p.ReadOnly })
}

func createsBySomeVerb(r *ochazuke.Runtime, noun string) bool {
	for _, op := range r.Operations.Entries() {
		if op.Noun() == noun && op.Creates && op.Unavailable == "" {
			return true
		}
	}
	return false
}

// valueVerbs are the free verbs each value kind gives a noun, by prop type
// (a reference is any prop with a Noun).
var valueVerbs = map[string][]string{
	ochazuke.PropPath: {"open", "open-with", "reveal", "yank-path"},
	ochazuke.PropURL:  {"open", "yank-path"},
}

func missingValueVerbs(r *ochazuke.Runtime, n *ochazuke.Noun) []string {
	var want []string
	for _, p := range n.Props {
		want = append(want, valueVerbs[p.Type]...)
		if p.Noun != "" && p.Items == nil {
			want = append(want, "follow")
		}
	}
	slices.Sort(want)
	want = slices.Compact(want)
	var missing []string
	for _, verb := range want {
		if _, why := available(r, n.Name, verb); why != "" {
			missing = append(missing, verb)
		}
	}
	return missing
}

// Check fails t on a failure the baseline lacks or a baseline line that
// passes now (see the package doc for SCAFFOLDCHECK_UPDATE).
func Check(t testing.TB, c Config) {
	t.Helper()
	problems := Problems(c)
	got := map[string]Problem{}
	for _, p := range problems {
		got[p.Row()] = p
	}
	base := map[string]bool{}
	if data, err := os.ReadFile(c.Baseline); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
				base[line] = true
			}
		}
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	switch os.Getenv("SCAFFOLDCHECK_UPDATE") {
	case "seed":
		writeBaseline(t, c.Baseline, keysOf(got))
		return
	case "1":
		var kept []string
		for row := range base {
			if _, ok := got[row]; ok {
				kept = append(kept, row)
			}
		}
		writeBaseline(t, c.Baseline, kept)
		base = map[string]bool{}
		for _, row := range kept {
			base[row] = true
		}
	}
	var news, stale []string
	for row, p := range got {
		if !base[row] {
			news = append(news, fmt.Sprintf("%s: %s", row, p.Why))
		}
	}
	for row := range base {
		if _, ok := got[row]; !ok {
			stale = append(stale, row)
		}
	}
	sort.Strings(news)
	sort.Strings(stale)
	if len(news) > 0 || len(stale) > 0 {
		t.Errorf("scaffoldcheck: %d new, %d fixed (baseline %s; SCAFFOLDCHECK_UPDATE=1 drops fixed lines)\n  new: %s\n  fixed: %s", len(news), len(stale), c.Baseline, strings.Join(news, "\n  new: "), strings.Join(stale, "\n  fixed: "))
	}
}

func keysOf(m map[string]Problem) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func writeBaseline(t testing.TB, path string, rows []string) {
	t.Helper()
	sort.Strings(rows)
	text := "# scaffoldcheck baseline: noun<TAB>check, a failure accepted today; it only shrinks.\n"
	if len(rows) > 0 {
		text += strings.Join(rows, "\n") + "\n"
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
