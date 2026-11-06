package ochazuke

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"

	"ofam.dev/ochazuke/namecheck"
)

// removedVerbsJSON is the framework's own list of verbs that go (or have
// gone) from the catalog, with what to write instead. It is data: the
// verb check (namecheck.Verbs) and bindings check read it, and a release
// that purges a verb adds its row here first, so every reference to the
// verb is found before the verb goes.
//
//go:embed removed_verbs.json
var removedVerbsJSON []byte

var (
	frameworkRemovedOnce sync.Once
	frameworkRemoved     []namecheck.Removed
)

func frameworkRemovedVerbs() []namecheck.Removed {
	frameworkRemovedOnce.Do(func() {
		if err := json.Unmarshal(removedVerbsJSON, &frameworkRemoved); err != nil {
			panic("removed_verbs.json: " + err.Error())
		}
	})
	return frameworkRemoved
}

// RemovedVerbs are the verbs that go from the catalog: the framework's
// list (removed_verbs.json) and the rows the application added.
func (r *Runtime) RemovedVerbs() []namecheck.Removed {
	return append(slices.Clone(frameworkRemovedVerbs()), r.actionNames.removed...)
}

// AddRemovedVerbs adds rows to the removed list: an application's own
// verbs that a release purges. A row needs a verb and a replacement.
func (r *Runtime) AddRemovedVerbs(rows ...namecheck.Removed) error {
	for _, row := range rows {
		if row.Verb == "" || row.Replacement == "" {
			return fmt.Errorf("removed verb %q needs a verb and a replacement", row.Noun+"."+row.Verb)
		}
	}
	r.actionNames.removed = append(r.actionNames.removed, rows...)
	return nil
}

var (
	generatedOnce sync.Once
	generatedSet  map[string]bool
)

// generatedVerbs are the verbs the framework generates for a thing noun,
// read off a probe noun with every role and a full store, so a verb that
// leaves the standard table leaves this set without an edit here.
func generatedVerbs() map[string]bool {
	generatedOnce.Do(func() {
		generatedSet = map[string]bool{}
		r := NewRuntime()
		noop := func(Ctx, string) (Thing, error) { return nil, nil }
		r.Define("probe-owner").Props(PropSpec{Name: "id", Key: true}, PropSpec{Name: "name", Role: RoleLabel})
		r.Define("probe").Props(
			PropSpec{Name: "id", Key: true},
			PropSpec{Name: "title", Role: RoleLabel},
			PropSpec{Name: "owner", Role: RoleOwner, Parameter: Parameter{Noun: "probe-owner"}},
			PropSpec{Name: "archived", Role: RoleArchive, Parameter: Parameter{Type: SettingBoolean}},
		).Store(Store{
			Get:    noop,
			List:   func(Ctx, ListQuery) ([]Thing, error) { return nil, nil },
			Create: func(Ctx, Thing) (Thing, error) { return nil, nil },
			Patch:  func(Ctx, string, Thing) (Thing, error) { return nil, nil },
			Delete: func(Ctx, string) error { return nil },
		})
		for _, op := range r.Operations {
			if op.Noun == "probe" && op.Standard != "" {
				generatedSet[op.Standard] = true
			}
		}
		for _, a := range r.actionNames.aliases {
			if noun, verb, ok := r.splitNounName(a.Alias); ok && noun == "probe" && a.Source == AliasStandard {
				generatedSet[verb] = true
			}
		}
	})
	return generatedSet
}

// nounSpellings are every name a canonical noun answers to.
func (r *Runtime) nounSpellings(noun string) []string {
	out := []string{noun}
	for short, target := range r.aliases {
		if target == noun {
			out = append(out, short)
		}
	}
	for _, a := range r.NounAliasesOf(noun) {
		out = append(out, a.Alias)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// VerbCatalog is what namecheck.Verbs knows about this runtime: every noun
// in each spelling, and every dotted name a reference may legitimately
// use (an operation, an alias, a prop, a setting, in each noun spelling).
// A host with extension nouns mounts them before asking.
func (r *Runtime) VerbCatalog() namecheck.Catalog {
	c := namecheck.Catalog{Nouns: map[string]string{}, Known: map[string]string{}, Generated: maps.Clone(generatedVerbs())}
	spell := map[string][]string{}
	add := func(noun, verb, std string) {
		for _, s := range spell[noun] {
			ref := s + "." + verb
			if _, ok := c.Known[ref]; !ok || std == "" {
				c.Known[ref] = std
			}
			// A verb holding a dot (dirs.base.add) is also read by its first word.
			if i := strings.Index(verb, "."); i > 0 {
				c.Known[s+"."+verb[:i]] = ""
			}
		}
	}
	for name := range r.Nouns {
		spell[name] = r.nounSpellings(name)
		for _, s := range spell[name] {
			c.Nouns[s] = name
		}
	}
	for key, op := range r.Operations {
		noun, verb := op.Noun, op.Verb
		if noun == "" {
			noun, verb = SplitKey(key)
		}
		if _, ok := spell[noun]; !ok {
			spell[noun] = r.nounSpellings(noun)
			for _, s := range spell[noun] {
				c.Nouns[s] = noun
			}
		}
		add(noun, verb, op.Standard)
	}
	for _, a := range r.actionNames.aliases {
		noun, verb, ok := r.splitNounName(a.Alias)
		if !ok {
			continue
		}
		std := ""
		if a.Source == AliasStandard {
			std = verb
		}
		add(noun, verb, std)
	}
	for name, n := range r.Nouns {
		for _, p := range n.Props {
			add(name, p.Name, "")
			for _, al := range p.Aliases {
				add(name, al, "")
			}
		}
	}
	if s := r.Settings(); s != nil {
		for _, one := range s.Declared() {
			c.Known[one.Name] = ""
		}
	}
	return c
}

// removedRefNote is the line bindings check prints for a reference to a
// removed verb: a warning while the verb still works, an error after.
func removedRefNote(cat *namecheck.Catalog, removed []namecheck.Removed, file, where, noun, verb string) string {
	state, row := cat.Classify(noun, verb, removed)
	if state != namecheck.StateRemoved && state != namecheck.StatePending {
		return ""
	}
	line := fmt.Sprintf("%s: %s: %s.%s is a removed verb", file, where, noun, verb)
	if row.Since != "" {
		line += " (since " + row.Since + ")"
	}
	line += "; use " + row.Replacement
	if state == namecheck.StatePending {
		return "warning: " + line + " (it works until then)"
	}
	return line
}

// removedBindingNotes reports the rows of the user's bindings.json whose
// request names a removed verb, with the replacement. It reads the row as
// the file wrote it, before it is validated, so a row the catalog already
// rejects is still told what to use.
func (r *Runtime) removedBindingNotes(rows []fileRow) []string {
	if len(rows) == 0 {
		return nil
	}
	var out []string
	vc, removed := r.VerbCatalog(), r.RemovedVerbs()
	cat := &vc
	for _, f := range rows {
		q := f.row.Request
		if q.Empty() || q.Verb == "" {
			continue
		}
		if note := removedRefNote(cat, removed, BindingsFile, f.where+" "+f.row.Keys+f.row.Pattern, q.Noun, q.Verb); note != "" {
			out = append(out, note)
		}
	}
	return out
}

// removedActionNotes reports the entries of the user's actions.json that
// alias a removed verb, or whose call names one. A name the user gives an
// entry (its own noun.verb) is theirs to choose and is not a reference.
func (r *Runtime) removedActionNotes(entries []fileAction) []string {
	if len(entries) == 0 {
		return nil
	}
	var out []string
	vc, removed := r.VerbCatalog(), r.RemovedVerbs()
	cat := &vc
	note := func(where, noun, verb string) {
		if n := removedRefNote(cat, removed, ActionsFile, where, noun, verb); n != "" {
			out = append(out, n)
		}
	}
	for _, f := range entries {
		if f.alias != nil {
			where := fmt.Sprintf("alias %d (%s)", f.n, f.alias.Alias)
			if n, v := SplitKey(f.alias.Action); v != "" {
				note(where, n, v)
			}
			continue
		}
		a := f.action
		where := fmt.Sprintf("action %d (%s.%s)", f.n, a.Noun, a.Verb)
		for _, ref := range cat.FindRefs(a.Call, removed) {
			note(where+" call", ref.Noun, ref.Verb)
		}
	}
	return out
}
