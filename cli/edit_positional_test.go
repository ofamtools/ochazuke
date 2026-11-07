package cli

import (
	"io"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
)

// The CLI reads edit's bare words as the palette does: the key, the field,
// then chooser; a prop is given only as prop=value or --prop, so a word
// after the field that is not true or false is refused, never a prop's
// value (UX R3 F3).
func TestEditPositionalWords(t *testing.T) {
	t.Parallel()
	r := ochazuke.NewRuntime()
	r.Config().Disable()
	r.Define("parcel").Props(
		ochazuke.PropSpec{Name: "id", Key: true},
		ochazuke.PropSpec{Name: "title", Role: ochazuke.RoleLabel},
		ochazuke.PropSpec{Name: "status", Parameter: ochazuke.Parameter{Enum: []string{"todo", "done"}}},
		ochazuke.PropSpec{Name: "dir"},
	).Store(ochazuke.Store{
		Get: func(ochazuke.Ctx, string) (ochazuke.Thing, error) { return ochazuke.Thing{"id": "t1"}, nil },
		Patch: func(ochazuke.Ctx, string, ochazuke.Thing) (ochazuke.Thing, error) {
			return ochazuke.Thing{"id": "t1"}, nil
		},
	})
	s := New(r, "tool", strings.NewReader(""), io.Discard, io.Discard)
	for words, want := range map[string]ochazuke.Arguments{
		"t2 status false":   {"id": "t2", "field": "status", "chooser": false},
		"t2 hello":          {"id": "t2", "field": "hello"},
		"status false":      {"field": "status", "chooser": false},
		"t2 --dir=hello":    {"id": "t2", "dir": "hello"},
		"t2 status --dir x": {"id": "t2", "field": "status", "dir": "x"},
	} {
		q, _, err := s.request("parcel", "edit", strings.Fields(words))
		if err != nil || len(q.Args) != len(want) {
			t.Fatalf("%q: %v %v", words, q.Args, err)
		}
		for k, v := range want {
			if q.Args[k] != v {
				t.Fatalf("%q: %s=%#v, want %#v (%v)", words, k, q.Args[k], v, q.Args)
			}
		}
	}
	for _, words := range []string{"t2 status done", "t2 status false more"} {
		if q, _, err := s.request("parcel", "edit", strings.Fields(words)); err == nil {
			t.Fatalf("%q read: %v", words, q.Args)
		}
	}
}
