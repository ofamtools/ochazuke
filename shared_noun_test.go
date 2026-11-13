package ochazuke

import (
	"errors"
	"testing"
)

// TestSharedNoun: a bare word two namespaced nouns share reads as the
// setting cli.ambiguous says, one case per value, the same in the CLI
// reader (lexNouns) and the palette's line index (tails).
func TestSharedNoun(t *testing.T) {
	done := func(Ctx) Result { return Done(nil) }
	build := func(setting string) *Runtime {
		r := NewRuntime()
		r.Define("md.doc").Verb("rows", done, "Rows")
		r.Define("json.doc").Verb("rows", done, "Rows") // registered after md.doc
		if err := r.Settings().Set(CLIAmbiguousSetting, setting); err != nil {
			t.Fatal(err)
		}
		return r
	}
	for _, c := range []struct{ setting, want string }{
		{CLIAmbiguousHost, ""}, // no host noun named doc: refused
		{CLIAmbiguousAsk, ""},
		{CLIAmbiguousFirst, "md.doc"},
	} {
		r := build(c.setting)
		got, err := r.SharedNoun("doc", []string{"json.doc", "md.doc"})
		var amb *AmbiguousNounError
		if got != c.want || (c.want == "") != errors.As(err, &amb) {
			t.Errorf("%s: SharedNoun = %q, %v; want %q", c.setting, got, err, c.want)
		}
		if amb != nil && (len(amb.Nouns) != 2 || amb.Nouns[0] != "json.doc") {
			t.Errorf("%s: the error names %v, want json.doc, md.doc", c.setting, amb.Nouns)
		}
		reader := 0
		for _, m := range r.lexNouns("doc", true) {
			if m.Via == ViaTail {
				reader++
			}
		}
		palette := len(r.lineIndex(func(Operation) bool { return true }, func(a, b string) bool { return a < b }).tails["doc"])
		if want := map[bool]int{true: 2, false: 1}[c.want == ""]; reader != want || palette != want {
			t.Errorf("%s: the reader reads doc as %d nouns and the palette as %d, want %d", c.setting, reader, palette, want)
		}
	}
	// host: the host's own noun of the name wins over the namespaced ones.
	r := build(CLIAmbiguousHost)
	r.DefineOwn("doc").Verb("rows", done, "Rows")
	if got, err := r.SharedNoun("doc", []string{"json.doc", "doc", "md.doc"}); got != "doc" || err != nil {
		t.Errorf("host: SharedNoun = %q, %v; want doc", got, err)
	}
}
