package ochazuke

import "testing"

// A Ref prop and a Ref parameter read what the edge wrote through the
// noun's store once: a label becomes the key, a key stays, a text no thing
// holds and a label several things hold are kept as written.
func TestRefValueResolvesALabelOnce(t *testing.T) {
	t.Parallel()
	props := keyedProps()
	props[2].Ref = []string{"note"}
	r, s := newKeyedRuntime(t, props...)
	r.Define("note").Verb("echo", func(c Ctx) Result { return Done(c.Args["of"]) }, "Echo a reference")
	op := r.Operations.At(Key(r.CanonicalKey("note.echo")))
	op.Parameters = map[string]Parameter{"of": {Type: "string", Ref: []string{"note"}}}
	r.Operations.Set(op)
	for _, c := range []struct{ written, stored string }{
		{"Beta", "t10"},      // a label: its key
		{"t2", "t2"},         // a key
		{"t1", "t1"},         // a key beats the thing named t1
		{"nosuch", "nosuch"}, // kept as written
		{"Twin", "Twin"},     // two things hold it: kept, with a notice
	} {
		if _, err := r.Execute(Request{Noun: "note", Verb: "set", Args: Arguments{"id": "t1", "prop": "owner", "value": c.written}}); err != nil {
			t.Fatalf("set owner %q: %v", c.written, err)
		}
		if got := s.find("t1")["owner"]; got != c.stored {
			t.Errorf("owner %q stored %v, want %s", c.written, got, c.stored)
		}
		got, err := r.Execute(Request{Noun: "note", Verb: "echo", Args: Arguments{"of": c.written}})
		if err != nil || got != c.stored {
			t.Errorf("echo %q = %v, %v; want %s", c.written, got, err, c.stored)
		}
	}
}
