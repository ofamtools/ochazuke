package main

import (
	"fmt"
	"reflect"
	"testing"

	"ochazuke"
)

// Rule for phase 5: on every context the configured project produces, the
// instance graph rebuilds the active context and the noun walk agrees with the
// layer path on bindings and action overrides.
func TestSmokeKeepsNounModelConsistent(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := m.Runtime
	focusOf := func(c ochazuke.Context) string {
		if c.InputID != "" {
			return c.InputID
		}
		return c.Path[len(c.Path)-1]
	}
	pointer := func(fn ochazuke.Handler) uintptr {
		if fn == nil {
			return 0
		}
		return reflect.ValueOf(fn).Pointer()
	}
	check := func(label string) error {
		if got, want := r.ContextFromFocus(), r.Context(); !reflect.DeepEqual(got, want) {
			return fmt.Errorf("%s: ContextFromFocus %+v; Context %+v", label, got, want)
		}
		if ambiguous := r.AmbiguousInstances(); len(ambiguous) != 0 {
			return fmt.Errorf("%s: ambiguous parents %v", label, ambiguous)
		}
		var contexts []ochazuke.Context
		for _, f := range r.Frames {
			contexts = append(contexts, f.Context)
		}
		for _, page := range r.Pages {
			contexts = append(contexts, page.Frame.Context)
		}
		for _, c := range contexts {
			focus := focusOf(c)
			if got, want := r.EffectiveBindingsFor(focus, r.Mode(c)), r.EffectiveBindings(c); !reflect.DeepEqual(got, want) {
				return fmt.Errorf("%s: bindings for %+v differ\n new %v\n old %v", label, c, got, want)
			}
			for key := range r.Operations {
				var want ochazuke.Handler
				for i := len(c.Path) - 1; i >= 0 && want == nil; i-- {
					want = r.Layers[c.Path[i]].Actions[key]
				}
				if pointer(r.ActionFor(focus, key)) != pointer(want) {
					return fmt.Errorf("%s: action %s for %+v resolves differently", label, key, c)
				}
			}
		}
		return nil
	}
	if err := check("build"); err != nil {
		t.Fatal(err)
	}
	smokeStep = check
	defer func() { smokeStep = nil }()
	if err := smokeExample(m); err != nil {
		t.Fatal(err)
	}
}
