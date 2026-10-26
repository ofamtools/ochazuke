package cli

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/unixsuperhero/ochazuke"
)

// Schema is the catalog format version Catalog writes.
const Schema = 1

// CatalogDoc is the catalog answer: what a runtime registers, grouped by
// noun, in the runtime's own JSON shapes (ochazuke.Parameter and
// ochazuke.Binding unchanged). Nothing in it is invented: every field reads
// a registry the runtime already has.
//
// State is the state source, "NOUN.VERB", "" for none: the first verb (in
// catalog order) declared with NounBuilder.State. Its answer is every
// list's rows in one process, as JSON Lines (Lines of Row): one object per
// line, no outer array, each tagged with the list noun whose row source (the
// verb declaring rows, else list) a host would otherwise run:
//
//	{"noun":"agent","id":"agent/a1","label":"ada"}
//	{"noun":"skill","id":"skill/s1","label":"review [claude]","data":{...}}
//
// data is optional (a host's list needs id and label; show prints the
// record); cells are the row's text per column of its noun's columns
// (CatalogNoun.Columns), optional too. The answer covers every list: a noun with no lines has no rows.
// A host decodes it as it arrives (RowReader), skipping a malformed line
// silently; only a failed exec or a stream with nothing decodable is an
// error. It runs the verb once, keeps the rows as its cached state and
// refreshes them when it chooses; an extension without a state verb keeps
// per-list row sources. A boolean parameter fresh on the state verb, when
// declared, asks the extension to bypass its own caches (an explicit
// refresh).
type CatalogDoc struct {
	Schema   int                `json:"schema"`
	Name     string             `json:"name"`
	Version  string             `json:"version"`
	State    string             `json:"state,omitempty"`
	Nouns    []CatalogNoun      `json:"nouns"`
	Bindings []ochazuke.Binding `json:"bindings"`
	Pages    []CatalogPage      `json:"pages"`
}

// CatalogNoun is a noun and its verbs, by verb name. Pin is the noun's
// pinned base mode (ochazuke.Noun.Pin), "" for none: a host can tell a
// modal-only noun from its data without knowing its name. Columns are the
// table columns a list of the noun's rows draws (ochazuke.Noun.Columns,
// declared with NounBuilder.Columns), in order; each row's cells (Row.Cells)
// follow that order. A host passes them to ochazuke.Columns when it mounts
// the noun's rows; a noun without them draws its labels.
type CatalogNoun struct {
	Name    string            `json:"name"`
	Pin     string            `json:"pin,omitempty"`
	Columns []ochazuke.Column `json:"columns,omitempty"`
	Verbs   []CatalogVerb     `json:"verbs"`
}

// CatalogVerb is one ochazuke.Operation. Params are the operation's
// ochazuke.Parameter values as they are: type, required, description, the
// static enum, the source (an operation, NOUN.VERB, whose result lists more
// choices) and free. Free (omitted when false) says the choices are
// suggestions and any value is accepted (mtm's --model); a parameter with an
// enum or a source and no free accepts only its choices, so a host may
// refuse other values before running the verb.
//
// Kind is "application" for a verb
// whose handler may return pending work, "interaction" otherwise. Rows
// names the noun whose rows the verb's result lists (ochazuke.Operation.Rows,
// declared with NounBuilder.Rows), "" when the result is not rows. Quiet
// marks bookkeeping the jobs ticker hides (NounBuilder.Quiet). Positional
// names the parameters bare words fill first (NounBuilder.Positional).
type CatalogVerb struct {
	Name        string                        `json:"name"`
	Description string                        `json:"description,omitempty"`
	Kind        string                        `json:"kind,omitempty"`
	Params      map[string]ochazuke.Parameter `json:"params,omitempty"`
	Result      string                        `json:"result,omitempty"`
	Confirm     string                        `json:"confirm,omitempty"`
	Repeatable  bool                          `json:"repeatable,omitempty"`
	Rows        string                        `json:"rows,omitempty"`
	Quiet       bool                          `json:"quiet,omitempty"`
	Positional  []string                      `json:"positional,omitempty"`
}

// CatalogPage is a page in registration order.
type CatalogPage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Catalog describes r as indented JSON: every operation grouped by noun
// (nouns and verbs sorted; params are a map, which encoding/json writes
// sorted), every noun's binding rows (sorted by mode, noun, key, then
// request, stably; noun and mode filled in, the LoadBindings shape) and the
// pages in registration order (their tab order). The same runtime gives the
// same bytes on every run.
func Catalog(r *ochazuke.Runtime, name, version string) ([]byte, error) {
	doc := CatalogDoc{Schema: Schema, Name: name, Version: version, Nouns: []CatalogNoun{}, Bindings: []ochazuke.Binding{}, Pages: []CatalogPage{}}
	for _, op := range r.Catalog() {
		if n := len(doc.Nouns); n == 0 || doc.Nouns[n-1].Name != op.Noun {
			noun := CatalogNoun{Name: op.Noun}
			if n := r.Nouns[op.Noun]; n != nil {
				noun.Pin, noun.Columns = n.Pin, slices.Clone(n.Columns)
			}
			doc.Nouns = append(doc.Nouns, noun)
		}
		if op.State && doc.State == "" {
			doc.State = op.Key()
		}
		noun := &doc.Nouns[len(doc.Nouns)-1]
		verb := CatalogVerb{Name: op.Verb, Description: op.Description, Kind: op.Kind, Result: op.Result, Confirm: op.Confirm, Repeatable: op.Repeatable, Rows: op.Rows, Quiet: op.Quiet, Positional: op.Positional}
		if len(op.Parameters) > 0 {
			verb.Params = op.Parameters
		}
		noun.Verbs = append(noun.Verbs, verb)
	}
	names := make([]string, 0, len(r.Nouns))
	for name := range r.Nouns {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		n := r.Nouns[name]
		modes := make([]string, 0, len(n.Bindings))
		for mode := range n.Bindings {
			modes = append(modes, mode)
		}
		sort.Strings(modes)
		for _, mode := range modes {
			for _, row := range n.Bindings[mode] {
				row.Noun, row.Mode = name, mode
				doc.Bindings = append(doc.Bindings, row)
			}
		}
	}
	// Rows sort by mode, noun, key, then request, stably: equal rows keep
	// table order, and no map order reaches the output.
	sort.SliceStable(doc.Bindings, func(i, j int) bool {
		a, b := doc.Bindings[i], doc.Bindings[j]
		if a.Mode != b.Mode {
			return a.Mode < b.Mode
		}
		if a.Noun != b.Noun {
			return a.Noun < b.Noun
		}
		if a.Keys != b.Keys {
			return a.Keys < b.Keys
		}
		return requestKey(a.Request) < requestKey(b.Request)
	})
	for _, page := range r.PageList() {
		doc.Pages = append(doc.Pages, CatalogPage{ID: page.ID, Title: page.Title})
	}
	return json.MarshalIndent(doc, "", "  ")
}

// requestKey orders requests by their JSON, whose object keys
// encoding/json writes sorted.
func requestKey(req ochazuke.Request) string {
	data, _ := json.Marshal(req)
	return string(data)
}
