package ochazuke

import (
	"fmt"
	"sort"
	"strings"
)

// PaletteInstance is a labeled object whose ID becomes the default action argument.
type PaletteInstance struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// PaletteSource reads current instances in the context captured when the palette opened.
type PaletteSource func(*Runtime, Context) []PaletteInstance

// PaletteNoun explicitly chooses the action and string argument used for an instance.
type PaletteNoun struct {
	Source      PaletteSource
	DefaultVerb string
	Argument    string
}

// PaletteEntry exposes the exact select.palette value independently of its display label.
type PaletteEntry struct {
	Value string `json:"value"`
	Kind  string `json:"kind"`
	Label string `json:"label"`
	ID    string `json:"id,omitempty"`
	Verb  string `json:"verb,omitempty"`
}

// RegisterNoun adds or replaces a live instance source without changing operation forms.
func (p *Palette) RegisterNoun(noun string, config PaletteNoun) error {
	known := false
	for _, op := range p.r.Catalog() {
		if op.Noun == noun {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("palette: unknown noun %q", noun)
	}
	if config.Source == nil {
		return fmt.Errorf("palette noun %q: source is required", noun)
	}
	op, ok := p.r.Operations[Request{Verb: config.DefaultVerb, Noun: noun}.Key()]
	if !ok {
		return fmt.Errorf("palette noun %q: unknown default action %q", noun, config.DefaultVerb+"."+noun)
	}
	param, ok := op.Parameters[config.Argument]
	if !ok || param.Type != "string" {
		return fmt.Errorf("palette noun %q: default action %s requires a string instance argument; got %q", noun, op.Key(), config.Argument)
	}
	for name, param := range op.Parameters {
		if name != config.Argument && param.Required {
			return fmt.Errorf("palette noun %q: default action %s also requires argument %q", noun, op.Key(), name)
		}
	}
	p.nouns[noun] = config
	return nil
}

// PagesPaletteSource reads the runtime registry on every listing, not at startup.
func PagesPaletteSource(r *Runtime, _ Context) []PaletteInstance {
	var instances []PaletteInstance
	for _, page := range r.PageList() {
		instances = append(instances, PaletteInstance{ID: page.ID, Label: page.Title})
	}
	return instances
}

func (p *Palette) entries() []PaletteEntry {
	var entries []PaletteEntry
	seen := map[string]bool{}
	add := func(entry PaletteEntry) {
		if !seen[entry.Value] {
			seen[entry.Value] = true
			entries = append(entries, entry)
		}
	}
	config, instances := p.nouns[p.first]
	for _, op := range p.r.Catalog() {
		if p.stage == "root" {
			add(PaletteEntry{Value: "verb:" + op.Verb, Kind: "verb", Label: op.Verb, Verb: op.Verb})
			add(PaletteEntry{Value: "noun:" + op.Noun, Kind: "noun", Label: op.Noun})
		} else if p.section == "verbs" && op.Verb == p.first {
			add(PaletteEntry{Value: op.Noun, Kind: "noun", Label: op.Noun})
		} else if p.section == "nouns" && op.Noun == p.first {
			value := op.Verb
			if instances {
				value = "verb:" + op.Verb
			}
			add(PaletteEntry{Value: value, Kind: "verb", Label: op.Verb, Verb: op.Verb})
		}
	}
	if p.stage == "second" && p.section == "nouns" && instances {
		for _, instance := range config.Source(p.r, p.origin.Clone()) {
			label := instance.Label
			if label == "" {
				label = instance.ID
			}
			add(PaletteEntry{Value: "instance:" + instance.ID, Kind: "instance", Label: label, ID: instance.ID})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind == "verb"
		}
		return entries[i].Value < entries[j].Value
	})
	if p.query == "" {
		return entries
	}
	query := strings.ToLower(p.query)
	filtered := make([]PaletteEntry, 0, len(entries))
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Value+" "+entry.Label), query) {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}
