package ochazuke

import (
	"slices"
	"strings"
)

// NameVocab is every name this runtime declares, each with its
// categories, for namecheck: the nouns (full and short names), the verbs
// and operations, the modes, the motions rows and parameters carry, and
// the parameter names. An application passes it to namecheck.Check so the
// check finds Go that branches on a name the declarations already hold.
func (r *Runtime) NameVocab() map[string][]string {
	v := map[string][]string{}
	add := func(cat string, names ...string) {
		for _, n := range names {
			if n == "" {
				continue
			}
			if !slices.Contains(v[n], cat) {
				v[n] = append(v[n], cat)
			}
		}
	}
	for name, n := range r.Nouns {
		add("noun", name)
		add("noun", name[strings.LastIndex(name, ".")+1:])
		add("verb", r.Operations.ActionNames(n)...)
		for mode := range n.Bindings {
			add("mode", mode)
		}
		for mode, base := range n.Extends {
			add("mode", mode, base)
		}
		r.rowNames(n.Bindings, add)
	}
	for _, inst := range r.Instances {
		r.rowNames(map[string][]Binding{"": inst.Bindings}, add)
	}
	for key, op := range r.Operations.Entries() {
		add("operation", key)
		add("verb", op.Name())
		add("noun", op.Noun())
		for name, p := range op.Parameters {
			add("parameter", name)
			if name == "motion" {
				add("motion", p.Enum...)
			}
		}
	}
	for mode, spec := range r.modes {
		add("mode", mode, spec.Extends, spec.Outside)
	}
	for _, d := range r.DefaultBindings() {
		add("mode", d.Mode)
		add("noun", d.Noun)
		add("verb", d.Request.Verb)
		if m, ok := d.Request.Args["motion"].(string); ok {
			add("motion", m)
		}
	}
	add("motion", MotionNames()...)
	for _, cats := range v {
		slices.Sort(cats)
	}
	return v
}

// rowNames adds the modes, verbs and motions the binding rows name.
func (r *Runtime) rowNames(tables map[string][]Binding, add func(string, ...string)) {
	for _, rows := range tables {
		for _, b := range rows {
			add("mode", b.Mode)
			add("verb", b.Request.Verb)
			if m, ok := b.Request.Args["motion"].(string); ok {
				add("motion", m)
			}
		}
	}
}
