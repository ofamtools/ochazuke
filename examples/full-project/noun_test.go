package main

import (
	"fmt"
	"slices"
	"testing"
)

// Rule: Runtime.Page is always an ancestor of the base frame's focus, on
// every step of the configured project's smoke session.
func TestSmokeKeepsPageOnFocusPath(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := m.Runtime
	check := func(label string) error {
		if r.Page == "" {
			return fmt.Errorf("%s: no current page", label)
		}
		if !slices.Contains(r.Ancestry(r.Frames[0].Focus), r.Page) {
			return fmt.Errorf("%s: page %q is not an ancestor of base focus %q", label, r.Page, r.Frames[0].Focus)
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

// Every row the configured project installs, from its binding files and its
// Go, is a Request to a registered operation or an unbind.
func TestFullProjectBindingRowsAreAtomic(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Build(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Runtime.ValidateBindings(); err != nil {
		t.Fatal(err)
	}
}
