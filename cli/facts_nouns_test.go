package cli

import (
	"io"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/nouncheck"
)

// nounRows are the nouncheck rows of r that name noun.
func nounRows(r *ochazuke.Runtime, noun string) []string {
	var out []string
	for _, row := range nouncheck.Rows(r) {
		if strings.Split(row, "\t")[1] == noun {
			out = append(out, row)
		}
	}
	return out
}

// ochazuke.catalog is a thing noun with a facts store over the catalog's
// nouns, so no tool's nouncheck baseline holds its list: get reads one
// noun, the writing verbs are unavailable (a system fact), and the
// discovery verbs list and describe answer as before.
func TestCatalogIsAFactsNoun(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	r.Config().NoHistoryFile = true
	New(r, "tool", strings.NewReader(""), io.Discard, io.Discard)
	if rows := nounRows(r, "ochazuke.catalog"); len(rows) > 0 {
		t.Fatalf("nouncheck rows: %q", rows)
	}
	v, err := r.Execute(ochazuke.Request{Noun: "ochazuke.catalog", Verb: "get", Args: ochazuke.Arguments{"name": "ochazuke.register"}})
	if err != nil {
		t.Fatal(err)
	}
	thing, _ := v.(map[string]any)
	if thing["name"] != "ochazuke.register" || !strings.Contains(strings.Join(anyStrings(thing["verbs"]), " "), "append") {
		t.Fatalf("catalog get: %#v", v)
	}
	if op := r.Operations["ochazuke.catalog.set"]; op.Unavailable != ochazuke.FactReason {
		t.Fatalf("catalog set: unavailable %q", op.Unavailable)
	}
	if op := r.Operations["ochazuke.catalog.list"]; op.Standard != "" {
		t.Fatalf("catalog list became the standard list: %+v", op)
	}
	v, err = r.Execute(ochazuke.Request{Noun: "ochazuke.catalog", Verb: "list", Args: ochazuke.Arguments{"noun": "ochazuke.register", "include": "all"}})
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := v.(map[string]any)
	if nouns, _ := doc["nouns"].([]map[string]any); len(nouns) != 1 || nouns[0]["name"] != "ochazuke.register" {
		t.Fatalf("catalog list: %#v", v)
	}
	if _, err := r.Execute(ochazuke.Request{Noun: "ochazuke.catalog", Verb: "describe", Args: ochazuke.Arguments{"verb": "ochazuke.register.set"}}); err != nil {
		t.Fatalf("catalog describe: %v", err)
	}
}

// The facts nouns' own list verbs (catalog, setup) replace the standard
// list on purpose (Operation.ShadowsStandard), so a tool's checks print no
// info line for them.
func TestFactsNounListsAreDeclaredShadows(t *testing.T) {
	f := newSetupFixture(t)
	New(f.r, "tool", strings.NewReader(""), io.Discard, io.Discard)
	for _, key := range []string{"ochazuke.catalog.list", "ochazuke.setup.list"} {
		if op := f.r.Operations[key]; !op.ShadowsStandard {
			t.Fatalf("%s not declared a shadow: %+v", key, op)
		}
	}
	if got := f.r.StandardShadows(); len(got) > 0 {
		t.Fatalf("shadows reported: %q", got)
	}
}

func anyStrings(v any) []string {
	switch s := v.(type) {
	case []string:
		return s
	case []any:
		out := make([]string, len(s))
		for i, x := range s {
			out[i], _ = x.(string)
		}
		return out
	}
	return nil
}

// ochazuke.setup is a thing noun with a facts store over the declared
// items: get reads one item's state, and setup list, status and the rest
// answer as before.
func TestSetupIsAFactsNoun(t *testing.T) {
	f := newSetupFixture(t)
	New(f.r, "tool", strings.NewReader(""), io.Discard, io.Discard)
	if rows := nounRows(f.r, "ochazuke.setup"); len(rows) > 0 {
		t.Fatalf("nouncheck rows: %q", rows)
	}
	v, err := f.r.Execute(ochazuke.Request{Noun: "ochazuke.setup", Verb: "get", Args: ochazuke.Arguments{"id": "rules"}})
	if err != nil {
		t.Fatal(err)
	}
	thing, _ := v.(map[string]any)
	if thing["id"] != "rules" || thing["state"] != "missing" || thing["name"] != "Agent rules" {
		t.Fatalf("setup get: %#v", v)
	}
	if _, err := f.r.Execute(ochazuke.Request{Noun: "ochazuke.setup", Verb: "get", Args: ochazuke.Arguments{"id": "nope"}}); err == nil {
		t.Fatal("setup get of an unknown item")
	}
	if op := f.r.Operations["ochazuke.setup.delete"]; op.Unavailable != ochazuke.FactReason {
		t.Fatalf("setup delete: unavailable %q", op.Unavailable)
	}
	if op := f.r.Operations["ochazuke.setup.list"]; op.Standard != "" {
		t.Fatalf("setup list became the standard list: %+v", op)
	}
	out, stderr, err := f.run(t, "", "setup", "list")
	if err != nil || !strings.Contains(out, "rules  block  missing") {
		t.Fatalf("setup list: %v\n%s\n%s", err, out, stderr)
	}
}
