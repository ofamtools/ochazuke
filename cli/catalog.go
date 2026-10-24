package cli

import (
	"encoding/json"
	"sort"

	"github.com/unixsuperhero/ochazuke"
)

// Schema is the catalog format version Catalog writes.
const Schema = 1

// CatalogDoc is the catalog answer: what a runtime registers, grouped by
// noun, in the runtime's own JSON shapes (ochazuke.Parameter and
// ochazuke.Binding unchanged). Nothing in it is invented: every field reads
// a registry the runtime already has.
type CatalogDoc struct {
	Schema   int                `json:"schema"`
	Name     string             `json:"name"`
	Version  string             `json:"version"`
	Nouns    []CatalogNoun      `json:"nouns"`
	Bindings []ochazuke.Binding `json:"bindings"`
	Pages    []CatalogPage      `json:"pages"`
}

// CatalogNoun is a noun and its verbs, by verb name.
type CatalogNoun struct {
	Name  string        `json:"name"`
	Verbs []CatalogVerb `json:"verbs"`
}

// CatalogVerb is one ochazuke.Operation. Kind is "application" for a verb
// whose handler may return pending work, "interaction" otherwise.
type CatalogVerb struct {
	Name        string                        `json:"name"`
	Description string                        `json:"description,omitempty"`
	Kind        string                        `json:"kind,omitempty"`
	Params      map[string]ochazuke.Parameter `json:"params,omitempty"`
	Result      string                        `json:"result,omitempty"`
	Confirm     string                        `json:"confirm,omitempty"`
	Repeatable  bool                          `json:"repeatable,omitempty"`
}

// CatalogPage is a page in registration order.
type CatalogPage struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Catalog describes r as indented JSON: every operation grouped by noun
// (nouns and verbs sorted), every noun's binding rows (noun, then mode,
// sorted; rows in table order, with noun and mode filled in, the
// LoadBindings shape) and the pages in registration order.
func Catalog(r *ochazuke.Runtime, name, version string) ([]byte, error) {
	doc := CatalogDoc{Schema: Schema, Name: name, Version: version, Nouns: []CatalogNoun{}, Bindings: []ochazuke.Binding{}, Pages: []CatalogPage{}}
	for _, op := range r.Catalog() {
		if n := len(doc.Nouns); n == 0 || doc.Nouns[n-1].Name != op.Noun {
			doc.Nouns = append(doc.Nouns, CatalogNoun{Name: op.Noun})
		}
		noun := &doc.Nouns[len(doc.Nouns)-1]
		verb := CatalogVerb{Name: op.Verb, Description: op.Description, Kind: op.Kind, Result: op.Result, Confirm: op.Confirm, Repeatable: op.Repeatable}
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
	for _, page := range r.PageList() {
		doc.Pages = append(doc.Pages, CatalogPage{ID: page.ID, Title: page.Title})
	}
	return json.MarshalIndent(doc, "", "  ")
}
