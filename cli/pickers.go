package cli

import "ofam.dev/ochazuke"

// CatalogPickers are doc's declared pickers as a host adds them with
// Runtime.AddPicker: each name prefixed (prefix + "-" + name, so two
// tools' "run" pickers stay apart; "" keeps it), each noun and each verb's
// noun mapped to the host's (noun, as CatalogSegments maps it; nil keeps
// it). The host's derived noun pickers come from the nouns it mounts.
func CatalogPickers(doc CatalogDoc, prefix string, noun func(string) string) []ochazuke.PickerDef {
	mapNoun := func(n string) string {
		if n == "" || noun == nil {
			return n
		}
		return noun(n)
	}
	mapVerb := func(key string) string {
		if key == "" {
			return key
		}
		n, v := ochazuke.SplitKey(key)
		return mapNoun(n) + "." + v
	}
	var out []ochazuke.PickerDef
	for _, d := range doc.Pickers {
		if prefix != "" {
			d.Name = prefix + "-" + d.Name
		}
		d.Noun = mapNoun(d.Noun)
		d.Source, d.Preview, d.Pick, d.Verb = mapVerb(d.Source), mapVerb(d.Preview), mapVerb(d.Pick), mapVerb(d.Verb)
		sources := make([]string, len(d.Sources))
		for i, s := range d.Sources {
			sources[i] = mapVerb(s)
		}
		if len(sources) > 0 {
			d.Sources = sources
		}
		out = append(out, d)
	}
	return out
}

// MountCatalogPickers adds doc's declared pickers to r (CatalogPickers):
// the one call a host makes per extension, beside MountCatalogHooks.
func MountCatalogPickers(r *ochazuke.Runtime, doc CatalogDoc, prefix string, noun func(string) string) error {
	for _, d := range CatalogPickers(doc, prefix, noun) {
		if err := r.AddPicker(d); err != nil {
			return err
		}
	}
	return nil
}
